//! Media time: a pausable clock every track stamps its samples with, so one pause stops all of
//! them together and paused spans never appear in the outputs.

use std::sync::Mutex;
use std::time::{Duration, Instant};

/// A clock that starts paused at zero, runs while recording, and holds its value while paused.
pub struct MediaClock {
    state: Mutex<State>,
}

enum State {
    Paused { elapsed: Duration },
    Running { elapsed_before: Duration, since: Instant },
}

impl MediaClock {
    /// A clock paused at media time zero.
    pub fn paused() -> Self {
        Self { state: Mutex::new(State::Paused { elapsed: Duration::ZERO }) }
    }

    /// Media time elapsed so far, excluding paused spans.
    pub fn now(&self) -> Duration {
        match &*self.state.lock().unwrap() {
            State::Paused { elapsed } => *elapsed,
            State::Running { elapsed_before, since } => *elapsed_before + since.elapsed(),
        }
    }

    /// `now()` in whole milliseconds.
    pub fn now_ms(&self) -> u64 {
        self.now().as_millis() as u64
    }

    pub fn is_paused(&self) -> bool {
        matches!(&*self.state.lock().unwrap(), State::Paused { .. })
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
