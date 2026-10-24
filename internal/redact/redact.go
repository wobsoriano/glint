// Package redact finds sensitive text in OCR output (emails, phone and card numbers, keys and
// tokens, network addresses, ID numbers, URLs carrying credentials) and returns the image
// rectangles to cover.
package redact

import (
	"regexp"
	"strings"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

type Kind int

const (
	Email Kind = iota
	Phone
	Card
	Secret
	Network
	ID
	URL
)

var kindInfo = [...]struct {
	label   string
	enabled func(settings.RedactRules) bool
}{
	Email:   {"email", func(r settings.RedactRules) bool { return r.Emails }},
	Phone:   {"phone number", func(r settings.RedactRules) bool { return r.Phones }},
	Card:    {"card number", func(r settings.RedactRules) bool { return r.CreditCards }},
	Secret:  {"secret", func(r settings.RedactRules) bool { return r.Secrets }},
	Network: {"IP/MAC address", func(r settings.RedactRules) bool { return r.IPAddresses }},
	ID:      {"ID number", func(r settings.RedactRules) bool { return r.IDs }},
	URL:     {"URL with token", func(r settings.RedactRules) bool { return r.URLsWithTokens }},
}

func (k Kind) Label() string { return kindInfo[k].label }

// Finding is an area to cover, in pixels of the recognized image.
type Finding struct {
	Kind Kind
	Rect render.R
}

// rule matches one kind. group picks the part of the match to cover, and validate, when set,
// rejects matches the pattern alone cannot rule out.
type rule struct {
	kind     Kind
	re       *regexp.Regexp
	group    int
	validate func(string) bool
}

// The patterns are the Rust build's, character for character. Go's \d, \s and \b are ASCII
// where Rust's are Unicode. Text next to a non-ASCII letter can match here and not in Rust
// ("é4242 4242 4242 4242" is a card here), and an ID or IPv4 address written in non-ASCII
// digits matches only in Rust. Card and phone numbers are unaffected: their validators
// count ASCII digits in both.
var rules = []rule{
	{kind: Email, re: regexp.MustCompile(`[A-Za-z0-9._%+-]+ ?@ ?[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)},
	{kind: Card, re: regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`), validate: luhn},
	{
		kind:     Phone,
		re:       regexp.MustCompile(`\+\d{1,3}[\s.-]?(?:\(?\d{1,4}\)?[\s.-]?){2,4}\d{2,4}\b|\(?\b\d{3}\)?[\s.-]\d{3}[\s.-]\d{4}\b`),
		validate: func(s string) bool { d := countDigits(s); return d >= 9 && d <= 15 },
	},
	{kind: ID, re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{kind: ID, re: regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){3,7}(?: ?[A-Z0-9]{1,4})?\b`)},
	{kind: Network, re: regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|1?\d?\d)\b`)},
	{kind: Network, re: regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){4,7}[0-9a-fA-F]{1,4}\b`)},
	{kind: Network, re: regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}\b`)},
	{kind: Secret, re: regexp.MustCompile(`\b(?:sk|pk|rk)_(?:live|test)_[A-Za-z0-9]{8,}|\bsk-[A-Za-z0-9_-]{16,}|\bgh[pousr]_[A-Za-z0-9]{20,}|\bgithub_pat_[A-Za-z0-9_]{20,}|\bxox[abprs]-[A-Za-z0-9-]{10,}|\bAKIA[0-9A-Z]{16}\b|\bAIza[0-9A-Za-z_-]{30,}|\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{6,}|\bglpat-[A-Za-z0-9_-]{16,}|\bnpm_[A-Za-z0-9]{30,}|\bshpat_[a-fA-F0-9]{32}`)},
	{kind: Secret, group: 1, re: regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|passcode|secret|token|api[_ -]?key|access[_ -]?key|client[_ -]?secret|private[_ -]?key|auth)\b\s*[:=]\s*(\S+(?:\s+[A-Za-z0-9_+/=-]{6,})*)`)},
	{kind: Secret, re: regexp.MustCompile(`[A-Za-z0-9+/_=-]{28,}`), validate: highEntropy},
	{kind: URL, re: regexp.MustCompile(`(?i)https?://\S*[?&#](?:token|key|signature|sig|code|access_token|id_token|auth|apikey|api_key|secret|session)=\S+|https?://[^\s/:@]+:[^\s/@]+@\S+`)},
}

// danglingLabel is a line that ends in "password:" or similar: the value is usually the next
// thing on screen, out of the line's reach or masked, so the space after the label is covered.
var danglingLabel = regexp.MustCompile(`(?i)\b(?:password|passwd|pwd|passcode|secret|token|api[_ -]?key|access[_ -]?key|secret[_ -]?key|client[_ -]?secret|private[_ -]?key|key|auth)\s*[:=]\s*$`)

func countDigits(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n++
		}
	}
	return n
}

func luhn(s string) bool {
	var digits []int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i := range digits {
		d := digits[len(digits)-1-i]
		if i%2 == 1 {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

func highEntropy(s string) bool {
	var digit, lower, upper bool
	seen := map[rune]bool{}
	hex := true
	for _, c := range s {
		digit = digit || (c >= '0' && c <= '9')
		lower = lower || (c >= 'a' && c <= 'z')
		upper = upper || (c >= 'A' && c <= 'Z')
		hex = hex && strings.ContainsRune("0123456789abcdefABCDEF", c)
		seen[c] = true
	}
	return (hex && len(s) >= 32) || (digit && lower && upper && len(seen) >= 14)
}

// Find returns the areas to redact for the enabled rules, each grown by pad. A hit that mostly
// overlaps an earlier one widens it instead of stacking a second rectangle.
func Find(lines []ocr.Line, enabled settings.RedactRules, pad float32) []Finding {
	var out []Finding
	for _, line := range lines {
		var text strings.Builder
		spans := make([][2]int, len(line.Words))
		for i, w := range line.Words {
			if i > 0 {
				text.WriteByte(' ')
			}
			spans[i][0] = text.Len()
			text.WriteString(w.Text)
			spans[i][1] = text.Len()
		}
		s := text.String()
		if enabled.Secrets && danglingLabel.MatchString(s) && len(line.Words) > 0 {
			last := render.R(line.Words[len(line.Words)-1].Bounds)
			h := line.Bounds.H
			out = append(out, Finding{Secret, render.R{X: last.Right() + h*0.2, Y: line.Bounds.Y, W: h * 24, H: h}.Inflate(pad)})
		}
		for _, r := range rules {
			if !kindInfo[r.kind].enabled(enabled) {
				continue
			}
			for _, m := range r.re.FindAllStringSubmatchIndex(s, -1) {
				start, end := m[2*r.group], m[2*r.group+1]
				if start < 0 || (r.validate != nil && !r.validate(s[start:end])) {
					continue
				}
				var rect render.R
				hit := false
				for i, sp := range spans {
					if sp[0] < end && sp[1] > start {
						if wr := render.R(line.Words[i].Bounds); hit {
							rect = rect.Union(wr)
						} else {
							rect, hit = wr, true
						}
					}
				}
				if !hit {
					continue
				}
				out = merge(out, Finding{r.kind, rect.Inflate(pad)})
			}
		}
	}
	return out
}

func merge(out []Finding, f Finding) []Finding {
	for i := range out {
		if in, ok := out[i].Rect.Intersect(f.Rect); ok && in.Area() > 0.6*min(f.Rect.Area(), out[i].Rect.Area()) {
			out[i].Rect = out[i].Rect.Union(f.Rect)
			return out
		}
	}
	return append(out, f)
}
