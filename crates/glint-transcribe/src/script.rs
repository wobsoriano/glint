use crate::transcript::normalize_word;

/// How far past the current position a spoken word may land, so a skipped or misheard word
/// doesn't stall the prompter but a common word can't yank it far ahead.
const LOOKAHEAD: usize = 8;

struct ScriptWord {
    normalized: String,
    display_index: usize,
}

/// Follows a teleprompter script from recognized speech. Progress only moves forward.
pub struct ScriptTracker {
    words: Vec<ScriptWord>,
    display_len: usize,
    /// Number of `words` read so far.
    position: usize,
    previous_spoken: String,
}

impl ScriptTracker {
    pub fn new(script: &str) -> Self {
        let display: Vec<&str> = script.split_whitespace().collect();
        let words = display
            .iter()
            .enumerate()
            .filter_map(|(display_index, word)| {
                let normalized = normalize_word(word);
                (!normalized.is_empty()).then_some(ScriptWord { normalized, display_index })
            })
            .collect();
        Self { words, display_len: display.len(), position: 0, previous_spoken: String::new() }
    }

    /// Number of whitespace-separated words in the script, the range of `current_index`.
    pub fn display_len(&self) -> usize {
        self.display_len
    }

    /// Index (into the script's whitespace-separated words) of the last word read, or `None`
    /// before the first match.
    pub fn current_index(&self) -> Option<usize> {
        self.position.checked_sub(1).map(|i| self.words[i].display_index)
    }

    /// Feeds newly recognized words in spoken order and returns the updated index.
    pub fn feed<S: AsRef<str>>(&mut self, spoken: &[S]) -> Option<usize> {
        for word in spoken {
            let word = normalize_word(word.as_ref());
            if word.is_empty() {
                continue;
            }
            let joined = format!("{}{}", self.previous_spoken, word);
            let window = self.position..(self.position + LOOKAHEAD).min(self.words.len());
            let hit = window
                .clone()
                .find(|&i| matches(&self.words[i].normalized, &word))
                .or_else(|| window.clone().find(|&i| self.words[i].normalized == joined));
            if let Some(i) = hit {
                self.position = i + 1;
            }
            self.previous_spoken = word;
        }
        self.current_index()
    }
}

/// Exact match, or a close spelling for words long enough that a near miss is still telling
/// ("colour" for "color", "recognise" for "recognize").
fn matches(script: &str, spoken: &str) -> bool {
    if script == spoken {
        return true;
    }
    let longest = script.chars().count().max(spoken.chars().count());
    longest >= 5 && edit_distance(script, spoken) * 4 <= longest
}

fn edit_distance(a: &str, b: &str) -> usize {
    let b: Vec<char> = b.chars().collect();
    let mut row: Vec<usize> = (0..=b.len()).collect();
    for (i, ca) in a.chars().enumerate() {
        let mut diagonal = row[0];
        row[0] = i + 1;
        for (j, &cb) in b.iter().enumerate() {
            let above = row[j + 1];
            row[j + 1] = (diagonal + usize::from(ca != cb)).min(above + 1).min(row[j] + 1);
            diagonal = above;
        }
    }
    row[b.len()]
}

#[cfg(test)]
mod tests {
    use super::*;

    const SCRIPT: &str = "Hello and welcome! Today, we're recording a short demo of the new editor.";

    #[test]
    fn follows_speech_ignoring_case_and_punctuation() {
        let mut tracker = ScriptTracker::new(SCRIPT);
        assert_eq!(tracker.current_index(), None);
        assert_eq!(tracker.feed(&["hello", "AND", "Welcome"]), Some(2));
        assert_eq!(tracker.feed(&["today", "were", "recording"]), Some(5));
    }

    #[test]
    fn skips_misheard_and_missing_words() {
        let mut tracker = ScriptTracker::new(SCRIPT);
        assert_eq!(tracker.feed(&["hello", "and", "welcome", "toady", "we're", "recorded", "short"]), Some(7));
        assert_eq!(tracker.feed(&["demo", "of", "the", "new", "editor"]), Some(12));
    }

    #[test]
    fn never_moves_backward_and_ignores_far_jumps() {
        let mut tracker = ScriptTracker::new(SCRIPT);
        tracker.feed(&["hello", "and", "welcome", "today"]);
        assert_eq!(tracker.feed(&["hello", "and"]), Some(3));
        assert_eq!(tracker.feed(&["editor"]), Some(3));
    }

    #[test]
    fn rejoins_words_the_recognizer_split() {
        let mut tracker = ScriptTracker::new("Open the teleprompter now");
        assert_eq!(tracker.feed(&["open", "the", "tele", "prompter"]), Some(2));
    }

    #[test]
    fn fuzzy_matching_is_limited_to_long_words() {
        assert!(matches("recognize", "recognise"));
        assert!(!matches("cat", "cut"));
        assert_eq!(edit_distance("kitten", "sitting"), 3);
    }
}
