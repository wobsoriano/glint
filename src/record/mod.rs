mod bar;
pub(crate) mod chrome;
mod countdown;
mod guide;
mod picker;
mod prompter;
mod target;

use crate::library::{Item, PendingRecording, RecordingInfo};
use crate::settings::{RecordPrefs, RecordTarget, Settings};
use crate::{flow, overlay, stack, store, toast};
use chrono::Local;
use glint_media::{DeviceId, Fps, MicMonitor, RecordOptions, Recorder, VideoReader};
use gpui::{AnyWindowHandle, App, AppContext as _, Global, Timer};
use std::path::PathBuf;
use std::time::Duration;
pub use bar::clock;
pub use target::Target;

#[derive(Default)]
enum Stage {
    #[default]
    Idle,
    Picking,
    Countdown { left: u32 },
    Starting,
    Live(Box<Live>),
    Finishing,
}

struct Live {
    recorder: Recorder,
    pending: PendingRecording,
    target: Target,
}

#[derive(Default)]
struct Windows {
    picker: Option<AnyWindowHandle>,
    guide: Option<AnyWindowHandle>,
    countdown: Option<AnyWindowHandle>,
    bar: Option<AnyWindowHandle>,
    prompter: Option<AnyWindowHandle>,
    camera: Option<AnyWindowHandle>,
}

#[derive(Default)]
pub struct Rec {
    stage: Stage,
    epoch: u64,
    target: Option<Target>,
    monitor: Option<MicMonitor>,
    windows: Windows,
    quitting: bool,
}

impl Global for Rec {}

fn rec(cx: &mut App) -> &mut Rec {
    cx.default_global::<Rec>()
}

fn prefs(cx: &App) -> RecordPrefs {
    cx.global::<Settings>().record.clone()
}

fn update_prefs(cx: &mut App, f: impl FnOnce(&mut RecordPrefs)) {
    let s = cx.global_mut::<Settings>();
    f(&mut s.record);
    s.save();
}

pub fn toggle(cx: &mut App) {
    match rec(cx).stage {
        Stage::Idle => open_picker(cx),
        Stage::Picking => {
            overlay::close_all(cx);
            close_picker(cx);
        }
        Stage::Countdown { .. } => cancel_countdown(cx),
        Stage::Live(_) => stop(cx),
        Stage::Starting | Stage::Finishing => {}
    }
}

pub fn quit(cx: &mut App) {
    rec(cx).quitting = true;
    match rec(cx).stage {
        Stage::Idle => cx.quit(),
        Stage::Picking => {
            overlay::close_all(cx);
            close_picker(cx);
            cx.quit();
        }
        Stage::Countdown { .. } => {
            cancel_countdown(cx);
            close_picker(cx);
            cx.quit();
        }
        Stage::Live(_) => stop(cx),
        Stage::Starting | Stage::Finishing => {}
    }
}

fn open_picker(cx: &mut App) {
    if rec(cx).target.is_none() {
        let p = prefs(cx);
        let target = match (p.target, p.area) {
            (RecordTarget::Area, Some(area)) => Target::area(area).ok().flatten(),
            (RecordTarget::Window, _) => None,
            _ => Target::display_at_cursor().ok(),
        };
        rec(cx).target = target;
    }
    rec(cx).stage = Stage::Picking;
    restart_monitor(cx);
    show_guide(cx);
    picker::open(cx);
    camera_bubble(cx);
}

pub fn reopen_picker(cx: &mut App) {
    if matches!(rec(cx).stage, Stage::Picking) {
        open_picker(cx);
    }
}

pub fn picked(rect: [i32; 4], window: Option<(isize, String)>, cx: &mut App) {
    let target = match window {
        Some((hwnd, title)) => Target::window(hwnd, title).map(Some),
        None => Target::from_selection(rect),
    };
    match target {
        Ok(Some(t)) => {
            let kind = match &t {
                Target::Display(_) => RecordTarget::Display,
                Target::Window { .. } => RecordTarget::Window,
                Target::Area { .. } => RecordTarget::Area,
            };
            update_prefs(cx, |p| {
                p.target = kind;
                if let Target::Area { rect, .. } = &t {
                    p.area = Some(*rect);
                }
            });
            rec(cx).target = Some(t);
        }
        Ok(None) => toast::show("That area is too small to record", cx),
        Err(e) => toast::show(format!("Could not read the displays: {e:#}"), cx),
    }
    open_picker(cx);
}

fn choose(kind: RecordTarget, cx: &mut App) {
    if !matches!(rec(cx).stage, Stage::Picking) {
        return;
    }
    match kind {
        RecordTarget::Display => {
            match Target::display_at_cursor() {
                Ok(t) => rec(cx).target = Some(t),
                Err(e) => return toast::show(format!("Could not read the displays: {e:#}"), cx),
            }
            update_prefs(cx, |p| p.target = RecordTarget::Display);
            show_guide(cx);
        }
        RecordTarget::Window | RecordTarget::Area => {
            hide_picker_chrome(cx);
            flow::start_area(overlay::Mode::Record, cx);
        }
    }
}

fn hide_picker_chrome(cx: &mut App) {
    let w = &mut rec(cx).windows;
    let handles = [w.picker.take(), w.guide.take(), w.camera.take()];
    for h in handles {
        chrome::close(h, cx);
    }
}

fn close_picker(cx: &mut App) {
    rec(cx).epoch += 1;
    hide_picker_chrome(cx);
    let r = rec(cx);
    r.monitor = None;
    r.stage = Stage::Idle;
    let countdown = r.windows.countdown.take();
    chrome::close(countdown, cx);
}

fn restart_monitor(cx: &mut App) {
    let mic = prefs(cx).mic;
    rec(cx).monitor = None;
    rec(cx).monitor = mic.and_then(|id| MicMonitor::start(Some(DeviceId(id))).ok());
}

fn camera_bubble(cx: &mut App) {
    let old = rec(cx).windows.camera.take();
    chrome::close(old, cx);
    let Some(id) = prefs(cx).camera else { return };
    let Some(display) = rec(cx).target.as_ref().map(|t| t.display().clone()) else { return };
    rec(cx).windows.camera = picker::open_camera(DeviceId(id), &display, cx);
}

fn show_guide(cx: &mut App) {
    let old = rec(cx).windows.guide.take();
    chrome::close(old, cx);
    let Some(target) = rec(cx).target.clone() else { return };
    rec(cx).windows.guide = guide::open(target, cx);
}

fn record(cx: &mut App) {
    if !matches!(rec(cx).stage, Stage::Picking) {
        return;
    }
    let Some(target) = rec(cx).target.clone() else {
        return choose(prefs(cx).target, cx);
    };
    let secs = prefs(cx).countdown;
    if secs == 0 {
        return begin(cx);
    }
    let r = rec(cx);
    r.epoch += 1;
    let epoch = r.epoch;
    r.stage = Stage::Countdown { left: secs };
    let window = countdown::open(&target, cx);
    rec(cx).windows.countdown = window;
    cx.spawn(async move |cx| loop {
        Timer::after(Duration::from_secs(1)).await;
        let go_on = cx.update(|cx| {
            let r = rec(cx);
            if r.epoch != epoch {
                return false;
            }
            let Stage::Countdown { left } = &mut r.stage else { return false };
            *left -= 1;
            if *left == 0 {
                begin(cx);
                return false;
            }
            true
        });
        if !matches!(go_on, Ok(true)) {
            break;
        }
    })
    .detach();
}

fn cancel_countdown(cx: &mut App) {
    if !matches!(rec(cx).stage, Stage::Countdown { .. }) {
        return;
    }
    let r = rec(cx);
    r.epoch += 1;
    r.stage = Stage::Picking;
    let countdown = r.windows.countdown.take();
    chrome::close(countdown, cx);
}

fn countdown_left(cx: &App) -> Option<u32> {
    match cx.try_global::<Rec>()?.stage {
        Stage::Countdown { left } => Some(left),
        _ => None,
    }
}

fn begin(cx: &mut App) {
    let Some(target) = rec(cx).target.clone() else { return };
    let p = prefs(cx);
    let r = rec(cx);
    r.epoch += 1;
    let epoch = r.epoch;
    r.stage = Stage::Starting;
    r.monitor = None;
    let countdown = r.windows.countdown.take();
    chrome::close(countdown, cx);
    let picker = rec(cx).windows.picker.take();
    chrome::close(picker, cx);
    let camera = rec(cx).windows.camera.take();
    chrome::close(camera, cx);
    if rec(cx).windows.guide.is_none() {
        show_guide(cx);
    }

    let pending = match store::library(cx).begin_recording(Local::now()) {
        Ok(p) => p,
        Err(e) => {
            toast::show(format!("Could not start recording: {e:#}"), cx);
            return end(cx);
        }
    };
    let prompting = p.teleprompter && !p.script.trim().is_empty();
    let follower_audio = prompting.then(|| std::sync::mpsc::sync_channel::<Vec<f32>>(prompter::AUDIO_QUEUE));
    let test_wav = std::env::var_os("GLINT_TELEPROMPTER_WAV").map(PathBuf::from);
    let (mic_tap, follower) = match follower_audio {
        Some((tx, rx)) => {
            let follow = test_wav.is_some() || p.mic.is_some();
            let follower = follow.then(|| prompter::Follower::start(p.script.clone(), rx, prompter::TAP_RATE, crate::settings::data_dir()));
            match test_wav {
                Some(wav) => {
                    prompter::feed_wav(wav, tx);
                    (None, follower)
                }
                None => (p.mic.is_some().then_some(tx), follower),
            }
        }
        None => (None, None),
    };
    if prompting {
        let lines = p.prompter_lines.clamp(1, 6) as usize;
        rec(cx).windows.prompter = prompter::open(&target, &p.script, lines, follower, cx);
    }

    let opts = RecordOptions {
        source: target.source(),
        session_dir: pending.dir.clone(),
        fps: Fps::F30,
        system_audio: p.system_audio,
        mic: p.mic.clone().map(DeviceId),
        camera: p.camera.clone().map(DeviceId),
        record_input: true,
        mic_tap,
    };
    let task = cx.background_spawn(async move { Recorder::start(opts) });
    cx.spawn(async move |cx| {
        let started = task.await;
        let _ = cx.update(|cx| {
            if rec(cx).epoch != epoch {
                if let Ok(recorder) = started {
                    cx.background_spawn(async move { recorder.cancel() }).detach();
                }
                return;
            }
            match started {
                Ok(recorder) => {
                    let warnings = recorder.warnings().to_vec();
                    rec(cx).stage = Stage::Live(Box::new(Live { recorder, pending, target: target.clone() }));
                    if rec(cx).quitting {
                        return stop(cx);
                    }
                    if rec(cx).windows.bar.is_none() {
                        rec(cx).windows.bar = bar::open(&target, cx);
                    }
                    if !warnings.is_empty() {
                        eprintln!("recording started with warnings: {}", warnings.join("; "));
                        toast::show_at(warning_summary(&warnings), bar::TOP + 8., cx);
                    }
                    watch_live(epoch, cx);
                }
                Err(e) => {
                    let _ = std::fs::remove_dir_all(&pending.dir);
                    toast::show(format!("Could not start recording: {e:#}"), cx);
                    end(cx);
                    if rec(cx).quitting {
                        cx.quit();
                    }
                }
            }
        });
    })
    .detach();
}

fn watch_live(epoch: u64, cx: &mut App) {
    cx.spawn(async move |cx| loop {
        Timer::after(Duration::from_millis(250)).await;
        let go_on = cx.update(|cx| {
            let r = rec(cx);
            if r.epoch != epoch {
                return false;
            }
            let ended = matches!(&r.stage, Stage::Live(live) if !live.recorder.is_live());
            if ended {
                stop(cx);
            }
            !ended
        });
        if !matches!(go_on, Ok(true)) {
            break;
        }
    })
    .detach();
}

fn end(cx: &mut App) {
    let r = rec(cx);
    r.epoch += 1;
    r.stage = Stage::Idle;
    r.monitor = None;
    let w = std::mem::take(&mut r.windows);
    for h in [w.picker, w.guide, w.countdown, w.bar, w.prompter, w.camera] {
        chrome::close(h, cx);
    }
}

fn take_live(cx: &mut App) -> Option<Live> {
    let r = rec(cx);
    match std::mem::take(&mut r.stage) {
        Stage::Live(live) => Some(*live),
        other => {
            r.stage = other;
            None
        }
    }
}

fn with_live<T>(cx: &mut App, f: impl FnOnce(&Live) -> T) -> Option<T> {
    match &cx.try_global::<Rec>()?.stage {
        Stage::Live(live) => Some(f(live)),
        _ => None,
    }
}

fn pause_toggle(cx: &mut App) {
    with_live(cx, |l| if l.recorder.is_paused() { l.recorder.resume() } else { l.recorder.pause() });
}

fn restart(cx: &mut App) {
    let Some(live) = take_live(cx) else { return };
    let prompter = rec(cx).windows.prompter.take();
    chrome::close(prompter, cx);
    rec(cx).target = Some(live.target);
    let discard = cx.background_spawn(async move { live.recorder.cancel() });
    rec(cx).stage = Stage::Starting;
    cx.spawn(async move |cx| {
        let _ = discard.await;
        let _ = cx.update(begin);
    })
    .detach();
}

fn delete(cx: &mut App) {
    let Some(live) = take_live(cx) else { return };
    end(cx);
    cx.background_spawn(async move { live.recorder.cancel() }).detach();
    toast::show("Recording deleted", cx);
}

fn stop(cx: &mut App) {
    let Some(live) = take_live(cx) else { return };
    end(cx);
    rec(cx).stage = Stage::Finishing;
    let lib = store::library(cx);
    let task = cx.background_spawn(async move {
        let Live { recorder, pending, target } = live;
        let summary = recorder.stop();
        let first = VideoReader::open(&summary.files[0]).and_then(|mut r| r.next_frame()).ok().flatten();
        let info = RecordingInfo { width: summary.width, height: summary.height, duration_ms: summary.duration_ms, source: target.label() };
        let item = lib.finish_recording(&pending, &info, first.as_ref().map(|f| &f.image))?;
        anyhow::Ok((item, summary.warnings))
    });
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| {
            rec(cx).stage = Stage::Idle;
            match result {
                Ok((item, warnings)) => {
                    store::upsert(item.clone(), cx);
                    cx.global_mut::<flow::AppState>().last = Some(item.dir.clone());
                    if !warnings.is_empty() {
                        toast::show(warnings.join(" · "), cx);
                    }
                    if !rec(cx).quitting {
                        automate(item, cx);
                    }
                }
                Err(e) => toast::show(format!("The recording could not be saved: {e:#}"), cx),
            }
            if rec(cx).quitting {
                cx.quit();
            }
        });
    })
    .detach();
}

fn automate(item: Item, cx: &mut App) {
    let a = cx.global::<Settings>().after_recording.clone();
    if a.copy {
        crate::actions::copy(&item, cx);
    }
    if a.save {
        crate::actions::save(&item, false, cx, Box::new(|r, cx| crate::actions::report_save(&r, cx)));
    }
    if a.studio {
        crate::actions::edit(&item, cx);
    }
    if a.preview {
        stack::push_recording(item, cx);
    }
}

fn warning_summary(warnings: &[String]) -> String {
    warnings.iter().map(|w| w.split(": ").next().unwrap_or(w)).collect::<Vec<_>>().join(" · ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn warning_summary_keeps_every_warning_and_drops_device_detail() {
        let warnings = [
            "microphone not recorded: microphone {0.0.1.00000000}.{deadbeef}: Element not found.".to_string(),
            r"camera not recorded: no camera with id \\?\usb#gone".to_string(),
            "could not disable the capture border".to_string(),
        ];
        assert_eq!(warning_summary(&warnings), "microphone not recorded · camera not recorded · could not disable the capture border");
    }
}
