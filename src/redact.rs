//! Finds sensitive information (emails, keys, card numbers...) in OCR output.

use crate::annotate::R;
use crate::ocr::Line;
use crate::settings::RedactRules;
use regex::Regex;
use std::sync::LazyLock;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Email,
    Phone,
    Card,
    Secret,
    Network,
    Id,
    Url,
}

impl Kind {
    pub fn label(self) -> &'static str {
        match self {
            Kind::Email => "email",
            Kind::Phone => "phone number",
            Kind::Card => "card number",
            Kind::Secret => "secret",
            Kind::Network => "IP/MAC address",
            Kind::Id => "ID number",
            Kind::Url => "URL with token",
        }
    }
    fn enabled(self, r: &RedactRules) -> bool {
        match self {
            Kind::Email => r.emails,
            Kind::Phone => r.phones,
            Kind::Card => r.credit_cards,
            Kind::Secret => r.secrets,
            Kind::Network => r.ip_addresses,
            Kind::Id => r.ids,
            Kind::Url => r.urls_with_tokens,
        }
    }
}

#[derive(Clone, Debug)]
pub struct Finding {
    pub kind: Kind,
    pub rect: R,
}

struct Rule {
    kind: Kind,
    re: Regex,
    /// Capture group to redact (0 = whole match).
    group: usize,
    validate: Option<fn(&str) -> bool>,
}

fn rule(kind: Kind, re: &str) -> Rule {
    Rule {
        kind,
        re: Regex::new(re).unwrap(),
        group: 0,
        validate: None,
    }
}

static RULES: LazyLock<Vec<Rule>> = LazyLock::new(|| {
    vec![
        rule(Kind::Email, r"[A-Za-z0-9._%+-]+ ?@ ?[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}"),
        // Cards before phones so digit groups are classified as cards first.
        Rule {
            validate: Some(luhn),
            ..rule(Kind::Card, r"\b(?:\d[ -]?){12,18}\d\b")
        },
        Rule {
            validate: Some(|s| {
                let d = s.chars().filter(|c| c.is_ascii_digit()).count();
                (9..=15).contains(&d)
            }),
            ..rule(
                Kind::Phone,
                r"\+\d{1,3}[\s.-]?(?:\(?\d{1,4}\)?[\s.-]?){2,4}\d{2,4}\b|\(?\b\d{3}\)?[\s.-]\d{3}[\s.-]\d{4}\b",
            )
        },
        rule(Kind::Id, r"\b\d{3}-\d{2}-\d{4}\b"),
        rule(Kind::Id, r"\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){3,7}(?: ?[A-Z0-9]{1,4})?\b"),
        rule(Kind::Network, r"\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b"),
        rule(Kind::Network, r"\b(?:[0-9a-fA-F]{1,4}:){4,7}[0-9a-fA-F]{1,4}\b"),
        rule(Kind::Network, r"\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b"),
        rule(
            Kind::Secret,
            r"\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{8,}|\bsk-[A-Za-z0-9_-]{16,}|\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_[A-Za-z0-9_]{20,}|\bxox[abprs]-[A-Za-z0-9-]{10,}|\bAKIA[0-9A-Z]{16}\b|\bAIza[0-9A-Za-z_-]{30,}|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{6,}|\bglpat-[A-Za-z0-9_-]{16,}|\bnpm_[A-Za-z0-9]{30,}|\bshpat_[a-fA-F0-9]{32}",
        ),
        Rule {
            group: 1,
            ..rule(
                Kind::Secret,
                r"(?i)\b(?:password|passwd|pwd|passcode|secret|token|api[_ -]?key|access[_ -]?key|client[_ -]?secret|private[_ -]?key|auth)\b\s*[:=]\s*(\S+(?:\s+[A-Za-z0-9_+/=-]{6,})*)",
            )
        },
        Rule {
            validate: Some(high_entropy),
            ..rule(Kind::Secret, r"[A-Za-z0-9+/_=-]{28,}")
        },
        rule(
            Kind::Url,
            r"(?i)https?://\S*[?&#](?:token|key|signature|sig|code|access_token|id_token|auth|apikey|api_key|secret|session)=\S+|https?://[^\s/:@]+:[^\s/@]+@\S+",
        ),
    ]
});

static DANGLING_LABEL: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?i)\b(?:password|passwd|pwd|passcode|secret|token|api[_ -]?key|access[_ -]?key|secret[_ -]?key|client[_ -]?secret|private[_ -]?key|key|auth)\s*[:=]\s*$").unwrap()
});

fn luhn(s: &str) -> bool {
    let digits: Vec<u32> = s.chars().filter_map(|c| c.to_digit(10)).collect();
    if !(13..=19).contains(&digits.len()) {
        return false;
    }
    let sum: u32 = digits
        .iter()
        .rev()
        .enumerate()
        .map(|(i, &d)| {
            if i % 2 == 1 {
                let x = d * 2;
                if x > 9 { x - 9 } else { x }
            } else {
                d
            }
        })
        .sum();
    sum % 10 == 0
}

/// Looks like a random token: mixed case and digits, many distinct characters.
fn high_entropy(s: &str) -> bool {
    let has_digit = s.chars().any(|c| c.is_ascii_digit());
    let has_lower = s.chars().any(|c| c.is_ascii_lowercase());
    let has_upper = s.chars().any(|c| c.is_ascii_uppercase());
    let mut seen = std::collections::HashSet::new();
    s.chars().for_each(|c| {
        seen.insert(c);
    });
    let hex = s.chars().all(|c| c.is_ascii_hexdigit()) && s.len() >= 32;
    hex || (has_digit && has_lower && has_upper && seen.len() >= 14)
}

pub fn find(lines: &[Line], rules: &RedactRules, pad: f32) -> Vec<Finding> {
    let mut out: Vec<Finding> = Vec::new();
    for line in lines {
        // Rebuild the line string and remember where each word lives in it.
        let mut text = String::new();
        let mut spans = Vec::new();
        for (i, w) in line.words.iter().enumerate() {
            if i > 0 {
                text.push(' ');
            }
            let start = text.len();
            text.push_str(&w.text);
            spans.push((start, text.len()));
        }
        // OCR often drops random-looking tokens entirely, leaving just "Key:". Hide the area
        // to the right of such a dangling label.
        if rules.secrets && DANGLING_LABEL.is_match(&text) {
            if let Some(last) = line.words.last() {
                let h = line.rect.h;
                out.push(Finding {
                    kind: Kind::Secret,
                    rect: R {
                        x: last.rect.right() + h * 0.2,
                        y: line.rect.y,
                        w: h * 24.0,
                        h,
                    }
                    .inflate(pad),
                });
            }
        }
        for r in RULES.iter().filter(|r| r.kind.enabled(rules)) {
            for caps in r.re.captures_iter(&text) {
                let Some(m) = caps.get(r.group) else { continue };
                if let Some(v) = r.validate {
                    if !v(m.as_str()) {
                        continue;
                    }
                }
                let mut rect: Option<R> = None;
                for (i, &(s, e)) in spans.iter().enumerate() {
                    if s < m.end() && e > m.start() {
                        let wr = line.words[i].rect;
                        rect = Some(rect.map_or(wr, |r| r.union(&wr)));
                    }
                }
                let Some(rect) = rect else { continue };
                let rect = rect.inflate(pad);
                let overlaps = out.iter_mut().find(|f| {
                    f.rect
                        .intersect(&rect)
                        .is_some_and(|i| i.area() > 0.6 * rect.area().min(f.rect.area()))
                });
                match overlaps {
                    Some(f) => f.rect = f.rect.union(&rect),
                    None => out.push(Finding { kind: r.kind, rect }),
                }
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ocr::Word;

    fn line(text: &str) -> Line {
        let words: Vec<Word> = text
            .split(' ')
            .enumerate()
            .map(|(i, t)| Word {
                text: t.to_string(),
                rect: R { x: i as f32 * 100.0, y: 0.0, w: 90.0, h: 20.0 },
            })
            .collect();
        Line { rect: R { x: 0.0, y: 0.0, w: 100.0 * words.len() as f32, h: 20.0 }, words }
    }

    fn kinds(text: &str) -> Vec<Kind> {
        find(&[line(text)], &RedactRules::default(), 0.0).iter().map(|f| f.kind).collect()
    }

    #[test]
    fn detects_common_secrets() {
        assert_eq!(kinds("contact me at jane.doe@example.com today"), vec![Kind::Email]);
        assert_eq!(kinds("card 4242 4242 4242 4242 exp"), vec![Kind::Card]);
        assert!(kinds("key sk_live_51HxYzAbCdEfGhIjKlMn here").contains(&Kind::Secret));
        assert!(kinds("token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789").contains(&Kind::Secret));
        assert_eq!(kinds("server 192.168.1.20 is up"), vec![Kind::Network]);
        assert_eq!(kinds("call +1 415-555-0132 now"), vec![Kind::Phone]);
        assert!(kinds("password: hunter2").contains(&Kind::Secret));
    }

    #[test]
    fn labeled_secret_covers_a_token_ocr_split_in_two() {
        let found = find(&[line("API key: sk_live_51 HxYzAbCdEfGhIjKlMnOpQr")], &RedactRules::default(), 0.0);
        assert_eq!(found.len(), 1);
        assert_eq!(found[0].kind, Kind::Secret);
        assert_eq!((found[0].rect.x, found[0].rect.right()), (200.0, 390.0));
    }

    #[test]
    fn ignores_plain_text() {
        assert!(kinds("Hello world, version 1.2.3 released on 2026-10-14").is_empty());
        assert!(kinds("card 4242 4242 4242 4241").is_empty());
    }
}
