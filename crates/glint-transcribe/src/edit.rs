use std::ops::Range;

use crate::transcript::{TimeRange, Word};

const MAX_WORD_PADDING_MS: u64 = 120;

/// Source-time ranges that remove the selected words, sorted and merged. Each cut extends into
/// the surrounding pauses by up to half the gap, never into a neighboring word.
pub fn word_cut_ranges(words: &[Word], selections: &[Range<usize>], source_duration_ms: u64) -> Vec<TimeRange> {
    let cuts = selections
        .iter()
        .filter(|r| r.start < r.end && r.end <= words.len())
        .filter_map(|r| {
            let first = &words[r.start];
            let last = &words[r.end - 1];
            let prev_end = if r.start > 0 { words[r.start - 1].end_ms } else { 0 };
            let next_start = words.get(r.end).map_or(source_duration_ms, |w| w.start_ms);
            let pad_before = MAX_WORD_PADDING_MS.min(first.start_ms.saturating_sub(prev_end) / 2);
            let pad_after = MAX_WORD_PADDING_MS.min(next_start.saturating_sub(last.end_ms) / 2);
            let range = TimeRange {
                start_ms: first.start_ms.saturating_sub(pad_before),
                end_ms: (last.end_ms + pad_after).min(source_duration_ms),
            };
            (range.end_ms > range.start_ms).then_some(range)
        })
        .collect();
    merge(cuts)
}

/// Turns silence spans into cuts that keep `keep_ms` of quiet on each side of the splice, so
/// trimmed narration still breathes. Spans too short to leave `min_cut_ms` are skipped.
pub fn silence_cut_ranges(spans: &[TimeRange], keep_ms: u64, min_cut_ms: u64) -> Vec<TimeRange> {
    let cuts = spans
        .iter()
        .filter_map(|s| {
            let cut = TimeRange { start_ms: s.start_ms + keep_ms, end_ms: s.end_ms.saturating_sub(keep_ms) };
            (cut.end_ms > cut.start_ms && cut.duration_ms() >= min_cut_ms).then_some(cut)
        })
        .collect();
    merge(cuts)
}

pub fn merge(mut ranges: Vec<TimeRange>) -> Vec<TimeRange> {
    ranges.sort_by_key(|r| r.start_ms);
    let mut merged: Vec<TimeRange> = Vec::with_capacity(ranges.len());
    for range in ranges {
        match merged.last_mut() {
            Some(last) if range.start_ms <= last.end_ms => last.end_ms = last.end_ms.max(range.end_ms),
            _ => merged.push(range),
        }
    }
    merged
}

#[cfg(test)]
#[allow(clippy::single_range_in_vec_init)]
mod tests {
    use super::*;

    fn w(start_ms: u64, end_ms: u64) -> Word {
        Word { text: "w".into(), start_ms, end_ms, confidence: 1.0 }
    }

    fn r(start_ms: u64, end_ms: u64) -> TimeRange {
        TimeRange { start_ms, end_ms }
    }

    #[test]
    fn cut_bites_into_pauses_but_not_neighbors() {
        let words = [w(0, 400), w(500, 700), w(1_500, 1_800), w(1_820, 2_000)];
        assert_eq!(word_cut_ranges(&words, &[1..2], 3_000), vec![r(450, 820)]);
        assert_eq!(word_cut_ranges(&words, &[2..3], 3_000), vec![r(1_380, 1_810)]);
    }

    #[test]
    fn edges_pad_toward_media_bounds() {
        let words = [w(100, 400), w(500, 900)];
        assert_eq!(word_cut_ranges(&words, &[0..1], 1_000), vec![r(50, 450)]);
        assert_eq!(word_cut_ranges(&words, &[1..2], 1_000), vec![r(450, 950)]);
    }

    #[test]
    fn adjacent_selections_merge_and_bad_ranges_are_ignored() {
        let words = [w(0, 400), w(400, 800), w(800, 1_200)];
        assert_eq!(word_cut_ranges(&words, &[1..2, 0..1, 2..9, 3..3], 2_000), vec![r(0, 800)]);
    }

    #[test]
    fn silence_cuts_keep_breathing_room() {
        let spans = [r(1_000, 3_000), r(5_000, 5_900)];
        assert_eq!(silence_cut_ranges(&spans, 350, 300), vec![r(1_350, 2_650)]);
    }
}
