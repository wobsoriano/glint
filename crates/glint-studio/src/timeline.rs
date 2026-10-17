use crate::project::{Clip, Project, Zoom, ZoomFocus};
use std::ops::Range;

pub const MIN_CLIP_MS: f64 = 120.0;
pub const MIN_ZOOM_MS: f64 = 500.0;
pub const MIN_SPEED: f64 = 0.25;
pub const MAX_SPEED: f64 = 8.0;

impl Clip {
    pub fn source_len(&self) -> f64 {
        (self.end_ms - self.start_ms).max(0.0)
    }

    pub fn output_len(&self) -> f64 {
        self.source_len() / self.speed
    }
}

impl Project {
    pub fn duration_ms(&self) -> f64 {
        self.clips.iter().map(Clip::output_len).sum()
    }

    /// The clip playing at `output_ms` and the output time it starts at. A time on a boundary
    /// belongs to the later clip; the end of the timeline belongs to the last clip.
    pub fn clip_at(&self, output_ms: f64) -> Option<(usize, f64)> {
        let mut start = 0.0;
        for (i, c) in self.clips.iter().enumerate() {
            let end = start + c.output_len();
            if output_ms < end || i + 1 == self.clips.len() {
                return Some((i, start));
            }
            start = end;
        }
        None
    }

    pub fn clip_output_start(&self, index: usize) -> f64 {
        self.clips[..index].iter().map(Clip::output_len).sum()
    }

    pub fn source_time(&self, output_ms: f64) -> f64 {
        let t = output_ms.clamp(0.0, self.duration_ms());
        match self.clip_at(t) {
            Some((i, start)) => {
                let c = &self.clips[i];
                (c.start_ms + (t - start) * c.speed).min(c.end_ms)
            }
            None => 0.0,
        }
    }

    /// Output time of a source instant, or `None` when that footage was removed.
    /// Clip boundaries are inclusive, which suits the playhead.
    pub fn output_time(&self, source_ms: f64) -> Option<f64> {
        self.map_source(source_ms, |s, c| s >= c.start_ms && s <= c.end_ms)
    }

    /// Like `output_time`, but a clip owns `[start, end)`, so an event at the exact end of an
    /// outgoing clip never shows up on the first frame of the next one.
    pub fn event_output_time(&self, source_ms: f64) -> Option<f64> {
        self.map_source(source_ms, |s, c| s >= c.start_ms && s < c.end_ms)
    }

    fn map_source(&self, source_ms: f64, inside: impl Fn(f64, &Clip) -> bool) -> Option<f64> {
        let mut start = 0.0;
        for c in &self.clips {
            if inside(source_ms, c) {
                return Some(start + (source_ms - c.start_ms) / c.speed);
            }
            start += c.output_len();
        }
        None
    }

    /// Output ranges where the source range `[start, end)` is visible, for drawing zooms and
    /// transcript spans on the output timeline.
    pub fn output_ranges(&self, source: Range<f64>) -> Vec<Range<f64>> {
        let mut out = Vec::new();
        let mut start = 0.0;
        for c in &self.clips {
            let a = source.start.max(c.start_ms);
            let b = source.end.min(c.end_ms);
            if b > a {
                out.push(start + (a - c.start_ms) / c.speed..start + (b - c.start_ms) / c.speed);
            }
            start += c.output_len();
        }
        out
    }

    /// Splits the clip under the playhead. Returns the index of the new trailing clip, or
    /// `None` when either half would be shorter than `MIN_CLIP_MS`.
    pub fn split_at(&mut self, output_ms: f64) -> Option<usize> {
        let (i, _) = self.clip_at(output_ms)?;
        let at = self.source_time(output_ms);
        let c = self.clips[i];
        if at - c.start_ms < MIN_CLIP_MS || c.end_ms - at < MIN_CLIP_MS {
            return None;
        }
        self.clips[i].end_ms = at;
        self.clips.insert(i + 1, Clip { start_ms: at, ..c });
        Some(i + 1)
    }

    /// Moves a clip's source bounds, clamped so it never overlaps its neighbours or shrinks
    /// below `MIN_CLIP_MS`. Returns false when nothing changed.
    pub fn trim_clip(&mut self, index: usize, start_ms: f64, end_ms: f64) -> bool {
        let Some(&c) = self.clips.get(index) else { return false };
        let lo = if index > 0 { self.clips[index - 1].end_ms } else { 0.0 };
        let hi = self.clips.get(index + 1).map_or(f64::INFINITY, |n| n.start_ms);
        let start = start_ms.clamp(lo, c.end_ms - MIN_CLIP_MS);
        let end = end_ms.clamp(start + MIN_CLIP_MS, hi.max(start + MIN_CLIP_MS));
        if start == c.start_ms && end == c.end_ms {
            return false;
        }
        self.clips[index].start_ms = start;
        self.clips[index].end_ms = end;
        true
    }

    /// Removes a clip. The last remaining clip cannot be deleted.
    pub fn delete_clip(&mut self, index: usize) -> bool {
        if self.clips.len() < 2 || index >= self.clips.len() {
            return false;
        }
        self.clips.remove(index);
        true
    }

    pub fn set_speed(&mut self, index: usize, speed: f64) {
        if let Some(c) = self.clips.get_mut(index) {
            c.speed = speed.clamp(MIN_SPEED, MAX_SPEED);
        }
    }

    /// Removes source ranges (transcript cuts, filler words, silences) from every clip they
    /// touch, splitting clips where a range falls inside one. Pieces shorter than
    /// `MIN_CLIP_MS` are dropped. Returns false, leaving the project unchanged, when nothing
    /// playable would survive.
    pub fn cut_source_ranges(&mut self, ranges: &[Range<f64>]) -> bool {
        let mut cuts: Vec<Range<f64>> =
            ranges.iter().filter(|r| r.end > r.start).cloned().collect();
        cuts.sort_by(|a, b| a.start.total_cmp(&b.start));
        let mut next = Vec::new();
        for c in &self.clips {
            let mut cursor = c.start_ms;
            let mut push = |a: f64, b: f64| {
                if b - a >= MIN_CLIP_MS {
                    next.push(Clip { start_ms: a, end_ms: b, speed: c.speed });
                }
            };
            for cut in cuts.iter().filter(|r| r.end > c.start_ms && r.start < c.end_ms) {
                if cut.start > cursor {
                    push(cursor, cut.start);
                }
                cursor = cursor.max(cut.end);
            }
            if cursor < c.end_ms {
                push(cursor, c.end_ms);
            }
        }
        if next.is_empty() {
            return false;
        }
        self.clips = next;
        true
    }

    /// Adds a manual zoom over an output range and returns its index.
    pub fn add_zoom(&mut self, output: Range<f64>) -> usize {
        let (start, end) = self.source_span(output);
        self.insert_zoom(Zoom {
            start_ms: start,
            end_ms: end,
            scale: 1.5,
            focus: ZoomFocus::Pointer,
            enabled: true,
            auto: false,
        })
    }

    pub fn remove_zoom(&mut self, index: usize) {
        if index < self.zooms.len() {
            self.zooms.remove(index);
        }
    }

    /// Moves or resizes a zoom to an output range. An edited auto zoom becomes manual so
    /// regenerating auto zoom keeps the edit. Returns the zoom's new index.
    pub fn retime_zoom(&mut self, index: usize, output: Range<f64>) -> usize {
        let (start, end) = self.source_span(output);
        let mut z = self.zooms.remove(index);
        z.start_ms = start;
        z.end_ms = end;
        z.auto = false;
        self.insert_zoom(z)
    }

    fn source_span(&self, output: Range<f64>) -> (f64, f64) {
        let out_end = output.start + (output.end - output.start).max(MIN_ZOOM_MS);
        let start = self.source_time(output.start);
        let end = match self.clip_at(output.start) {
            Some((i, clip_start)) if out_end <= clip_start + self.clips[i].output_len() => {
                self.clips[i].start_ms + (out_end - clip_start) * self.clips[i].speed
            }
            _ => self.source_time(out_end),
        };
        (start, end.max(start + MIN_ZOOM_MS))
    }

    fn insert_zoom(&mut self, z: Zoom) -> usize {
        let i = self.zooms.partition_point(|o| o.start_ms <= z.start_ms);
        self.zooms.insert(i, z);
        i
    }
}

/// Undo and redo of whole-project snapshots. Record the project before each edit.
#[derive(Debug, Default)]
pub struct History {
    undo: Vec<Project>,
    redo: Vec<Project>,
}

impl History {
    const LIMIT: usize = 200;

    pub fn record(&mut self, before: &Project) {
        if self.undo.last() == Some(before) {
            return;
        }
        if self.undo.len() == Self::LIMIT {
            self.undo.remove(0);
        }
        self.undo.push(before.clone());
        self.redo.clear();
    }

    pub fn undo(&mut self, project: &mut Project) -> bool {
        let Some(prev) = self.undo.pop() else { return false };
        self.redo.push(std::mem::replace(project, prev));
        true
    }

    pub fn redo(&mut self, project: &mut Project) -> bool {
        let Some(next) = self.redo.pop() else { return false };
        self.undo.push(std::mem::replace(project, next));
        true
    }

    pub fn can_undo(&self) -> bool {
        !self.undo.is_empty()
    }

    pub fn can_redo(&self) -> bool {
        !self.redo.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn clips(p: &Project) -> Vec<(f64, f64, f64)> {
        p.clips.iter().map(|c| (c.start_ms, c.end_ms, c.speed)).collect()
    }

    #[test]
    fn split_then_speed_maps_both_directions() {
        let mut p = Project::new(10_000.0);
        assert_eq!(p.split_at(4_000.0), Some(1));
        p.set_speed(1, 2.0);
        assert_eq!(clips(&p), vec![(0.0, 4_000.0, 1.0), (4_000.0, 10_000.0, 2.0)]);
        assert_eq!(p.duration_ms(), 7_000.0);
        assert_eq!(p.source_time(3_000.0), 3_000.0);
        assert_eq!(p.source_time(5_000.0), 6_000.0);
        assert_eq!(p.source_time(7_000.0), 10_000.0);
        assert_eq!(p.output_time(8_000.0), Some(6_000.0));
        assert_eq!(p.output_time(4_000.0), Some(4_000.0));
    }

    #[test]
    fn delete_removes_footage_from_output() {
        let mut p = Project::new(9_000.0);
        p.split_at(3_000.0);
        p.split_at(6_000.0);
        assert!(p.delete_clip(1));
        assert_eq!(clips(&p), vec![(0.0, 3_000.0, 1.0), (6_000.0, 9_000.0, 1.0)]);
        assert_eq!(p.duration_ms(), 6_000.0);
        assert_eq!(p.source_time(3_500.0), 6_500.0);
        assert_eq!(p.output_time(4_000.0), None);
        assert_eq!(p.output_time(7_000.0), Some(4_000.0));
        assert!(p.delete_clip(0));
        assert!(!p.delete_clip(0), "the last clip stays");
    }

    #[test]
    fn split_refuses_slivers() {
        let mut p = Project::new(10_000.0);
        assert_eq!(p.split_at(50.0), None);
        assert_eq!(p.split_at(9_950.0), None);
        assert_eq!(p.clips.len(), 1);
    }

    #[test]
    fn trim_clamps_to_neighbours() {
        let mut p = Project::new(10_000.0);
        p.split_at(5_000.0);
        assert!(p.trim_clip(1, 4_000.0, 9_000.0));
        assert_eq!(clips(&p), vec![(0.0, 5_000.0, 1.0), (5_000.0, 9_000.0, 1.0)]);
        assert!(p.trim_clip(0, 1_000.0, 4_000.0));
        assert_eq!(p.clips[0].start_ms, 1_000.0);
        assert_eq!(p.duration_ms(), 7_000.0);
    }

    #[test]
    fn transcript_cuts_split_and_drop_clips() {
        let mut p = Project::new(10_000.0);
        p.split_at(5_000.0);
        p.set_speed(1, 2.0);
        assert!(p.cut_source_ranges(&[1_000.0..2_000.0, 4_500.0..6_000.0, 9_950.0..10_000.0]));
        assert_eq!(
            clips(&p),
            vec![
                (0.0, 1_000.0, 1.0),
                (2_000.0, 4_500.0, 1.0),
                (6_000.0, 9_950.0, 2.0),
            ]
        );
        assert_eq!(p.duration_ms(), 1_000.0 + 2_500.0 + 1_975.0);
        assert_eq!(p.source_time(3_500.0), 6_000.0);
        assert_eq!(p.output_time(7_000.0), Some(4_000.0));
        assert_eq!(p.output_time(1_500.0), None);
        assert!(!p.cut_source_ranges(&[0.0..10_000.0]));
        assert_eq!(p.clips.len(), 3, "a cut removing everything is refused");
    }

    #[test]
    fn event_mapping_is_half_open() {
        let mut p = Project::new(4_000.0);
        p.split_at(2_000.0);
        p.delete_clip(1);
        p.clips.push(Clip { start_ms: 3_000.0, end_ms: 4_000.0, speed: 1.0 });
        assert_eq!(p.output_time(2_000.0), Some(2_000.0));
        assert_eq!(p.event_output_time(2_000.0), None);
        assert_eq!(p.event_output_time(3_000.0), Some(2_000.0));
    }

    #[test]
    fn zooms_follow_their_footage_through_edits() {
        let mut p = Project::new(10_000.0);
        let i = p.add_zoom(6_000.0..8_000.0);
        assert_eq!((p.zooms[i].start_ms, p.zooms[i].end_ms), (6_000.0, 8_000.0));
        p.split_at(2_000.0);
        p.split_at(4_000.0);
        p.delete_clip(1);
        assert_eq!(p.output_ranges(6_000.0..8_000.0), vec![4_000.0..6_000.0]);
        let j = p.retime_zoom(0, 1_000.0..2_500.0);
        assert_eq!((p.zooms[j].start_ms, p.zooms[j].end_ms), (1_000.0, 4_500.0));
        assert_eq!(p.output_ranges(1_000.0..4_500.0), vec![1_000.0..2_000.0, 2_000.0..2_500.0]);
    }

    #[test]
    fn history_round_trips_edits() {
        let mut p = Project::new(10_000.0);
        let mut h = History::default();
        h.record(&p);
        p.split_at(5_000.0);
        h.record(&p);
        p.set_speed(0, 4.0);
        assert_eq!(p.duration_ms(), 6_250.0);
        assert!(h.undo(&mut p));
        assert_eq!(p.duration_ms(), 10_000.0);
        assert_eq!(p.clips.len(), 2);
        assert!(h.undo(&mut p));
        assert_eq!(p.clips.len(), 1);
        assert!(!h.undo(&mut p));
        assert!(h.redo(&mut p));
        assert!(h.redo(&mut p));
        assert_eq!(p.duration_ms(), 6_250.0);
        assert!(!h.can_redo());
    }
}
