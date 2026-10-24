use crate::doc::{EditDoc, Effects};
use crate::style::{Background, Style};
use anyhow::{bail, ensure, Context as _};
use serde::{Deserialize, Serialize};
use std::path::Path;

pub const FORMAT: &str = "glint-presets";
pub const VERSION: u32 = 1;
pub const EXTENSION: &str = "glintpreset";
const MAX_FILE_BYTES: usize = 1_000_000;
const MAX_PRESETS: usize = 100;
const OMITTED_IMAGE: &str = "background_image";

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Preset {
    pub name: String,
    pub style: Style,
    #[serde(default)]
    pub effects: Effects,
}

impl Preset {
    pub fn from_doc(name: impl Into<String>, doc: &EditDoc) -> Self {
        Self { name: name.into(), style: doc.style.clone(), effects: doc.effects.clone() }
    }

    /// Applies the look; crop and annotations are untouched.
    pub fn apply(&self, doc: &mut EditDoc) {
        doc.style = self.style.clone();
        doc.effects = self.effects.clone();
    }
}

#[derive(Serialize, Deserialize)]
struct PresetFile {
    format: String,
    version: u32,
    presets: Vec<PortablePreset>,
}

#[derive(Serialize, Deserialize)]
struct PortablePreset {
    name: String,
    style: Style,
    #[serde(default)]
    effects: Effects,
    #[serde(default)]
    omitted: Vec<String>,
}

fn portable_style(style: &Style) -> (Style, Vec<String>) {
    let mut style = style.clone();
    let mut omitted = Vec::new();
    if matches!(style.background, Background::Image(_)) {
        style.background = Background::None;
        omitted.push(OMITTED_IMAGE.to_string());
    }
    (style, omitted)
}

/// Serializes presets as a `.glintpreset` document. Local image paths are never written.
pub fn export(presets: &[Preset]) -> String {
    let file = PresetFile {
        format: FORMAT.into(),
        version: VERSION,
        presets: presets
            .iter()
            .map(|p| {
                let (style, omitted) = portable_style(&p.style);
                PortablePreset { name: p.name.clone(), style, effects: p.effects.clone(), omitted }
            })
            .collect(),
    };
    serde_json::to_string_pretty(&file).expect("presets serialize")
}

pub fn import(json: &str) -> anyhow::Result<Vec<Preset>> {
    ensure!(!json.trim().is_empty(), "The preset file is empty");
    ensure!(json.len() <= MAX_FILE_BYTES, "The preset file is too large");
    let file: PresetFile = serde_json::from_str(json).context("Not a valid preset file")?;
    ensure!(file.format == FORMAT, "Not a Glint preset file");
    if file.version > VERSION {
        bail!("This preset file needs a newer version of Glint (format {})", file.version);
    }
    ensure!(!file.presets.is_empty(), "The preset file has no presets");
    ensure!(file.presets.len() <= MAX_PRESETS, "The preset file has too many presets");
    Ok(file
        .presets
        .into_iter()
        .map(|p| Preset { name: p.name.trim().chars().take(80).collect(), style: portable_style(&p.style).0, effects: p.effects })
        .collect())
}

/// Reads the user's saved presets; a missing file is an empty list.
pub fn load_store(path: &Path) -> anyhow::Result<Vec<Preset>> {
    match std::fs::read_to_string(path) {
        Ok(s) => serde_json::from_str(&s).with_context(|| format!("reading {}", path.display())),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
        Err(e) => Err(e).with_context(|| format!("reading {}", path.display())),
    }
}

pub fn save_store(path: &Path, presets: &[Preset]) -> anyhow::Result<()> {
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir).with_context(|| format!("creating {}", dir.display()))?;
    }
    let json = serde_json::to_string_pretty(presets)?;
    let tmp = path.with_extension("tmp");
    std::fs::write(&tmp, json).with_context(|| format!("writing {}", tmp.display()))?;
    std::fs::rename(&tmp, path).with_context(|| format!("writing {}", path.display()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doc::{Border, Perspective};
    use crate::style::{Color, Gradient};

    #[test]
    fn export_drops_local_image_paths_and_import_falls_back_to_no_background() {
        let mut with_image = Preset::from_doc("Beach", &EditDoc::default());
        with_image.style.background = Background::Image("C:\\Users\\me\\Pictures\\beach.jpg".into());
        with_image.style.padding = 48.0;
        with_image.effects.border = Border { enabled: true, width: 3.0, color: Color::rgb(0xABCDEF) };
        let mut gradient = Preset::from_doc("Dusk", &EditDoc::default());
        gradient.style.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x111111), Color::rgb(0x999999)], angle: 45.0 });
        gradient.effects.perspective = Perspective { rot_y: 18.0, ..Default::default() };

        let json = export(&[with_image.clone(), gradient.clone()]);
        assert!(!json.contains("beach.jpg") && !json.contains("Pictures"), "{json}");
        assert!(json.contains("\"format\": \"glint-presets\"") && json.contains("\"version\": 1"));
        assert!(json.contains("background_image"));

        let back = import(&json).unwrap();
        assert_eq!(back[0].style.background, Background::None);
        assert_eq!(back[0].style.padding, 48.0);
        assert_eq!(back[0].effects, with_image.effects);
        assert_eq!(back[1], gradient);
    }

    #[test]
    #[ignore = "writes presets.json fixtures for the Go port into GLINT_GO_FIXTURES"]
    fn go_fixtures() {
        let dir = std::path::PathBuf::from(std::env::var_os("GLINT_GO_FIXTURES").expect("set GLINT_GO_FIXTURES"));
        let mut dusk = Preset::from_doc("Dusk \u{25d0}", &EditDoc::default());
        dusk.style.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x111111), Color::rgb(0x999999)], angle: 45.0 });
        dusk.style.padding = 48.5;
        dusk.effects.border = Border { enabled: true, width: 3.0, color: Color::rgb(0xABCDEF) };
        dusk.effects.perspective = Perspective { rot_y: 18.0, ..Default::default() };
        let mut photo = Preset::from_doc("Photo", &EditDoc::default());
        photo.style.background = Background::Image("C:\\Users\\me\\Pictures\\beach.jpg".into());
        save_store(&dir.join("presets.json"), &[Preset::from_doc("Plain", &EditDoc::default()), dusk, photo]).unwrap();
        assert_eq!(load_store(&dir.join("presets.json")).unwrap().len(), 3);
        let no_effects = r#"[{"name":"Bare","style":{}}]"#;
        let decoded = serde_json::from_str::<Vec<Preset>>(no_effects).unwrap();
        assert_eq!(decoded[0].effects, Effects::default());
        std::fs::write(dir.join("presets-no-effects.json"), no_effects).unwrap();
        assert!(serde_json::from_str::<Vec<Preset>>(r#"[{"name":"NoStyle"}]"#).is_err());
        std::fs::write(dir.join("presets-reject-no-style.json"), r#"[{"name":"NoStyle"}]"#).unwrap();
    }

    #[test]
    #[ignore = "reads presets.json written by the Go port from GLINT_GO_WRITTEN"]
    fn reads_go_written_presets() {
        let dir = std::path::PathBuf::from(std::env::var_os("GLINT_GO_WRITTEN").expect("set GLINT_GO_WRITTEN"));
        let text = std::fs::read_to_string(dir.join("presets.json")).unwrap();
        let presets = load_store(&dir.join("presets.json")).unwrap();
        assert!(!presets.is_empty());
        assert_eq!(serde_json::to_string_pretty(&presets).unwrap(), text, "presets.json round-trips byte for byte");
    }

    #[test]
    fn import_rejects_foreign_and_future_files_and_strips_paths() {
        assert!(import("").is_err());
        assert!(import("{}").is_err());
        assert!(import(r#"{"format":"other","version":1,"presets":[]}"#).is_err());
        let future = export(&[Preset::from_doc("A", &EditDoc::default())]).replace("\"version\": 1", "\"version\": 7");
        assert!(import(&future).unwrap_err().to_string().contains("newer"));
        let mut p = Preset::from_doc("Sneaky", &EditDoc::default());
        p.style.background = Background::Image("C:\\secret.png".into());
        let hand_edited = serde_json::json!({
            "format": FORMAT, "version": 1,
            "presets": [{ "name": "Sneaky", "style": p.style }]
        });
        assert_eq!(import(&hand_edited.to_string()).unwrap()[0].style.background, Background::None);
    }
}
