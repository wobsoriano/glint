use anyhow::{bail, Context as _, Result};
use chrono::{DateTime, Local};
use glint_render::doc::EditDoc;
use image::RgbaImage;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

pub use crate::sys::library::recycle;

pub const THUMB_MAX: u32 = 512;
const RECORDING_EXT: &str = "glintrec";

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum CaptureKind {
    Screenshot,
    Recording,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct CaptureMeta {
    pub version: u32,
    pub kind: CaptureKind,
    pub title: String,
    pub created: DateTime<Local>,
    pub modified: DateTime<Local>,
    pub width: u32,
    pub height: u32,
    #[serde(default)]
    pub duration_ms: Option<u64>,
    #[serde(default)]
    pub source: Option<String>,
    #[serde(default)]
    pub imported_from: Option<PathBuf>,
}

#[derive(Clone, Debug)]
pub struct PendingRecording {
    pub id: String,
    pub dir: PathBuf,
    pub created: DateTime<Local>,
}

#[derive(Clone, Debug)]
pub struct RecordingInfo {
    pub width: u32,
    pub height: u32,
    pub duration_ms: u64,
    pub source: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct Item {
    pub dir: PathBuf,
    pub meta: CaptureMeta,
    pub bytes: u64,
}

impl Item {
    pub fn id(&self) -> &str {
        self.dir.file_name().and_then(|n| n.to_str()).unwrap_or_default()
    }

    pub fn kind(&self) -> CaptureKind {
        self.meta.kind
    }

    pub fn deliverable(&self) -> PathBuf {
        match self.meta.kind {
            CaptureKind::Screenshot => self.dir.join("image.png"),
            CaptureKind::Recording => self.dir.join("screen.mp4"),
        }
    }

    pub fn thumb(&self) -> PathBuf {
        self.dir.join("thumb.png")
    }

    pub fn is_edited(&self) -> bool {
        let sidecar = match self.meta.kind {
            CaptureKind::Screenshot => "edit.json",
            CaptureKind::Recording => "project.json",
        };
        self.dir.join(sidecar).exists()
    }
}

pub fn default_title(kind: CaptureKind, at: &DateTime<Local>) -> String {
    let noun = match kind {
        CaptureKind::Screenshot => "Screenshot",
        CaptureKind::Recording => "Recording",
    };
    format!("{noun} {}", at.format("%Y-%m-%d at %H.%M.%S"))
}

#[derive(Clone, Debug)]
pub struct Library {
    root: PathBuf,
}

impl Library {
    pub fn new(root: impl Into<PathBuf>) -> Self {
        Self { root: root.into() }
    }

    pub fn default_root() -> PathBuf {
        crate::settings::data_dir().join("Library")
    }

    pub fn import_screenshot(
        &self,
        base: &RgbaImage,
        edit: Option<(&RgbaImage, &EditDoc)>,
        created: DateTime<Local>,
        source: Option<String>,
        imported_from: Option<PathBuf>,
        title: Option<String>,
    ) -> Result<Item> {
        let meta = CaptureMeta {
            version: 1,
            kind: CaptureKind::Screenshot,
            title: title.unwrap_or_else(|| default_title(CaptureKind::Screenshot, &created)),
            created,
            modified: created,
            width: base.width(),
            height: base.height(),
            duration_ms: None,
            source,
            imported_from,
        };
        std::fs::create_dir_all(&self.root).with_context(|| format!("creating {}", self.root.display()))?;
        let (id, staging) = self.reserve(&created)?;
        let written = (|| -> Result<()> {
            write_png(base, &staging.join("base.png"))?;
            let image = match edit {
                Some((image, doc)) => {
                    write_png(image, &staging.join("image.png"))?;
                    std::fs::write(staging.join("edit.json"), doc.to_json())?;
                    image
                }
                None => {
                    std::fs::copy(staging.join("base.png"), staging.join("image.png"))?;
                    base
                }
            };
            write_png(&thumbnail(image), &staging.join("thumb.png"))?;
            write_meta(&staging, &meta)
        })();
        if let Err(e) = written {
            let _ = std::fs::remove_dir_all(&staging);
            return Err(e);
        }
        let dir = self.root.join(&id);
        std::fs::rename(&staging, &dir).with_context(|| format!("finalizing {}", dir.display()))?;
        load(&dir)
    }

    pub fn begin_recording(&self, created: DateTime<Local>) -> Result<PendingRecording> {
        std::fs::create_dir_all(&self.root).with_context(|| format!("creating {}", self.root.display()))?;
        let (id, dir) = self.reserve(&created)?;
        Ok(PendingRecording { id, dir, created })
    }

    pub fn finish_recording(&self, pending: &PendingRecording, info: &RecordingInfo, first_frame: Option<&RgbaImage>) -> Result<Item> {
        let meta = CaptureMeta {
            version: 1,
            kind: CaptureKind::Recording,
            title: default_title(CaptureKind::Recording, &pending.created),
            created: pending.created,
            modified: pending.created,
            width: info.width,
            height: info.height,
            duration_ms: Some(info.duration_ms),
            source: Some(info.source.clone()),
            imported_from: None,
        };
        if let Some(frame) = first_frame {
            write_png(&thumbnail(frame), &pending.dir.join("thumb.png"))?;
        }
        write_meta(&pending.dir, &meta)?;
        let dir = self.root.join(format!("{}.{RECORDING_EXT}", pending.id));
        std::fs::rename(&pending.dir, &dir).with_context(|| format!("finalizing {}", dir.display()))?;
        load(&dir)
    }

    pub fn import_file(&self, path: &Path) -> Result<Item> {
        let img = image::open(path)
            .with_context(|| format!("{} is not a readable image", path.display()))?
            .to_rgba8();
        if img.width() == 0 || img.height() == 0 {
            bail!("{} has no pixels", path.display());
        }
        let title = path.file_stem().map(|s| s.to_string_lossy().into_owned());
        self.import_screenshot(&img, None, Local::now(), None, Some(path.to_path_buf()), title)
    }

    pub fn item_dir(&self, path: &Path) -> Option<PathBuf> {
        let rel = path.strip_prefix(&self.root).ok()?;
        let first = rel.components().next()?;
        Some(self.root.join(first))
    }

    pub fn scan(&self) -> Vec<Item> {
        let Ok(entries) = std::fs::read_dir(&self.root) else { return Vec::new() };
        let mut items: Vec<Item> = entries
            .flatten()
            .filter(|e| e.file_type().map(|t| t.is_dir()).unwrap_or(false))
            .filter(|e| !e.file_name().to_string_lossy().starts_with('.'))
            .filter_map(|e| load(&e.path()).ok())
            .collect();
        items.sort_by(|a, b| b.meta.created.cmp(&a.meta.created).then_with(|| b.id().cmp(a.id())));
        items
    }

    fn reserve(&self, created: &DateTime<Local>) -> Result<(String, PathBuf)> {
        for _ in 0..16 {
            let id = format!("{}_{:04x}", created.format("%Y-%m-%d_%H-%M-%S"), random_u16());
            if self.root.join(&id).exists() || self.root.join(format!("{id}.{RECORDING_EXT}")).exists() {
                continue;
            }
            let staging = self.root.join(format!(".{id}.tmp"));
            match std::fs::create_dir(&staging) {
                Ok(()) => return Ok((id, staging)),
                Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => continue,
                Err(e) => return Err(e).with_context(|| format!("creating {}", staging.display())),
            }
        }
        bail!("could not allocate a capture id")
    }
}

pub fn load(dir: &Path) -> Result<Item> {
    let text = std::fs::read_to_string(dir.join("meta.json"))
        .with_context(|| format!("reading {}", dir.join("meta.json").display()))?;
    let meta: CaptureMeta = serde_json::from_str(&text).with_context(|| format!("parsing {}", dir.display()))?;
    let is_rec = dir.extension().is_some_and(|e| e == RECORDING_EXT);
    if is_rec != (meta.kind == CaptureKind::Recording) {
        bail!("{} has kind {:?} in the wrong directory type", dir.display(), meta.kind);
    }
    Ok(Item { dir: dir.to_path_buf(), meta, bytes: dir_size(dir) })
}

pub fn load_edit(item: &Item) -> Result<(RgbaImage, Option<EditDoc>)> {
    let path = item.dir.join("base.png");
    let base = image::open(&path).with_context(|| format!("reading {}", path.display()))?.to_rgba8();
    let doc = match std::fs::read_to_string(item.dir.join("edit.json")) {
        Ok(json) => Some(EditDoc::from_json(&json).with_context(|| format!("parsing {}", item.dir.join("edit.json").display()))?),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => None,
        Err(e) => return Err(e).context("reading edit.json"),
    };
    Ok((base, doc))
}

pub fn save_edit(item: &Item, image: &RgbaImage, doc: &EditDoc) -> Result<Item> {
    let dir = &item.dir;
    let staged = [("image.png", dir.join("image.png.tmp")), ("edit.json", dir.join("edit.json.tmp")), ("thumb.png", dir.join("thumb.png.tmp"))];
    let written = (|| -> Result<()> {
        write_png(image, &staged[0].1)?;
        std::fs::write(&staged[1].1, doc.to_json()).with_context(|| format!("writing {}", staged[1].1.display()))?;
        write_png(&thumbnail(image), &staged[2].1)
    })();
    if let Err(e) = written {
        for (_, tmp) in &staged {
            let _ = std::fs::remove_file(tmp);
        }
        return Err(e);
    }
    for (name, tmp) in &staged {
        std::fs::rename(tmp, dir.join(name)).with_context(|| format!("saving {}", dir.join(name).display()))?;
    }
    let mut meta = item.meta.clone();
    meta.modified = Local::now();
    write_meta(dir, &meta)?;
    load(dir)
}

pub fn rename(item: &Item, title: &str) -> Result<Item> {
    let title = title.trim();
    if title.is_empty() {
        bail!("a title cannot be empty");
    }
    let mut meta = item.meta.clone();
    meta.title = title.to_string();
    meta.modified = Local::now();
    write_meta(&item.dir, &meta)?;
    load(&item.dir)
}

fn write_meta(dir: &Path, meta: &CaptureMeta) -> Result<()> {
    let tmp = dir.join("meta.json.tmp");
    std::fs::write(&tmp, serde_json::to_vec_pretty(meta)?).with_context(|| format!("writing {}", tmp.display()))?;
    std::fs::rename(&tmp, dir.join("meta.json")).with_context(|| format!("saving {}", dir.display()))?;
    Ok(())
}

pub fn thumbnail(img: &RgbaImage) -> RgbaImage {
    let (w, h) = (img.width(), img.height());
    let k = (THUMB_MAX as f32 / w.max(h) as f32).min(1.0);
    let (tw, th) = (((w as f32 * k).round() as u32).max(1), ((h as f32 * k).round() as u32).max(1));
    image::imageops::thumbnail(img, tw, th)
}

fn write_png(img: &RgbaImage, path: &Path) -> Result<()> {
    use image::codecs::png::{CompressionType, FilterType, PngEncoder};
    use image::ImageEncoder;
    let file = std::fs::File::create(path).with_context(|| format!("creating {}", path.display()))?;
    let enc = PngEncoder::new_with_quality(std::io::BufWriter::new(file), CompressionType::Fast, FilterType::Adaptive);
    enc.write_image(img.as_raw(), img.width(), img.height(), image::ExtendedColorType::Rgba8)
        .with_context(|| format!("encoding {}", path.display()))?;
    Ok(())
}

fn dir_size(dir: &Path) -> u64 {
    let Ok(entries) = std::fs::read_dir(dir) else { return 0 };
    entries
        .flatten()
        .map(|e| match e.file_type() {
            Ok(t) if t.is_dir() => dir_size(&e.path()),
            Ok(_) => e.metadata().map(|m| m.len()).unwrap_or(0),
            Err(_) => 0,
        })
        .sum()
}

fn random_u16() -> u16 {
    use std::hash::{BuildHasher, Hasher};
    let mut h = std::collections::hash_map::RandomState::new().build_hasher();
    h.write_u128(std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_nanos());
    h.finish() as u16
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    struct TempRoot(PathBuf);

    impl TempRoot {
        fn new(name: &str) -> Self {
            let p = std::env::temp_dir().join(format!("glint-lib-{name}-{:04x}", random_u16()));
            let _ = std::fs::remove_dir_all(&p);
            Self(p)
        }
    }

    impl Drop for TempRoot {
        fn drop(&mut self) {
            let _ = std::fs::remove_dir_all(&self.0);
        }
    }

    fn checker(w: u32, h: u32) -> RgbaImage {
        RgbaImage::from_fn(w, h, |x, y| if (x / 8 + y / 8) % 2 == 0 { image::Rgba([255, 0, 0, 255]) } else { image::Rgba([0, 0, 255, 255]) })
    }

    fn at(h: u32, m: u32, s: u32) -> DateTime<Local> {
        Local.with_ymd_and_hms(2026, 10, 14, h, m, s).unwrap()
    }

    #[test]
    fn import_writes_the_capture_package() {
        let root = TempRoot::new("import");
        let lib = Library::new(&root.0);
        let base = checker(1200, 600);
        let mut redacted = base.clone();
        redacted.put_pixel(0, 0, image::Rgba([0, 0, 0, 255]));
        let item = lib
            .import_screenshot(&base, Some((&redacted, &EditDoc::default())), at(14, 3, 22), Some("Display 1".into()), None, None)
            .unwrap();

        assert!(item.id().starts_with("2026-10-14_14-03-22_"), "id {}", item.id());
        assert_eq!(item.id().len(), "2026-10-14_14-03-22_xxxx".len());
        assert_eq!(item.meta.title, "Screenshot 2026-10-14 at 14.03.22");
        assert_eq!((item.meta.width, item.meta.height), (1200, 600));
        assert_eq!(item.meta.source.as_deref(), Some("Display 1"));
        assert_eq!(image::open(item.dir.join("base.png")).unwrap().to_rgba8(), base);
        assert_eq!(image::open(item.deliverable()).unwrap().to_rgba8(), redacted);
        let thumb = image::open(item.thumb()).unwrap();
        assert_eq!((thumb.width(), thumb.height()), (512, 256));
        assert!(item.is_edited(), "automatic redactions stay editable");
        let names: Vec<String> = std::fs::read_dir(&root.0).unwrap().flatten().map(|e| e.file_name().to_string_lossy().into_owned()).collect();
        assert_eq!(names, vec![item.id().to_string()], "no staging directory left behind");
    }

    #[test]
    fn import_file_copies_and_never_touches_the_original() {
        let root = TempRoot::new("file");
        let src_dir = TempRoot::new("src");
        std::fs::create_dir_all(&src_dir.0).unwrap();
        let src = src_dir.0.join("Diagram.jpg");
        image::DynamicImage::ImageRgba8(checker(64, 32)).to_rgb8().save(&src).unwrap();
        let before = std::fs::read(&src).unwrap();

        let item = Library::new(&root.0).import_file(&src).unwrap();
        assert_eq!(item.meta.title, "Diagram");
        assert_eq!(item.meta.imported_from.as_deref(), Some(src.as_path()));
        assert_eq!((item.meta.width, item.meta.height), (64, 32));
        assert_eq!(std::fs::read(&src).unwrap(), before);

        let bogus = src_dir.0.join("notes.png");
        std::fs::write(&bogus, b"not an image").unwrap();
        let err = Library::new(&root.0).import_file(&bogus).unwrap_err();
        assert!(err.to_string().contains("is not a readable image"), "{err}");
    }

    #[test]
    fn scan_lists_newest_first_with_recordings_and_skips_junk() {
        let root = TempRoot::new("scan");
        let lib = Library::new(&root.0);
        let img = checker(40, 40);
        let older = lib.import_screenshot(&img, None, at(9, 0, 0), None, None, None).unwrap();
        let newer = lib.import_screenshot(&img, None, at(18, 30, 0), None, None, None).unwrap();

        let rec_dir = root.0.join("2026-10-14_12-00-00_beef.glintrec");
        std::fs::create_dir_all(&rec_dir).unwrap();
        let rec_meta = CaptureMeta {
            version: 1,
            kind: CaptureKind::Recording,
            title: "Demo take".into(),
            created: at(12, 0, 0),
            modified: at(12, 5, 0),
            width: 2560,
            height: 1440,
            duration_ms: Some(42_000),
            source: Some("Display 1".into()),
            imported_from: None,
        };
        std::fs::write(rec_dir.join("meta.json"), serde_json::to_vec(&rec_meta).unwrap()).unwrap();
        std::fs::write(rec_dir.join("screen.mp4"), vec![0u8; 1000]).unwrap();
        std::fs::create_dir_all(root.0.join(".2026-10-14_13-00-00_dead.tmp")).unwrap();
        std::fs::create_dir_all(root.0.join("no-meta")).unwrap();

        let items = lib.scan();
        let titles: Vec<&str> = items.iter().map(|i| i.meta.title.as_str()).collect();
        assert_eq!(titles, vec![newer.meta.title.as_str(), "Demo take", older.meta.title.as_str()]);
        let rec = &items[1];
        assert_eq!(rec.kind(), CaptureKind::Recording);
        assert_eq!(rec.meta.duration_ms, Some(42_000));
        assert_eq!(rec.bytes, 1000 + std::fs::metadata(rec_dir.join("meta.json")).unwrap().len());
        assert_eq!(rec.deliverable(), rec_dir.join("screen.mp4"));
    }

    #[test]
    fn rename_changes_title_in_meta_only() {
        let root = TempRoot::new("rename");
        let lib = Library::new(&root.0);
        let img = checker(16, 16);
        let item = lib.import_screenshot(&img, None, Local.with_ymd_and_hms(2001, 2, 3, 10, 0, 0).unwrap(), None, None, None).unwrap();

        let renamed = rename(&item, "  Login bug  ").unwrap();
        assert_eq!(renamed.meta.title, "Login bug");
        assert_eq!(renamed.dir, item.dir);
        assert!(renamed.meta.modified > item.meta.modified);
        assert_eq!(lib.scan()[0].meta.title, "Login bug");
        assert_eq!(rename(&item, "   ").unwrap_err().to_string(), "a title cannot be empty");
        assert_eq!(lib.scan()[0].meta.title, "Login bug");
    }

    #[test]
    fn finished_recording_is_renamed_into_place_with_meta_and_thumb() {
        let root = TempRoot::new("rec");
        let lib = Library::new(&root.0);
        let pending = lib.begin_recording(at(15, 4, 5)).unwrap();
        assert!(pending.dir.file_name().unwrap().to_string_lossy().starts_with('.'), "staging is hidden");
        std::fs::write(pending.dir.join("screen.mp4"), vec![0u8; 300]).unwrap();
        assert!(lib.scan().is_empty(), "an unfinished recording is not in the Library");

        let info = RecordingInfo { width: 1280, height: 720, duration_ms: 4_250, source: "Display 1".into() };
        let item = lib.finish_recording(&pending, &info, Some(&checker(1280, 720))).unwrap();
        assert_eq!(item.dir, root.0.join(format!("{}.glintrec", pending.id)));
        assert!(!pending.dir.exists());
        assert_eq!(item.kind(), CaptureKind::Recording);
        assert_eq!(item.meta.title, "Recording 2026-10-14 at 15.04.05");
        assert_eq!((item.meta.width, item.meta.height, item.meta.duration_ms), (1280, 720, Some(4_250)));
        assert_eq!(item.meta.source.as_deref(), Some("Display 1"));
        let thumb = image::open(item.thumb()).unwrap();
        assert_eq!((thumb.width(), thumb.height()), (512, 288));
        assert_eq!(lib.scan(), vec![item]);
    }

    #[test]
    fn save_edit_replaces_the_deliverable_and_reopens_the_same_doc() {
        let root = TempRoot::new("edit");
        let lib = Library::new(&root.0);
        let base = checker(800, 400);
        let item = lib.import_screenshot(&base, None, at(8, 0, 0), None, None, None).unwrap();
        assert_eq!(load_edit(&item).unwrap(), (base.clone(), None));

        let mut doc = EditDoc::default();
        doc.window_title = Some("Login bug".into());
        doc.crop = Some(glint_render::annotate::R::new(10.0, 20.0, 300.0, 200.0));
        let framed = checker(1000, 600);
        let saved = save_edit(&item, &framed, &doc).unwrap();

        assert!(saved.is_edited());
        assert!(saved.meta.modified > item.meta.modified);
        assert_eq!(saved.meta.created, item.meta.created);
        assert_eq!(image::open(saved.deliverable()).unwrap().to_rgba8(), framed);
        let thumb = image::open(saved.thumb()).unwrap();
        assert_eq!((thumb.width(), thumb.height()), (512, 307));
        assert_eq!(load_edit(&saved).unwrap(), (base, Some(doc)));
        let mut names: Vec<String> = std::fs::read_dir(&saved.dir).unwrap().flatten().map(|e| e.file_name().to_string_lossy().into_owned()).collect();
        names.sort();
        assert_eq!(names, ["base.png", "edit.json", "image.png", "meta.json", "thumb.png"]);
    }

    #[test]
    fn item_dir_finds_the_capture_a_path_belongs_to() {
        let lib_root = || PathBuf::from(r"C:\Data\Library");
        let lib = Library::new(lib_root());
        let dir = lib_root().join("2026-10-14_14-03-22_ab12");
        assert_eq!(lib.item_dir(&dir), Some(dir.clone()));
        assert_eq!(lib.item_dir(&dir.join("image.png")), Some(dir.clone()));
        assert_eq!(lib.item_dir(&lib_root()), None);
        assert_eq!(lib.item_dir(Path::new(r"C:\Pictures\shot.png")), None);
    }
}
