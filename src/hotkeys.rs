use crate::settings::{HotkeyAction, Settings};
use global_hotkey::hotkey::HotKey;
use global_hotkey::GlobalHotKeyManager;
use gpui::{App, Global};

pub struct Hotkeys {
    manager: Option<GlobalHotKeyManager>,
    bindings: Vec<(HotKey, HotkeyAction)>,
    pub failures: Vec<(HotkeyAction, String)>,
}

impl Global for Hotkeys {}

fn parse(accel: &str) -> Result<HotKey, String> {
    accel.parse::<HotKey>().map_err(|e| format!("{accel} is not a valid shortcut ({e})"))
}

impl Hotkeys {
    pub fn new() -> Self {
        Self { manager: GlobalHotKeyManager::new().ok(), bindings: Vec::new(), failures: Vec::new() }
    }

    pub fn register_all(&mut self, settings: &Settings) {
        let Some(manager) = &self.manager else { return };
        for (hk, _) in self.bindings.drain(..) {
            let _ = manager.unregister(hk);
        }
        self.failures.clear();
        for action in HotkeyAction::ALL {
            let accel = settings.hotkeys.get(action);
            if accel.is_empty() {
                continue;
            }
            match parse(accel).and_then(|hk| manager.register(hk).map(|_| hk).map_err(|e| e.to_string())) {
                Ok(hk) => self.bindings.push((hk, action)),
                Err(e) => self.failures.push((action, e)),
            }
        }
    }

    pub fn suspend(&mut self) {
        let Some(manager) = &self.manager else { return };
        for (hk, _) in self.bindings.drain(..) {
            let _ = manager.unregister(hk);
        }
    }

    pub fn action_for(&self, id: u32) -> Option<HotkeyAction> {
        self.bindings.iter().find(|(hk, _)| hk.id() == id).map(|(_, a)| *a)
    }

    fn rebind(&mut self, action: HotkeyAction, accel: &str) -> Result<(), String> {
        let manager = self.manager.as_ref().ok_or("global shortcuts are unavailable on this system")?;
        let new = parse(accel)?;
        if let Some((_, other)) = self.bindings.iter().find(|(hk, a)| hk.id() == new.id() && *a != action) {
            return Err(format!("{accel} is already used for \u{201c}{}\u{201d}", other.label()));
        }
        let old = self.bindings.iter().position(|(_, a)| *a == action).map(|i| self.bindings.remove(i).0);
        if let Some(old) = old {
            if old.id() == new.id() {
                self.bindings.push((old, action));
                return Ok(());
            }
            let _ = manager.unregister(old);
        }
        match manager.register(new) {
            Ok(()) => {
                self.bindings.push((new, action));
                self.failures.retain(|(a, _)| *a != action);
                Ok(())
            }
            Err(e) => {
                if let Some(old) = old {
                    if manager.register(old).is_ok() {
                        self.bindings.push((old, action));
                    }
                }
                Err(format!("{accel} could not be registered, another app probably uses it ({e})"))
            }
        }
    }
}

pub fn rebind(action: HotkeyAction, accel: &str, cx: &mut App) -> Result<(), String> {
    cx.global_mut::<Hotkeys>().rebind(action, accel)?;
    let s = cx.global_mut::<Settings>();
    *s.hotkeys.get_mut(action) = accel.to_string();
    s.save();
    crate::refresh_tray(cx);
    Ok(())
}

pub fn accelerator(k: &gpui::Keystroke) -> Result<Option<String>, String> {
    let key = match k.key.as_str() {
        "control" | "shift" | "alt" | "platform" | "function" | "" => return Ok(None),
        key if !key.is_ascii() => return Err(format!("{key} cannot be used in a global shortcut")),
        key => {
            let mut chars = key.chars();
            chars.next().into_iter().map(|c| c.to_ascii_uppercase()).chain(chars).collect::<String>()
        }
    };
    let m = &k.modifiers;
    let function_key = key.starts_with('F') && key.len() > 1 && key[1..].chars().all(|c| c.is_ascii_digit());
    if !(m.control || m.alt || m.platform || function_key || key == "Printscreen") {
        return Err(if cfg!(target_os = "macos") { "Include ⌃, ⌥ or ⌘ in the shortcut" } else { "Include Ctrl, Alt or Win in the shortcut" }.into());
    }
    let mut parts = Vec::new();
    if m.control {
        parts.push("Ctrl");
    }
    if m.alt {
        parts.push("Alt");
    }
    if m.shift {
        parts.push("Shift");
    }
    if m.platform {
        parts.push("Super");
    }
    let accel = format!("{}{}{key}", parts.join("+"), if parts.is_empty() { "" } else { "+" });
    parse(&accel).map(|_| Some(accel)).map_err(|_| format!("{key} cannot be used in a global shortcut"))
}

/// A global shortcut as the platform writes it: `Ctrl+Shift+4` on Windows, `⌃⇧4` on macOS.
pub fn display(accel: &str) -> String {
    if cfg!(target_os = "macos") {
        crate::ui::mac_chords(accel, "⌃")
    } else {
        accel.replace("Super", "Win")
    }
}

/// A global shortcut as a recording's input track labels the key press, so recordings can
/// leave Glint's own shortcuts out of their keystroke captions.
pub fn recorded_label(accel: &str) -> String {
    if cfg!(target_os = "macos") {
        return crate::ui::mac_chords(accel, "⌃");
    }
    let parts: Vec<&str> = accel.split('+').collect();
    let Some((key, mods)) = parts.split_last() else { return String::new() };
    let key = if key.eq_ignore_ascii_case("Printscreen") { "PrintScreen" } else { key };
    let mut out: Vec<&str> = [("Win", &["Super", "Win"][..]), ("Ctrl", &["Ctrl"]), ("Alt", &["Alt"]), ("Shift", &["Shift"])]
        .into_iter()
        .filter(|(_, names)| mods.iter().any(|m| names.contains(m)))
        .map(|(label, _)| label)
        .collect();
    out.push(key);
    out.join("+")
}

#[cfg(test)]
mod tests {
    use super::accelerator;

    #[test]
    #[cfg(not(target_os = "macos"))]
    fn recorded_labels_follow_the_input_track_order() {
        use super::recorded_label;
        assert_eq!(recorded_label("Ctrl+Shift+7"), "Ctrl+Shift+7");
        assert_eq!(recorded_label("Ctrl+Shift+Super+L"), "Win+Ctrl+Shift+L");
        assert_eq!(recorded_label("Printscreen"), "PrintScreen");
    }

    fn key(s: &str) -> gpui::Keystroke {
        gpui::Keystroke::parse(s).unwrap()
    }

    #[test]
    fn builds_accelerators_from_key_presses() {
        assert_eq!(accelerator(&key("ctrl-shift-4")), Ok(Some("Ctrl+Shift+4".into())));
        assert_eq!(accelerator(&key("ctrl-alt-l")), Ok(Some("Ctrl+Alt+L".into())));
        assert_eq!(accelerator(&key("f9")), Ok(Some("F9".into())));
        assert!(accelerator(&key("shift-k")).is_err_and(|e| e.starts_with("Include ")), "a modifier is required");
        assert_eq!(accelerator(&key("ctrl-a")), Ok(Some("Ctrl+A".into())));
        assert_eq!(accelerator(&key("ctrl-up")), Ok(Some("Ctrl+Up".into())));
    }

    #[test]
    fn rejects_non_ascii_keys() {
        let k = gpui::Keystroke {
            modifiers: gpui::Modifiers { control: true, ..Default::default() },
            key: "ä".into(),
            key_char: None,
        };
        assert_eq!(accelerator(&k), Err("ä cannot be used in a global shortcut".into()));
    }
}
