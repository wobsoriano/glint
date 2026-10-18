use anyhow::{bail, Result};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::sync::Mutex;

pub use crate::sys::ipc::{forward, listen};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum CaptureCmd {
    Area,
    Window,
    Screen,
    Text,
    Previous,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Request {
    Capture(CaptureCmd),
    Library,
    Record,
    Trim(PathBuf),
    Open(PathBuf),
    Studio(PathBuf),
}

#[derive(Debug, PartialEq, Eq)]
pub struct Launch {
    pub background: bool,
    pub requests: Vec<Request>,
}

pub const USAGE: &str = "usage: glint.exe [--background] [--library] [--record] [--capture area|window|screen|text|previous] [--studio <session dir>] [--trim <video>] [image, video or Library capture...]";

pub fn parse(args: &[String], cwd: &Path) -> Result<Launch> {
    let mut launch = Launch { background: false, requests: Vec::new() };
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--background" => launch.background = true,
            "--library" => launch.requests.push(Request::Library),
            "--record" => launch.requests.push(Request::Record),
            "--capture" => {
                let what = it.next().map(String::as_str).unwrap_or("");
                let cmd = match what {
                    "area" => CaptureCmd::Area,
                    "window" => CaptureCmd::Window,
                    "screen" => CaptureCmd::Screen,
                    "text" => CaptureCmd::Text,
                    "previous" => CaptureCmd::Previous,
                    _ => bail!("unknown capture kind {what:?}\n{USAGE}"),
                };
                launch.requests.push(Request::Capture(cmd));
            }
            "--trim" => match it.next() {
                Some(path) => launch.requests.push(Request::Trim(cwd.join(path))),
                None => bail!("--trim needs a video file\n{USAGE}"),
            },
            "--studio" => match it.next() {
                Some(dir) => launch.requests.push(Request::Studio(cwd.join(dir))),
                None => bail!("--studio needs a session directory\n{USAGE}"),
            },
            flag if flag.starts_with("--") => bail!("unknown option {flag}\n{USAGE}"),
            path if crate::clip::is_video(Path::new(path)) => launch.requests.push(Request::Trim(cwd.join(path))),
            path => launch.requests.push(Request::Open(cwd.join(path))),
        }
    }
    Ok(launch)
}

static INBOX: Mutex<Vec<Request>> = Mutex::new(Vec::new());

/// Queues requests a second instance forwarded. The platform listener calls it.
pub(crate) fn deliver(requests: Vec<Request>) {
    INBOX.lock().unwrap().extend(requests);
}

pub fn drain() -> Vec<Request> {
    std::mem::take(&mut *INBOX.lock().unwrap())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// An absolute path on this platform: `C:\a\b` on Windows, `/a/b` elsewhere.
    fn abs(parts: &str) -> PathBuf {
        let root = if cfg!(windows) { r"C:\" } else { "/" };
        Path::new(root).join(parts)
    }

    fn args(a: &[&str]) -> Vec<String> {
        a.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn parses_flags_captures_and_relative_paths() {
        let cwd = abs("Users/me/Pictures");
        let other = abs("x/y.jpg");
        let l = parse(&args(&["--background", "--capture", "window", "shot.png", other.to_str().unwrap(), "--library", "--record", "--studio", "rec.glintrec"]), &cwd).unwrap();
        assert_eq!(
            l,
            Launch {
                background: true,
                requests: vec![
                    Request::Capture(CaptureCmd::Window),
                    Request::Open(cwd.join("shot.png")),
                    Request::Open(other),
                    Request::Library,
                    Request::Record,
                    Request::Studio(cwd.join("rec.glintrec")),
                ],
            }
        );
    }

    #[test]
    fn routes_videos_to_trim() {
        let cwd = abs("Users/me/Videos");
        let other = abs("clip.MP4");
        let l = parse(&args(&["--trim", "talk.mov", other.to_str().unwrap(), "shot.png"]), &cwd).unwrap();
        assert_eq!(
            l.requests,
            vec![
                Request::Trim(cwd.join("talk.mov")),
                Request::Trim(other),
                Request::Open(cwd.join("shot.png")),
            ]
        );
        assert!(parse(&args(&["--trim"]), &cwd).unwrap_err().to_string().starts_with("--trim needs a video file"));
    }

    #[test]
    fn rejects_unknown_capture_kinds_and_flags() {
        let cwd = Path::new(r"C:\");
        assert!(parse(&args(&["--capture", "video"]), cwd).unwrap_err().to_string().starts_with("unknown capture kind \"video\""));
        assert!(parse(&args(&["--capture"]), cwd).unwrap_err().to_string().starts_with("unknown capture kind \"\""));
        assert!(parse(&args(&["--studio"]), cwd).unwrap_err().to_string().starts_with("--studio needs a session directory"));
        assert!(parse(&args(&["--nope"]), cwd).unwrap_err().to_string().starts_with("unknown option --nope"));
    }
}
