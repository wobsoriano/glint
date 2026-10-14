use std::ops::Range;

use crate::transcript::{normalize_word, Word};

/// Hesitation sounds that never carry meaning.
const HESITATIONS: &[&str] = &[
    "um", "umm", "ummm", "uh", "uhh", "uhhh", "uhm", "er", "erm", "ah", "ahh", "hmm", "hm", "mm", "mmm", "mhm",
];

/// Discourse fillers that are also ordinary words ("I like it", "do you know her"), so they only
/// count when set apart by punctuation or a pause.
const DISCOURSE: &[&[&str]] = &[&["you", "know"], &["i", "mean"], &["like"]];

/// A pause at least this long on both sides sets a word apart even without punctuation.
const STANDALONE_PAUSE_MS: u64 = 250;

/// Word index ranges that are filler, in order and non-overlapping.
pub fn filler_ranges(words: &[Word]) -> Vec<Range<usize>> {
    let normalized: Vec<String> = words.iter().map(|w| normalize_word(&w.text)).collect();
    let mut ranges = Vec::new();
    let mut i = 0;
    while i < words.len() {
        if HESITATIONS.contains(&normalized[i].as_str()) {
            ranges.push(i..i + 1);
            i += 1;
            continue;
        }
        let phrase = DISCOURSE.iter().find(|phrase| {
            let end = i + phrase.len();
            end <= words.len()
                && phrase.iter().zip(&normalized[i..end]).all(|(p, n)| p == n)
                && is_standalone(words, i..end)
        });
        match phrase {
            Some(phrase) => {
                ranges.push(i..i + phrase.len());
                i += phrase.len();
            }
            None => i += 1,
        }
    }
    ranges
}

fn is_standalone(words: &[Word], range: Range<usize>) -> bool {
    let first = &words[range.start];
    let last = &words[range.end - 1];
    let comma_before = range.start == 0 || ends_with_pause_mark(&words[range.start - 1].text);
    let comma_after = ends_with_pause_mark(&last.text);
    let pause_before = range.start == 0
        || first.start_ms.saturating_sub(words[range.start - 1].end_ms) >= STANDALONE_PAUSE_MS;
    let pause_after = range.end == words.len()
        || words[range.end].start_ms.saturating_sub(last.end_ms) >= STANDALONE_PAUSE_MS;
    (comma_before || pause_before) && (comma_after || pause_after)
}

fn ends_with_pause_mark(text: &str) -> bool {
    text.ends_with([',', '.', '?', '!', ';', ':', '-', '\u{2026}'])
}

#[cfg(test)]
mod tests {
    use super::*;

    fn words(text: &str) -> Vec<Word> {
        text.split_whitespace()
            .enumerate()
            .map(|(i, t)| Word { text: t.into(), start_ms: i as u64 * 300, end_ms: i as u64 * 300 + 250, confidence: 1.0 })
            .collect()
    }

    #[test]
    fn hesitations_always_count() {
        assert_eq!(filler_ranges(&words("So um this is uh, a Test")), vec![1..2, 4..5]);
        assert_eq!(filler_ranges(&words("Umm. Hmm?")), vec![0..1, 1..2]);
    }

    #[test]
    fn like_counts_only_when_set_apart() {
        assert_eq!(filler_ranges(&words("I like this tool")), vec![]);
        assert_eq!(filler_ranges(&words("it was, like, really fast")), vec![2..3]);
    }

    #[test]
    fn you_know_counts_only_when_set_apart() {
        assert_eq!(filler_ranges(&words("do you know her")), vec![]);
        assert_eq!(filler_ranges(&words("it is, you know, fine")), vec![2..4]);
    }

    #[test]
    fn a_pause_sets_like_apart() {
        let mut ws = words("it was like really fast");
        for w in &mut ws[2..] {
            w.start_ms += 400;
            w.end_ms += 400;
        }
        for w in &mut ws[3..] {
            w.start_ms += 400;
            w.end_ms += 400;
        }
        assert_eq!(filler_ranges(&ws), vec![2..3]);
    }

    #[test]
    fn the_words_um_and_like_inside_other_words_do_not_count() {
        assert_eq!(filler_ranges(&words("umbrella likely")), vec![]);
    }
}
