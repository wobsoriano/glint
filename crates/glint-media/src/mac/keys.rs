//! Shortcut labels for the input timeline, written the Mac way (`⇧⌘S`).

use objc2_core_graphics::{CGEvent, CGEventFlags};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct Mods {
    pub cmd: bool,
    pub ctrl: bool,
    pub alt: bool,
    pub shift: bool,
}

impl Mods {
    pub fn from_flags(flags: CGEventFlags) -> Self {
        Self {
            cmd: flags.contains(CGEventFlags::MaskCommand),
            ctrl: flags.contains(CGEventFlags::MaskControl),
            alt: flags.contains(CGEventFlags::MaskAlternate),
            shift: flags.contains(CGEventFlags::MaskShift),
        }
    }
}

/// The label for a key press, or `None` for typing. A press counts with ⌘ or ⌃ held, or for
/// a special key. Option alone types characters (⌥S is ß), so it only counts with special keys.
/// `typed` is the key's character on the current layout, without modifiers.
pub(crate) fn shortcut_label(keycode: u16, mods: Mods, typed: Option<char>) -> Option<String> {
    let special = special_name(keycode);
    if !(mods.cmd || mods.ctrl || special.is_some()) {
        return None;
    }
    let key = match special {
        Some(name) => name.to_string(),
        None if is_modifier(keycode) => return None,
        None if keycode == SPACE => "Space".to_string(),
        None => typed.filter(|c| !c.is_control() && !c.is_whitespace())?.to_uppercase().to_string(),
    };
    let mut label = String::new();
    for (on, symbol) in [(mods.ctrl, "⌃"), (mods.alt, "⌥"), (mods.shift, "⇧"), (mods.cmd, "⌘")] {
        if on {
            label.push_str(symbol);
        }
    }
    Some(label + &key)
}

const SPACE: u16 = 0x31;

fn is_modifier(keycode: u16) -> bool {
    matches!(keycode, 0x36..=0x3F)
}

fn special_name(keycode: u16) -> Option<&'static str> {
    Some(match keycode {
        0x24 => "↩",
        0x4C => "⌤",
        0x30 => "⇥",
        0x33 => "⌫",
        0x35 => "⎋",
        0x75 => "⌦",
        0x73 => "↖",
        0x77 => "↘",
        0x74 => "⇞",
        0x79 => "⇟",
        0x7B => "←",
        0x7C => "→",
        0x7D => "↓",
        0x7E => "↑",
        0x7A => "F1",
        0x78 => "F2",
        0x63 => "F3",
        0x76 => "F4",
        0x60 => "F5",
        0x61 => "F6",
        0x62 => "F7",
        0x64 => "F8",
        0x65 => "F9",
        0x6D => "F10",
        0x67 => "F11",
        0x6F => "F12",
        0x69 => "F13",
        0x6B => "F14",
        0x71 => "F15",
        0x6A => "F16",
        0x40 => "F17",
        0x4F => "F18",
        0x50 => "F19",
        0x5A => "F20",
        _ => return None,
    })
}

/// The character `keycode` types on the current keyboard layout with no modifiers held.
pub(crate) fn typed_char(keycode: u16) -> Option<char> {
    let event = CGEvent::new_keyboard_event(None, keycode, true)?;
    CGEvent::set_flags(Some(&event), CGEventFlags(0));
    let mut buf = [0u16; 4];
    let mut len = 0;
    unsafe { CGEvent::keyboard_get_unicode_string(Some(&event), buf.len() as _, &mut len, buf.as_mut_ptr()) };
    char::decode_utf16(buf[..len as usize].iter().copied()).next()?.ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    const NONE: Mods = Mods { cmd: false, ctrl: false, alt: false, shift: false };
    const CMD: Mods = Mods { cmd: true, ..NONE };
    const CMD_SHIFT: Mods = Mods { cmd: true, shift: true, ..NONE };
    const ALT: Mods = Mods { alt: true, ..NONE };
    const S: u16 = 0x01;

    #[test]
    fn modifier_combos_are_labelled_in_mac_order() {
        assert_eq!(shortcut_label(S, CMD_SHIFT, Some('s')).as_deref(), Some("⇧⌘S"));
        assert_eq!(shortcut_label(S, CMD, Some('s')).as_deref(), Some("⌘S"));
        assert_eq!(shortcut_label(S, Mods { ctrl: true, alt: true, shift: true, cmd: true }, Some('s')).as_deref(), Some("⌃⌥⇧⌘S"));
        assert_eq!(shortcut_label(0x31, CMD, Some(' ')).as_deref(), Some("⌘Space"));
        assert_eq!(shortcut_label(0x30, Mods { shift: true, ..NONE }, None).as_deref(), Some("⇧⇥"));
        assert_eq!(shortcut_label(0x24, NONE, None).as_deref(), Some("↩"));
        assert_eq!(shortcut_label(0x60, NONE, None).as_deref(), Some("F5"));
        assert_eq!(shortcut_label(0x7B, ALT, None).as_deref(), Some("⌥←"), "Option with a special key counts");
    }

    #[test]
    fn typing_and_lone_modifiers_are_not_recorded() {
        assert_eq!(shortcut_label(S, NONE, Some('s')), None);
        assert_eq!(shortcut_label(S, Mods { shift: true, ..NONE }, Some('s')), None);
        assert_eq!(shortcut_label(S, ALT, Some('s')), None, "⌥S types ß");
        assert_eq!(shortcut_label(0x31, NONE, Some(' ')), None);
        assert_eq!(shortcut_label(0x37, CMD, None), None, "Command itself");
        assert_eq!(shortcut_label(0x38, CMD_SHIFT, None), None, "Shift while Command is down");
    }
}
