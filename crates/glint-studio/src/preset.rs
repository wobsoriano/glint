use crate::project::{Design, Project};
use anyhow::Context;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

pub const PRESET_VERSION: u32 = 1;
const DEFAULT_FILE: &str = "default.txt";

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Preset {
    #[serde(default = "preset_version")]
    pub version: u32,
    pub name: String,
    #[serde(default)]
    pub design: Design,
}

fn preset_version() -> u32 {
    PRESET_VERSION
}

impl Preset {
    pub fn new(name: &str, design: Design) -> Self {
        Self { version: PRESET_VERSION, name: name.trim().to_string(), design }
    }

    pub fn apply(&self, project: &mut Project) {
        project.design = self.design.clone();
    }
}

pub struct PresetStore {
    dir: PathBuf,
}

impl PresetStore {
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self { dir: dir.into() }
    }

    /// All readable presets sorted by name. Unreadable files are skipped.
    pub fn list(&self) -> Vec<Preset> {
        let Ok(entries) = std::fs::read_dir(&self.dir) else { return Vec::new() };
        let mut out: Vec<Preset> = entries
            .filter_map(|e| {
                let path = e.ok()?.path();
                (path.extension()? == "json").then_some(())?;
                serde_json::from_str(&std::fs::read_to_string(path).ok()?).ok()
            })
            .collect();
        out.sort_by_key(|p| p.name.to_lowercase());
        out
    }

    /// Saves a preset, replacing one with the same name.
    pub fn save(&self, preset: &Preset) -> anyhow::Result<()> {
        anyhow::ensure!(!slug(&preset.name).is_empty(), "preset name is empty");
        std::fs::create_dir_all(&self.dir)
            .with_context(|| format!("creating {}", self.dir.display()))?;
        let path = self.path(&preset.name);
        std::fs::write(&path, serde_json::to_string_pretty(preset)?)
            .with_context(|| format!("writing {}", path.display()))
    }

    pub fn load(&self, name: &str) -> anyhow::Result<Preset> {
        let path = self.path(name);
        let json =
            std::fs::read_to_string(&path).with_context(|| format!("reading {}", path.display()))?;
        serde_json::from_str(&json).with_context(|| format!("parsing {}", path.display()))
    }

    pub fn delete(&self, name: &str) -> anyhow::Result<()> {
        if self.default_name().as_deref() == Some(name) {
            self.set_default(None)?;
        }
        let path = self.path(name);
        std::fs::remove_file(&path).with_context(|| format!("deleting {}", path.display()))
    }

    /// Chooses the preset applied to new recordings; `None` clears it.
    pub fn set_default(&self, name: Option<&str>) -> anyhow::Result<()> {
        let path = self.dir.join(DEFAULT_FILE);
        match name {
            Some(n) => {
                std::fs::create_dir_all(&self.dir)?;
                std::fs::write(&path, n).with_context(|| format!("writing {}", path.display()))
            }
            None => match std::fs::remove_file(&path) {
                Err(e) if e.kind() != std::io::ErrorKind::NotFound => {
                    Err(e).with_context(|| format!("deleting {}", path.display()))
                }
                _ => Ok(()),
            },
        }
    }

    /// The default preset, if one is set and still exists.
    pub fn default_preset(&self) -> Option<Preset> {
        self.load(&self.default_name()?).ok()
    }

    fn default_name(&self) -> Option<String> {
        let n = std::fs::read_to_string(self.dir.join(DEFAULT_FILE)).ok()?;
        Some(n.trim().to_string()).filter(|n| !n.is_empty())
    }

    fn path(&self, name: &str) -> PathBuf {
        self.dir.join(format!("{}.json", slug(name)))
    }
}

fn slug(name: &str) -> String {
    let mut s = String::new();
    for c in name.trim().chars() {
        if c.is_alphanumeric() {
            s.extend(c.to_lowercase());
        } else if !s.ends_with('-') && !s.is_empty() {
            s.push('-');
        }
    }
    s.trim_end_matches('-').to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::project::CameraSettings;
    use glint_render::style::{Background, Color};

    #[test]
    fn presets_round_trip_and_default_applies_to_new_projects() {
        let dir = std::env::temp_dir().join(format!("glint-presets-{}", std::process::id()));
        std::fs::remove_dir_all(&dir).ok();
        let store = PresetStore::new(&dir);
        assert_eq!(store.list(), vec![]);

        let mut design = Design::default();
        design.background = Background::Solid(Color::rgb(0x112233));
        design.padding = 0.1;
        design.cursor.size = 2.5;
        design.camera = CameraSettings { visible: false, center: [0.1, 0.2], size: 0.3, roundness: 0.1 };
        let dark = Preset::new("  Dark Demo! ", design.clone());
        store.save(&dark).unwrap();
        store.save(&Preset::new("Bright", Design::default())).unwrap();

        assert_eq!(dir.join("dark-demo.json").exists(), true);
        let names: Vec<String> = store.list().into_iter().map(|p| p.name).collect();
        assert_eq!(names, vec!["Bright".to_string(), "Dark Demo!".to_string()]);
        assert_eq!(store.load("Dark Demo!").unwrap().design, design);

        assert_eq!(store.default_preset(), None);
        store.set_default(Some("Dark Demo!")).unwrap();
        let mut project = Project::new(1000.0);
        store.default_preset().unwrap().apply(&mut project);
        assert_eq!(project.design.padding, 0.1);
        assert_eq!(project.design.camera.center, [0.1, 0.2]);

        store.delete("Dark Demo!").unwrap();
        assert_eq!(store.default_preset(), None);
        assert_eq!(store.list().len(), 1);
        std::fs::remove_dir_all(&dir).ok();
    }
}
