//! Single instance and request forwarding over a Unix socket in the per-user temp folder.
//! Binding the socket claims the instance; a second launch connects to it and sends its
//! requests as JSON.

use crate::ipc::Request;
use anyhow::{bail, Context as _, Result};
use std::io::{Read, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::Mutex;
use std::time::Duration;

static LISTENER: Mutex<Option<UnixListener>> = Mutex::new(None);

fn socket() -> PathBuf {
    std::env::temp_dir().join("glint.sock")
}

/// True when this process is the first Glint, which then owns the socket.
pub(crate) fn claim() -> bool {
    let path = socket();
    if UnixStream::connect(&path).is_ok() {
        return false;
    }
    let _ = std::fs::remove_file(&path);
    match UnixListener::bind(&path) {
        Ok(l) => {
            *LISTENER.lock().unwrap() = Some(l);
            true
        }
        Err(_) => true,
    }
}

pub fn listen() {
    let Some(listener) = LISTENER.lock().unwrap().take() else { return };
    std::thread::spawn(move || {
        for mut stream in listener.incoming().flatten() {
            let mut bytes = Vec::new();
            if stream.read_to_end(&mut bytes).is_ok() {
                if let Ok(reqs) = serde_json::from_slice::<Vec<Request>>(&bytes) {
                    crate::ipc::deliver(reqs);
                    let _ = stream.write_all(b"1");
                }
            }
        }
    });
}

pub fn forward(requests: &[Request]) -> Result<()> {
    let payload = serde_json::to_vec(requests)?;
    let mut stream = None;
    for _ in 0..50 {
        if let Ok(s) = UnixStream::connect(socket()) {
            stream = Some(s);
            break;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let mut stream = stream.context("Glint is starting but not answering")?;
    stream.set_read_timeout(Some(Duration::from_secs(5)))?;
    stream.write_all(&payload)?;
    stream.shutdown(std::net::Shutdown::Write)?;
    let mut ack = Vec::new();
    let _ = stream.read_to_end(&mut ack);
    if ack != b"1" {
        bail!("the running Glint did not accept the request");
    }
    Ok(())
}
