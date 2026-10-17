use std::sync::Mutex;
use std::time::{Duration, Instant};

pub struct MediaClock {
    state: Mutex<State>,
}

enum State {
    Paused { elapsed: Duration },
    Running { elapsed_before: Duration, since: Instant },
    /// The master track ended at `elapsed`; nothing resumes an ended clock.
    Ended { elapsed: Duration },
}

impl MediaClock {
    pub fn paused() -> Self {
        Self { state: Mutex::new(State::Paused { elapsed: Duration::ZERO }) }
    }

    /// Media time elapsed so far, excluding paused spans.
    pub fn now(&self) -> Duration {
        match &*self.state.lock().unwrap() {
            State::Paused { elapsed } | State::Ended { elapsed } => *elapsed,
            State::Running { elapsed_before, since } => *elapsed_before + since.elapsed(),
        }
    }

    pub fn now_ms(&self) -> u64 {
        self.now().as_millis() as u64
    }

    /// True whenever media time stands still, so tracks stop writing.
    pub fn is_paused(&self) -> bool {
        !matches!(&*self.state.lock().unwrap(), State::Running { .. })
    }

    /// Ends media time at `elapsed`, which may lie before now when the master track's last
    /// frame is already behind the wall clock. Every track stops writing there, and
    /// `resume` no longer does anything.
    pub fn end_at(&self, elapsed: Duration) {
        *self.state.lock().unwrap() = State::Ended { elapsed };
    }

    /// Freezes media time. Pausing a paused clock is a no-op.
    pub fn pause(&self) {
        let mut state = self.state.lock().unwrap();
        if let State::Running { elapsed_before, since } = &*state {
            *state = State::Paused { elapsed: *elapsed_before + since.elapsed() };
        }
    }

    /// Lets media time run again. Resuming a running clock is a no-op.
    pub fn resume(&self) {
        let mut state = self.state.lock().unwrap();
        if let State::Paused { elapsed } = &*state {
            *state = State::Running { elapsed_before: *elapsed, since: Instant::now() };
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::thread::sleep;

    #[test]
    fn starts_paused_at_zero() {
        let clock = MediaClock::paused();
        sleep(Duration::from_millis(20));
        assert_eq!(clock.now(), Duration::ZERO);
        assert!(clock.is_paused());
    }

    #[test]
    fn excludes_paused_spans() {
        let clock = MediaClock::paused();
        clock.resume();
        sleep(Duration::from_millis(30));
        clock.pause();
        let at_pause = clock.now();
        sleep(Duration::from_millis(60));
        assert_eq!(clock.now(), at_pause, "time must not move while paused");
        clock.resume();
        sleep(Duration::from_millis(30));
        let total = clock.now();
        assert!(total >= Duration::from_millis(60), "ran twice for 30ms: {total:?}");
        assert!(total < Duration::from_millis(100), "the 60ms pause leaked in: {total:?}");
    }

    #[test]
    fn an_ended_clock_reads_the_end_time_and_ignores_resume() {
        let clock = MediaClock::paused();
        clock.resume();
        sleep(Duration::from_millis(30));
        clock.end_at(Duration::from_millis(10));
        assert_eq!(clock.now(), Duration::from_millis(10));
        assert!(clock.is_paused());
        clock.resume();
        sleep(Duration::from_millis(10));
        assert_eq!(clock.now(), Duration::from_millis(10));
        assert!(clock.is_paused());
    }

    #[test]
    fn repeated_pause_and_resume_are_idempotent() {
        let clock = MediaClock::paused();
        clock.resume();
        clock.resume();
        sleep(Duration::from_millis(10));
        clock.pause();
        let t = clock.now();
        clock.pause();
        sleep(Duration::from_millis(10));
        assert_eq!(clock.now(), t);
    }
}
