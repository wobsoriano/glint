//! Which key presses count as shortcuts, and how they are labelled in `input.json`.

use windows::Win32::UI::Input::KeyboardAndMouse::*;

/// Modifier keys held at the moment of a key press.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Mods {
    pub ctrl: bool,
    pub shift: bool,
    pub alt: bool,
    pub win: bool,
}

impl Mods {
    /// Applies a modifier key going down or up. Returns false for non-modifier keys.
    pub fn apply(&mut self, vk: u32, down: bool) -> bool {
        let slot = match VIRTUAL_KEY(vk as u16) {
            VK_CONTROL | VK_LCONTROL | VK_RCONTROL => &mut self.ctrl,
            VK_SHIFT | VK_LSHIFT | VK_RSHIFT => &mut self.shift,
            VK_MENU | VK_LMENU | VK_RMENU => &mut self.alt,
            VK_LWIN | VK_RWIN => &mut self.win,
            _ => return false,
        };
        *slot = down;
        true
    }
}

/// The label for a key press, or `None` when it is plain typing that is not recorded.
///
/// Recorded: anything with Ctrl, Alt or Win held, and navigation/function keys on their own
/// or with Shift. Not recorded: letters, digits, punctuation and Space without Ctrl/Alt/Win.
pub(crate) fn shortcut_label(vk: u32, mods: Mods) -> Option<String> {
    let key = key_name(vk)?;
    let special = is_special(vk);
    if !(mods.ctrl || mods.alt || mods.win || special) {
        return None;
    }
    let mut parts = Vec::with_capacity(5);
    if mods.win {
        parts.push("Win");
    }
    if mods.ctrl {
        parts.push("Ctrl");
    }
    if mods.alt {
        parts.push("Alt");
    }
    if mods.shift {
        parts.push("Shift");
    }
    parts.push(&key);
    Some(parts.join("+"))
}

fn is_special(vk: u32) -> bool {
    let vk = VIRTUAL_KEY(vk as u16);
    matches!(
        vk,
        VK_RETURN | VK_ESCAPE | VK_TAB | VK_BACK | VK_DELETE | VK_INSERT | VK_HOME | VK_END | VK_PRIOR | VK_NEXT | VK_LEFT | VK_UP | VK_RIGHT | VK_DOWN | VK_SNAPSHOT
    ) || (VK_F1.0..=VK_F24.0).contains(&vk.0)
}

fn key_name(vk: u32) -> Option<String> {
    let v = VIRTUAL_KEY(vk as u16);
    let fixed = match v {
        VK_RETURN => "Enter",
        VK_ESCAPE => "Esc",
        VK_TAB => "Tab",
        VK_BACK => "Backspace",
        VK_DELETE => "Delete",
        VK_INSERT => "Insert",
        VK_HOME => "Home",
        VK_END => "End",
        VK_PRIOR => "PageUp",
        VK_NEXT => "PageDown",
        VK_LEFT => "Left",
        VK_UP => "Up",
        VK_RIGHT => "Right",
        VK_DOWN => "Down",
        VK_SPACE => "Space",
        VK_SNAPSHOT => "PrintScreen",
        VK_CONTROL | VK_LCONTROL | VK_RCONTROL | VK_SHIFT | VK_LSHIFT | VK_RSHIFT | VK_MENU | VK_LMENU | VK_RMENU | VK_LWIN | VK_RWIN => return None,
        VK_CAPITAL | VK_NUMLOCK | VK_SCROLL | VK_PAUSE | VK_APPS => return None,
        _ => "",
    };
    if !fixed.is_empty() {
        return Some(fixed.to_string());
    }
    if (VK_F1.0..=VK_F24.0).contains(&v.0) {
        return Some(format!("F{}", v.0 - VK_F1.0 + 1));
    }
    if (0x30..=0x39).contains(&vk) || (0x41..=0x5A).contains(&vk) {
        return Some((vk as u8 as char).to_string());
    }
    if (0x60..=0x69).contains(&vk) {
        return Some(format!("Num{}", vk - 0x60));
    }
    let ch = unsafe { MapVirtualKeyW(vk, MAPVK_VK_TO_CHAR) } & 0xFFFF;
    char::from_u32(ch).filter(|c| !c.is_control() && !c.is_whitespace()).map(|c| c.to_uppercase().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    const CTRL: Mods = Mods { ctrl: true, shift: false, alt: false, win: false };
    const CTRL_SHIFT: Mods = Mods { ctrl: true, shift: true, alt: false, win: false };
    const SHIFT: Mods = Mods { ctrl: false, shift: true, alt: false, win: false };
    const NONE: Mods = Mods { ctrl: false, shift: false, alt: false, win: false };

    #[test]
    fn modifier_combos_are_labelled() {
        assert_eq!(shortcut_label(b'S' as u32, CTRL_SHIFT).as_deref(), Some("Ctrl+Shift+S"));
        assert_eq!(shortcut_label(b'C' as u32, CTRL).as_deref(), Some("Ctrl+C"));
        assert_eq!(shortcut_label(VK_DELETE.0 as u32, Mods { ctrl: true, alt: true, ..NONE }).as_deref(), Some("Ctrl+Alt+Delete"));
        assert_eq!(shortcut_label(b'S' as u32, Mods { win: true, shift: true, ..NONE }).as_deref(), Some("Win+Shift+S"));
        assert_eq!(shortcut_label(VK_F5.0 as u32, NONE).as_deref(), Some("F5"));
        assert_eq!(shortcut_label(VK_TAB.0 as u32, SHIFT).as_deref(), Some("Shift+Tab"));
        assert_eq!(shortcut_label(VK_RETURN.0 as u32, NONE).as_deref(), Some("Enter"));
        assert_eq!(shortcut_label(VK_ESCAPE.0 as u32, NONE).as_deref(), Some("Esc"));
        assert_eq!(shortcut_label(VK_BACK.0 as u32, NONE).as_deref(), Some("Backspace"));
        assert_eq!(shortcut_label(VK_LEFT.0 as u32, NONE).as_deref(), Some("Left"));
    }

    #[test]
    fn plain_typing_and_lone_modifiers_are_not_recorded() {
        assert_eq!(shortcut_label(b'A' as u32, NONE), None);
        assert_eq!(shortcut_label(b'A' as u32, SHIFT), None);
        assert_eq!(shortcut_label(b'1' as u32, NONE), None);
        assert_eq!(shortcut_label(VK_SPACE.0 as u32, NONE), None);
        assert_eq!(shortcut_label(VK_LCONTROL.0 as u32, CTRL), None);
        assert_eq!(shortcut_label(VK_LSHIFT.0 as u32, SHIFT), None);
        assert_eq!(shortcut_label(VK_LWIN.0 as u32, Mods { win: true, ..NONE }), None);
    }

    #[test]
    fn modifier_state_tracks_down_and_up() {
        let mut m = Mods::default();
        assert!(m.apply(VK_LCONTROL.0 as u32, true));
        assert!(m.apply(VK_RSHIFT.0 as u32, true));
        assert_eq!(m, CTRL_SHIFT);
        assert!(!m.apply(b'S' as u32, true), "S is not a modifier");
        assert!(m.apply(VK_LCONTROL.0 as u32, false));
        assert_eq!(m, SHIFT);
    }
}
