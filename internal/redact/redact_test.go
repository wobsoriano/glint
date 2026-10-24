package redact

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/wobsoriano/glint/internal/settings"
)

// line lays words out 100px apart, each 90px wide and 20px tall, as the Rust tests do.
func line(text string) Line {
	var words []Word
	for i, t := range strings.Split(text, " ") {
		words = append(words, Word{Text: t, Rect: Rect{float32(i) * 100, 0, 90, 20}})
	}
	return Line{Words: words, Rect: Rect{0, 0, 100 * float32(len(words)), 20}}
}

func kindsIn(text string, rules settings.RedactRules) []Kind {
	var out []Kind
	for _, f := range Find([]Line{line(text)}, rules, 0) {
		out = append(out, f.Kind)
	}
	return out
}

func kinds(text string) []Kind { return kindsIn(text, settings.DefaultRedactRules()) }

func TestDetectsCommonSecrets(t *testing.T) {
	for _, c := range []struct {
		text string
		want []Kind
		any  bool
	}{
		{"contact me at jane.doe@example.com today", []Kind{Email}, false},
		{"card 4242 4242 4242 4242 exp", []Kind{Card}, false},
		{"key sk_live_51HxYzAbCdEfGhIjKlMn here", []Kind{Secret}, true},
		{"token ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789", []Kind{Secret}, true},
		{"server 192.168.1.20 is up", []Kind{Network}, false},
		{"call +1 415-555-0132 now", []Kind{Phone}, false},
		{"password: hunter2", []Kind{Secret}, true},
	} {
		got := kinds(c.text)
		if c.any && !slices.Contains(got, c.want[0]) || !c.any && !slices.Equal(got, c.want) {
			t.Errorf("%q found %v, want %v", c.text, got, c.want)
		}
	}
}

func TestLabeledSecretCoversATokenOCRSplitInTwo(t *testing.T) {
	found := Find([]Line{line("API key: sk_live_51 HxYzAbCdEfGhIjKlMnOpQr")}, settings.DefaultRedactRules(), 0)
	if len(found) != 1 || found[0].Kind != Secret {
		t.Fatalf("found %+v", found)
	}
	if r := found[0].Rect; r.X != 200 || r.Right() != 390 {
		t.Errorf("covers %v to %v, want 200 to 390", r.X, r.Right())
	}
}

func TestIgnoresPlainText(t *testing.T) {
	for _, text := range []string{
		"Hello world, version 1.2.3 released on 2026-10-14",
		"card 4242 4242 4242 4241",
	} {
		if got := kinds(text); len(got) != 0 {
			t.Errorf("%q found %v", text, got)
		}
	}
}

func TestMoreKinds(t *testing.T) {
	for _, c := range []struct {
		text string
		want Kind
	}{
		{"ssn 123-45-6789 on file", ID},
		{"IBAN GB82 WEST 1234 5698 7654 32", ID},
		{"mac 00:1A:2B:3C:4D:5E", Network},
		{"v6 2001:0db8:85a3:0000:0000:8a2e:0370:7334", Network},
		{"go to https://example.com/x?sig=abcdef&y=1", URL},
		{"aws AKIAIOSFODNN7EXAMPLE", Secret},
		{"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N", Secret},
		{"blob 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", Secret},
		{"mail bob @ example.org", Email},
		{"call (415) 555-0132", Phone},
	} {
		if got := kinds(c.text); !slices.Contains(got, c.want) {
			t.Errorf("%q found %v, want %v", c.text, got, c.want)
		}
	}
}

func TestValidatorsRejectLookalikes(t *testing.T) {
	for _, text := range []string{
		"order 1234 5678 9012 3456",
		"ip 256.1.1.1",
		"build abcdefghijklmnopqrstuvwxyzabcdef",
		"call +1 23 45",
	} {
		if got := kinds(text); len(got) != 0 {
			t.Errorf("%q found %v", text, got)
		}
	}
	if !luhn("4111-1111-1111-1111") || luhn("4111 1111 1111 1112") || luhn("424242424242") {
		t.Error("luhn")
	}
}

func TestDisabledRulesFindNothing(t *testing.T) {
	text := "jane@example.com 192.168.1.20 password: x"
	if got := kindsIn(text, settings.RedactRules{}); len(got) != 0 {
		t.Errorf("all rules off found %v", got)
	}
	rules := settings.DefaultRedactRules()
	rules.Emails, rules.Secrets = false, false
	if got := kindsIn(text, rules); !slices.Equal(got, []Kind{Network}) {
		t.Errorf("only network on found %v", got)
	}
}

func TestDanglingLabelCoversTheSpaceAfterIt(t *testing.T) {
	l := Line{
		Words: []Word{{"Password:", Rect{10, 40, 80, 20}}},
		Rect:  Rect{10, 40, 80, 20},
	}
	found := Find([]Line{l}, settings.DefaultRedactRules(), 2)
	if len(found) != 1 || found[0].Kind != Secret {
		t.Fatalf("found %+v", found)
	}
	if want := (Rect{90 + 4 - 2, 40 - 2, 480 + 4, 20 + 4}); found[0].Rect != want {
		t.Errorf("rect %+v, want %+v", found[0].Rect, want)
	}
}

func TestOverlappingHitsMergeAcrossRules(t *testing.T) {
	// The labeled rule and the high-entropy rule both hit the token; one rectangle results.
	found := Find([]Line{line("token: Zq9xW2vB7nM4kL1pR8sT3yU6aD5fG0hJ")}, settings.DefaultRedactRules(), 0)
	if len(found) != 1 {
		t.Fatalf("found %+v", found)
	}
	two := Find([]Line{line("a@b.co"), {Words: []Word{{"c@d.co", Rect{0, 100, 90, 20}}}, Rect: Rect{0, 100, 90, 20}}}, settings.DefaultRedactRules(), 0)
	if len(two) != 2 {
		t.Errorf("hits on separate lines merged: %+v", two)
	}
}

// testdata/redact-findings.json is what the Rust build's ignored go_fixtures test in
// src/redact.rs found in testdata/corpus.txt, one OCR line per text line, 30px apart.
func TestMatchesRustOnTheCorpus(t *testing.T) {
	corpus, err := os.ReadFile("testdata/corpus.txt")
	if err != nil {
		t.Fatal(err)
	}
	var lines []Line
	for n, text := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(corpus), "\r\n", "\n"), "\n"), "\n") {
		l := line(text)
		dy := float32(n) * 30
		l.Rect.Y += dy
		for i := range l.Words {
			l.Words[i].Rect.Y += dy
		}
		lines = append(lines, l)
	}
	var rust []struct {
		Kind string
		Rect [4]float32
	}
	data, _ := os.ReadFile("testdata/redact-findings.json")
	if err := json.Unmarshal(data, &rust); err != nil {
		t.Fatal(err)
	}
	names := map[Kind]string{Email: "Email", Phone: "Phone", Card: "Card", Secret: "Secret", Network: "Network", ID: "Id", URL: "Url"}
	var got, want []string
	for _, f := range Find(lines, settings.DefaultRedactRules(), 2) {
		got = append(got, fmt.Sprintf("%s %v", names[f.Kind], [4]float32{f.Rect.X, f.Rect.Y, f.Rect.W, f.Rect.H}))
	}
	for _, f := range rust {
		want = append(want, fmt.Sprintf("%s %v", f.Kind, f.Rect))
	}
	if !slices.Equal(got, want) {
		t.Errorf("Go found\n%s\nRust found\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(want) < 30 {
		t.Errorf("only %d Rust findings", len(want))
	}
}

func TestLabels(t *testing.T) {
	for k := Email; k <= URL; k++ {
		if k.Label() == "" {
			t.Errorf("kind %d has no label", k)
		}
	}
	if Card.Label() != "card number" || Network.Label() != "IP/MAC address" {
		t.Error("labels differ from the Rust build")
	}
}
