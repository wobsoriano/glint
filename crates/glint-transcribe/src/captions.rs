use std::ops::Range;

use crate::transcript::Word;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct CaptionOptions {
    pub max_chars: usize,
    /// A pause longer than this starts a new line.
    pub max_gap_ms: u64,
    pub max_duration_ms: u64,
    /// Short lines stay on screen at least this long.
    pub min_duration_ms: u64,
}

impl Default for CaptionOptions {
    fn default() -> Self {
        Self { max_chars: 42, max_gap_ms: 900, max_duration_ms: 4_000, min_duration_ms: 800 }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CaptionLine {
    pub start_ms: u64,
    pub end_ms: u64,
    pub text: String,
    /// Indices into the transcript's words, for karaoke highlighting and seeking.
    pub words: Range<usize>,
}

/// Groups words into subtitle lines, breaking on length, natural pauses and duration.
pub fn caption_lines(words: &[Word], options: &CaptionOptions) -> Vec<CaptionLine> {
    let mut lines = Vec::new();
    let mut start = 0;
    for i in 1..=words.len() {
        let breaks = i == words.len() || {
            let line = &words[start..i];
            let next = &words[i];
            let chars: usize = line.iter().map(|w| w.text.chars().count() + 1).sum::<usize>() - 1;
            let gap = next.start_ms.saturating_sub(line[line.len() - 1].end_ms);
            chars + 1 + next.text.chars().count() > options.max_chars
                || gap > options.max_gap_ms
                || next.end_ms.saturating_sub(line[0].start_ms) > options.max_duration_ms
        };
        if breaks && start < i {
            let line = &words[start..i];
            let start_ms = line[0].start_ms;
            let end_ms = line.iter().map(|w| w.end_ms).max().unwrap_or(start_ms);
            lines.push(CaptionLine {
                start_ms,
                end_ms: end_ms.max(start_ms + options.min_duration_ms),
                text: line.iter().map(|w| w.text.as_str()).collect::<Vec<_>>().join(" "),
                words: start..i,
            });
            start = i;
        }
    }
    lines
}

#[cfg(test)]
mod tests {
    use super::*;

    fn w(text: &str, start_ms: u64, end_ms: u64) -> Word {
        Word { text: text.into(), start_ms, end_ms, confidence: 1.0 }
    }

    #[test]
    fn breaks_on_pause_and_length() {
        let words = [
            w("Welcome", 0, 400),
            w("to", 450, 550),
            w("Glint.", 600, 1000),
            w("Today", 2500, 2800),
            w("we", 2850, 2950),
            w("record", 3000, 3300),
            w("something", 3350, 3700),
            w("wonderful", 3750, 4100),
        ];
        let options = CaptionOptions { max_chars: 20, ..CaptionOptions::default() };
        let lines = caption_lines(&words, &options);
        let texts: Vec<_> = lines.iter().map(|l| (l.text.as_str(), l.start_ms, l.end_ms, l.words.clone())).collect();
        assert_eq!(
            texts,
            vec![
                ("Welcome to Glint.", 0, 1000, 0..3),
                ("Today we record", 2500, 3300, 3..6),
                ("something wonderful", 3350, 4150, 6..8),
            ]
        );
    }

    #[test]
    fn short_line_gets_minimum_duration() {
        let lines = caption_lines(&[w("Hi.", 100, 300)], &CaptionOptions::default());
        assert_eq!(lines[0].end_ms, 900);
    }

    #[test]
    fn breaks_on_duration() {
        let words: Vec<_> = (0..6).map(|i| w("a", i * 1000, i * 1000 + 900)).collect();
        let lines = caption_lines(&words, &CaptionOptions::default());
        assert_eq!(lines.iter().map(|l| l.words.clone()).collect::<Vec<_>>(), vec![0..4, 4..6]);
    }

    #[test]
    fn empty_input_gives_no_lines() {
        assert!(caption_lines(&[], &CaptionOptions::default()).is_empty());
    }
}
