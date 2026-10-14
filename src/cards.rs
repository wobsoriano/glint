//! Which actions a preview card shows and where: four corner slots, up to three center
//! buttons, and a hidden tray. Every action lives in exactly one place.

use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum CardAction {
    Copy,
    Save,
    SaveAs,
    Compress,
    Pin,
    QuickLook,
    Edit,
    CopyText,
    Delete,
    Dismiss,
}

impl CardAction {
    pub const ALL: [CardAction; 10] = [
        CardAction::Copy,
        CardAction::Save,
        CardAction::SaveAs,
        CardAction::Compress,
        CardAction::Pin,
        CardAction::QuickLook,
        CardAction::Edit,
        CardAction::CopyText,
        CardAction::Delete,
        CardAction::Dismiss,
    ];

    pub fn label(self) -> &'static str {
        match self {
            CardAction::Copy => "Copy",
            CardAction::Save => "Save",
            CardAction::SaveAs => "Save As",
            CardAction::Compress => "Compress",
            CardAction::Pin => "Pin",
            CardAction::QuickLook => "Quick Look",
            CardAction::Edit => "Edit",
            CardAction::CopyText => "Copy Text",
            CardAction::Delete => "Delete",
            CardAction::Dismiss => "Dismiss",
        }
    }

    pub fn icon(self) -> &'static str {
        match self {
            CardAction::Copy => "copy",
            CardAction::Save => "save",
            CardAction::SaveAs => "save-as",
            CardAction::Compress => "compress",
            CardAction::Pin => "pin",
            CardAction::QuickLook => "eye",
            CardAction::Edit => "edit",
            CardAction::CopyText => "scan",
            CardAction::Delete => "trash",
            CardAction::Dismiss => "close",
        }
    }

    pub fn help(self) -> &'static str {
        match self {
            CardAction::Copy => "Copy the image to the clipboard",
            CardAction::Save => "Save to the export folder",
            CardAction::SaveAs => "Save to a location you choose",
            CardAction::Compress => "Save a smaller JPEG and copy it as a file",
            CardAction::Pin => "Pin above other windows",
            CardAction::QuickLook => "Open a large preview",
            CardAction::Edit => "Open in the editor",
            CardAction::CopyText => "Copy recognized text (on-device OCR)",
            CardAction::Delete => "Move the capture to the Recycle Bin",
            CardAction::Dismiss => "Close the card and keep the capture",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum Corner {
    TopLeft,
    TopRight,
    BottomLeft,
    BottomRight,
}

impl Corner {
    pub const ALL: [Corner; 4] = [Corner::TopLeft, Corner::TopRight, Corner::BottomLeft, Corner::BottomRight];
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Zone {
    Corner(Corner),
    Center,
    Tray,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct CardLayout {
    pub corners: [Option<CardAction>; 4],
    pub center: Vec<CardAction>,
    pub hidden: Vec<CardAction>,
}

impl Default for CardLayout {
    fn default() -> Self {
        Self {
            corners: [Some(CardAction::Delete), Some(CardAction::Dismiss), Some(CardAction::Edit), Some(CardAction::QuickLook)],
            center: vec![CardAction::Copy, CardAction::Save, CardAction::Pin],
            hidden: vec![CardAction::SaveAs, CardAction::Compress, CardAction::CopyText],
        }
    }
}

impl CardLayout {
    pub const CENTER_CAPACITY: usize = 3;

    pub fn corner(&self, c: Corner) -> Option<CardAction> {
        self.corners[c as usize]
    }

    pub fn zone_of(&self, a: CardAction) -> Option<Zone> {
        if let Some(c) = Corner::ALL.into_iter().find(|&c| self.corner(c) == Some(a)) {
            return Some(Zone::Corner(c));
        }
        if self.center.contains(&a) {
            return Some(Zone::Center);
        }
        self.hidden.contains(&a).then_some(Zone::Tray)
    }

    fn remove(&mut self, a: CardAction) {
        for slot in &mut self.corners {
            if *slot == Some(a) {
                *slot = None;
            }
        }
        self.center.retain(|&x| x != a);
        self.hidden.retain(|&x| x != a);
    }

    /// Moves `a` into `zone`. Dropping on an occupied corner swaps: the occupant goes
    /// where `a` came from. A full center pushes its last button into the tray.
    pub fn place(&mut self, a: CardAction, zone: Zone, center_index: Option<usize>) {
        let origin = self.zone_of(a).unwrap_or(Zone::Tray);
        if origin == zone && zone != Zone::Center {
            return;
        }
        let origin_index = self.center.iter().position(|&x| x == a);
        self.remove(a);
        match zone {
            Zone::Corner(c) => {
                if let Some(occupant) = self.corners[c as usize].take() {
                    match origin {
                        Zone::Corner(o) => self.corners[o as usize] = Some(occupant),
                        Zone::Center => self.center.insert(origin_index.unwrap_or(self.center.len()).min(self.center.len()), occupant),
                        Zone::Tray => self.hidden.push(occupant),
                    }
                }
                self.corners[c as usize] = Some(a);
            }
            Zone::Center => {
                let i = center_index.unwrap_or(self.center.len()).min(self.center.len());
                self.center.insert(i, a);
                if self.center.len() > Self::CENTER_CAPACITY {
                    let last = self.center.len() - 1;
                    let overflow = self.center.remove(if i == last { last - 1 } else { last });
                    self.hidden.push(overflow);
                }
            }
            Zone::Tray => self.hidden.push(a),
        }
    }

    /// Every action exactly once; unknown duplicates dropped, missing ones go to the tray.
    pub fn normalized(&self) -> CardLayout {
        let mut seen = Vec::new();
        let mut claim = |a: CardAction| {
            if seen.contains(&a) {
                false
            } else {
                seen.push(a);
                true
            }
        };
        let corners = self.corners.map(|c| c.filter(|&a| claim(a)));
        let mut center: Vec<CardAction> = self.center.iter().copied().filter(|&a| claim(a)).collect();
        let mut hidden: Vec<CardAction> = self.hidden.iter().copied().filter(|&a| claim(a)).collect();
        if center.len() > Self::CENTER_CAPACITY {
            hidden.extend(center.drain(Self::CENTER_CAPACITY..));
        }
        hidden.extend(CardAction::ALL.into_iter().filter(|&a| claim(a)));
        CardLayout { corners, center, hidden }
    }
}

#[cfg(test)]
mod tests {
    use super::CardAction::*;
    use super::*;

    #[test]
    fn dropping_on_an_occupied_corner_swaps_with_the_origin() {
        let mut l = CardLayout::default();
        l.place(Pin, Zone::Corner(Corner::TopLeft), None);
        assert_eq!(l.corners, [Some(Pin), Some(Dismiss), Some(Edit), Some(QuickLook)]);
        assert_eq!(l.center, vec![Copy, Save, Delete]);

        l.place(Pin, Zone::Corner(Corner::BottomRight), None);
        assert_eq!(l.corners, [Some(QuickLook), Some(Dismiss), Some(Edit), Some(Pin)]);
    }

    #[test]
    fn center_keeps_order_and_capacity() {
        let mut l = CardLayout::default();
        l.place(CopyText, Zone::Center, Some(0));
        assert_eq!(l.center, vec![CopyText, Copy, Save]);
        assert_eq!(l.hidden, vec![SaveAs, Compress, Pin]);

        l.place(Save, Zone::Center, Some(0));
        assert_eq!(l.center, vec![Save, CopyText, Copy]);

        l.place(Copy, Zone::Tray, None);
        assert_eq!(l.center, vec![Save, CopyText]);
        assert_eq!(l.hidden.last(), Some(&Copy));
    }

    #[test]
    fn normalized_repairs_duplicates_and_missing_actions() {
        let broken = CardLayout { corners: [Some(Copy), Some(Copy), None, None], center: vec![Save, Save, Pin, Edit, Delete], hidden: vec![] };
        let n = broken.normalized();
        assert_eq!(n.corners, [Some(Copy), None, None, None]);
        assert_eq!(n.center, vec![Save, Pin, Edit]);
        assert_eq!(n.hidden, vec![Delete, SaveAs, Compress, QuickLook, CopyText, Dismiss]);
    }
}
