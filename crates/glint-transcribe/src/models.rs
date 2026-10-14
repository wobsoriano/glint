use std::fs::{self, File};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};

use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};

/// Returned (inside `anyhow::Error`) when a cancel flag stops a long job.
#[derive(Debug)]
pub struct Cancelled;

impl std::fmt::Display for Cancelled {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("cancelled")
    }
}

impl std::error::Error for Cancelled {}

/// ggml whisper models published at huggingface.co/ggerganov/whisper.cpp.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum Model {
    TinyEn,
    BaseEn,
    Base,
    Small,
}

impl Model {
    pub const ALL: [Model; 4] = [Model::TinyEn, Model::BaseEn, Model::Base, Model::Small];

    pub fn file_name(self) -> &'static str {
        match self {
            Model::TinyEn => "ggml-tiny.en.bin",
            Model::BaseEn => "ggml-base.en.bin",
            Model::Base => "ggml-base.bin",
            Model::Small => "ggml-small.bin",
        }
    }

    /// Exact published size, used to verify a download and to recognize a complete install.
    pub fn size_bytes(self) -> u64 {
        match self {
            Model::TinyEn => 77_704_715,
            Model::BaseEn => 147_964_211,
            Model::Base => 147_951_465,
            Model::Small => 487_601_967,
        }
    }

    pub fn label(self) -> &'static str {
        match self {
            Model::TinyEn => "Tiny (English)",
            Model::BaseEn => "Base (English)",
            Model::Base => "Base (multilingual)",
            Model::Small => "Small (multilingual)",
        }
    }

    pub fn is_multilingual(self) -> bool {
        matches!(self, Model::Base | Model::Small)
    }

    pub fn url(self) -> String {
        format!("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/{}", self.file_name())
    }
}

pub fn models_dir(data_dir: &Path) -> PathBuf {
    data_dir.join("models")
}

pub fn model_path(data_dir: &Path, model: Model) -> PathBuf {
    models_dir(data_dir).join(model.file_name())
}

pub fn is_installed(data_dir: &Path, model: Model) -> bool {
    fs::metadata(model_path(data_dir, model)).is_ok_and(|m| m.len() == model.size_bytes())
}

pub fn installed_models(data_dir: &Path) -> Vec<Model> {
    Model::ALL.into_iter().filter(|&m| is_installed(data_dir, m)).collect()
}

/// Downloads `model` into `<data_dir>/models`, calling `progress(done, total)` as bytes arrive.
/// Bytes land in a `.part` file that is renamed only after its size checks out, so an
/// interrupted or cancelled download never looks installed. Returns at once if already installed.
pub fn download_model(
    data_dir: &Path,
    model: Model,
    mut progress: impl FnMut(u64, u64),
    cancel: &AtomicBool,
) -> Result<PathBuf> {
    let dest = model_path(data_dir, model);
    let total = model.size_bytes();
    if is_installed(data_dir, model) {
        progress(total, total);
        return Ok(dest);
    }
    let dir = models_dir(data_dir);
    fs::create_dir_all(&dir).with_context(|| format!("creating {}", dir.display()))?;
    let part = dest.with_extension("bin.part");

    let result = fetch(&model.url(), &part, total, &mut progress, cancel);
    if let Err(err) = result {
        let _ = fs::remove_file(&part);
        return Err(err);
    }
    fs::rename(&part, &dest).with_context(|| format!("renaming into {}", dest.display()))?;
    Ok(dest)
}

fn fetch(
    url: &str,
    part: &Path,
    total: u64,
    progress: &mut impl FnMut(u64, u64),
    cancel: &AtomicBool,
) -> Result<()> {
    if cancel.load(Ordering::Relaxed) {
        return Err(Cancelled.into());
    }
    let mut response = ureq::get(url).call().with_context(|| format!("requesting {url}"))?;
    if let Some(len) = response.body().content_length() {
        if len != total {
            bail!("{url} reports {len} bytes, expected {total}");
        }
    }
    let mut reader = response.body_mut().as_reader();
    let mut file = File::create(part).with_context(|| format!("creating {}", part.display()))?;
    let mut buf = vec![0u8; 256 * 1024];
    let mut done = 0u64;
    progress(0, total);
    loop {
        if cancel.load(Ordering::Relaxed) {
            return Err(Cancelled.into());
        }
        let n = reader.read(&mut buf).with_context(|| format!("downloading {url}"))?;
        if n == 0 {
            break;
        }
        file.write_all(&buf[..n]).with_context(|| format!("writing {}", part.display()))?;
        done += n as u64;
        progress(done, total);
    }
    file.sync_all()?;
    if done != total {
        bail!("downloaded {done} bytes of {url}, expected {total}");
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lists_only_complete_models() {
        let data = std::env::temp_dir().join(format!("glint-models-{}", std::process::id()));
        fs::create_dir_all(models_dir(&data)).unwrap();
        File::create(model_path(&data, Model::TinyEn))
            .unwrap()
            .set_len(Model::TinyEn.size_bytes())
            .unwrap();
        File::create(model_path(&data, Model::BaseEn)).unwrap().set_len(1234).unwrap();
        assert_eq!(installed_models(&data), vec![Model::TinyEn]);
        fs::remove_dir_all(&data).unwrap();
    }

    #[test]
    fn cancelled_download_leaves_nothing_behind() {
        let data = std::env::temp_dir().join(format!("glint-cancel-{}", std::process::id()));
        let err = download_model(&data, Model::TinyEn, |_, _| {}, &AtomicBool::new(true)).unwrap_err();
        assert!(err.is::<Cancelled>(), "{err:#}");
        assert!(!model_path(&data, Model::TinyEn).exists());
        assert!(!model_path(&data, Model::TinyEn).with_extension("bin.part").exists());
        let _ = fs::remove_dir_all(&data);
    }
}
