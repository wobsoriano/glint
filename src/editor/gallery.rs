use crate::{settings, desktop};
use anyhow::Context as _;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

pub struct Wallpaper {
    pub path: PathBuf,
    pub thumb: Arc<gpui::RenderImage>,
}

const THUMB: (u32, u32) = (144, 90);

static THUMBS: Mutex<Vec<(PathBuf, image::RgbaImage)>> = Mutex::new(Vec::new());
static SCANNED: std::sync::Once = std::sync::Once::new();

pub fn user_dir() -> PathBuf {
    settings::data_dir().join("Wallpapers")
}

fn is_image(p: &Path) -> bool {
    p.extension()
        .map(|e| e.to_string_lossy().to_lowercase())
        .is_some_and(|e| matches!(e.as_str(), "jpg" | "jpeg" | "png" | "webp"))
}

fn walk(dir: &Path, out: &mut Vec<PathBuf>) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    let mut entries: Vec<PathBuf> = entries.filter_map(|e| e.ok().map(|e| e.path())).collect();
    entries.sort();
    for p in entries {
        if p.is_dir() {
            walk(&p, out);
        } else if is_image(&p) {
            out.push(p);
        }
    }
}

fn sources() -> Vec<PathBuf> {
    let mut out = Vec::new();
    if let Some(p) = desktop::desktop_wallpaper() {
        out.push(p);
    }
    walk(&user_dir(), &mut out);
    let windir = std::env::var("WINDIR").unwrap_or_else(|_| "C:\\Windows".into());
    walk(&Path::new(&windir).join("Web").join("Wallpaper"), &mut out);
    let mut seen = std::collections::HashSet::new();
    out.retain(|p| seen.insert(p.to_string_lossy().to_lowercase()));
    out
}

fn thumbnail(path: &Path) -> Option<image::RgbaImage> {
    let img = image::open(path).ok()?.to_rgba8();
    let (tw, th) = THUMB;
    let k = (tw as f32 / img.width() as f32).max(th as f32 / img.height() as f32);
    let small = image::imageops::thumbnail(&img, (img.width() as f32 * k).ceil() as u32, (img.height() as f32 * k).ceil() as u32);
    let x = (small.width().saturating_sub(tw)) / 2;
    let y = (small.height().saturating_sub(th)) / 2;
    Some(image::imageops::crop_imm(&small, x, y, tw.min(small.width()), th.min(small.height())).to_image())
}

pub fn scan() {
    SCANNED.call_once(|| {
        std::thread::spawn(|| {
            for p in sources() {
                if let Some(t) = thumbnail(&p) {
                    THUMBS.lock().unwrap().push((p, t));
                }
            }
        });
    });
}

pub fn count() -> usize {
    THUMBS.lock().unwrap().len()
}

pub fn all() -> Vec<Wallpaper> {
    THUMBS.lock().unwrap()
        .iter()
        .map(|(path, t)| Wallpaper { path: path.clone(), thumb: desktop::render_image(t) })
        .collect()
}

pub fn add(path: &Path) -> anyhow::Result<PathBuf> {
    anyhow::ensure!(is_image(path), "Choose a PNG, JPEG or WebP image");
    let dir = user_dir();
    std::fs::create_dir_all(&dir).with_context(|| format!("creating {}", dir.display()))?;
    let name = path.file_name().context("image has no file name")?;
    let mut dest = dir.join(name);
    let mut n = 1;
    while dest.exists() {
        let stem = path.file_stem().unwrap_or_default().to_string_lossy();
        let ext = path.extension().unwrap_or_default().to_string_lossy();
        dest = dir.join(format!("{stem} ({n}).{ext}"));
        n += 1;
    }
    std::fs::copy(path, &dest).with_context(|| format!("copying {}", path.display()))?;
    let thumb = thumbnail(&dest).context("couldn't read that image")?;
    THUMBS.lock().unwrap().insert(0, (dest.clone(), thumb));
    Ok(dest)
}
