package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Kind is what a capture holds. It decides the directory form: <id> for a screenshot and
// <id>.glintrec for a recording.
type Kind string

const (
	Screenshot Kind = "screenshot"
	Recording  Kind = "recording"
)

func (k *Kind) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	switch v := Kind(s); v {
	case Screenshot, Recording:
		*k = v
		return nil
	}
	return fmt.Errorf("unknown capture kind %q", s)
}

// Meta is a capture's meta.json. Times are local; Created never changes and names the
// capture's directory, Modified moves on every edit or rename.
type Meta struct {
	Version  uint32
	Kind     Kind
	Title    string
	Created  time.Time
	Modified time.Time
	// Width and Height are the pixels of base.png or of the screen master.
	Width, Height uint32
	DurationMs    *uint64
	// Source is the captured window's title or a display name such as "Display 1".
	Source       *string
	ImportedFrom *string
}

// metaJSON lists the fields in the order Rust writes them. Pointers tell a missing required
// field, which makes Rust reject the whole file, from a zero value.
type metaJSON struct {
	Version      *uint32     `json:"version"`
	Kind         *Kind       `json:"kind"`
	Title        *string     `json:"title"`
	Created      *chronoTime `json:"created"`
	Modified     *chronoTime `json:"modified"`
	Width        *uint32     `json:"width"`
	Height       *uint32     `json:"height"`
	DurationMs   *uint64     `json:"duration_ms"`
	Source       *string     `json:"source"`
	ImportedFrom *string     `json:"imported_from"`
}

func (m Meta) MarshalJSON() ([]byte, error) {
	created, modified := chronoTime(m.Created), chronoTime(m.Modified)
	return marshalNoHTMLEscape(metaJSON{
		Version: &m.Version, Kind: &m.Kind, Title: &m.Title, Created: &created, Modified: &modified,
		Width: &m.Width, Height: &m.Height, DurationMs: m.DurationMs, Source: m.Source, ImportedFrom: m.ImportedFrom,
	})
}

// marshalNoHTMLEscape keeps <, > and & literal, as serde_json does; the escaping
// json.Marshal applies inside a MarshalJSON method would survive the outer encoder.
func marshalNoHTMLEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

func (m *Meta) UnmarshalJSON(b []byte) error {
	var j metaJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	var missing []string
	for _, f := range []struct {
		name    string
		present bool
	}{
		{"version", j.Version != nil}, {"kind", j.Kind != nil}, {"title", j.Title != nil}, {"created", j.Created != nil},
		{"modified", j.Modified != nil}, {"width", j.Width != nil}, {"height", j.Height != nil},
	} {
		if !f.present {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing fields %q", missing)
	}
	*m = Meta{
		Version: *j.Version, Kind: *j.Kind, Title: *j.Title,
		Created: time.Time(*j.Created), Modified: time.Time(*j.Modified),
		Width: *j.Width, Height: *j.Height, DurationMs: j.DurationMs, Source: j.Source, ImportedFrom: j.ImportedFrom,
	}
	return nil
}

// chronoTime is a chrono DateTime<Local> on the wire: RFC 3339 with a numeric offset (never
// "Z") and 0, 3, 6 or 9 fraction digits, whichever is shortest without losing precision.
type chronoTime time.Time

func (t chronoTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(formatChrono(time.Time(t)))
}

func (t *chronoTime) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	*t = chronoTime(parsed.In(time.Local))
	return nil
}

func formatChrono(t time.Time) string {
	t = t.In(time.Local)
	ns := t.Nanosecond()
	var frac string
	switch {
	case ns == 0:
	case ns%1_000_000 == 0:
		frac = fmt.Sprintf(".%03d", ns/1_000_000)
	case ns%1_000 == 0:
		frac = fmt.Sprintf(".%06d", ns/1_000)
	default:
		frac = fmt.Sprintf(".%09d", ns)
	}
	_, off := t.Zone()
	sign := byte('+')
	if off < 0 {
		sign, off = '-', -off
	}
	offset := fmt.Sprintf("%c%02d:%02d", sign, off/3600, off/60%60)
	if off%60 != 0 {
		offset += fmt.Sprintf(":%02d", off%60)
	}
	return t.Format("2006-01-02T15:04:05") + frac + offset
}

var ErrEmptyTitle = errors.New("a title cannot be empty")
