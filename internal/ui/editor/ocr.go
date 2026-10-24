package editor

import (
	"fmt"
	"image"
	"slices"
	"strings"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/redact"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

// croppedBase is the part of the screenshot the crop keeps, in annotation coordinates.
func (m *model) croppedBase() *image.RGBA {
	w, h := m.baseSize()
	c := m.doc.CropRect(w, h)
	return m.base.SubImage(image.Rect(int(c.X), int(c.Y), int(c.Right()), int(c.Bottom()))).(*image.RGBA)
}

// addFindings hides what OCR found that no redaction covers yet, as one undo step, and says
// what it hid.
func (m *model) addFindings(findings []redact.Finding, kind render.RedactStyle) string {
	var fresh []redact.Finding
	for _, f := range findings {
		fr := render.R{X: f.Rect.X, Y: f.Rect.Y, W: f.Rect.W, H: f.Rect.H}
		covered := slices.ContainsFunc(m.doc.Annotations, func(a render.Annotation) bool {
			r, ok := a.Shape.(render.Redact)
			if !ok {
				return false
			}
			i, ok := r.Rect.Intersect(fr)
			return ok && i.Area() > 0.6*fr.Area()
		})
		if !covered {
			fresh = append(fresh, f)
		}
	}
	if len(fresh) == 0 {
		return "No sensitive info found"
	}
	var labels []string
	counts := map[string]int{}
	for _, f := range fresh {
		if counts[f.Kind.Label()] == 0 {
			labels = append(labels, f.Kind.Label())
		}
		counts[f.Kind.Label()]++
	}
	m.checkpoint()
	added := make([]render.Annotation, len(fresh))
	m.sel = nil
	for i, f := range fresh {
		id := m.newID()
		added[i] = render.Annotation{
			ID:    id,
			Shape: render.Redact{Rect: render.R{X: f.Rect.X, Y: f.Rect.Y, W: f.Rect.W, H: f.Rect.H}, Kind: kind, Strength: m.ink.redactStrength * m.doc.DPI},
			Color: render.RGB(0),
		}
		m.sel = append(m.sel, id)
	}
	m.doc.Annotations = append(added, m.doc.Annotations...)
	m.tool = render.ToolSelect
	parts := make([]string, len(labels))
	for i, l := range labels {
		n := counts[l]
		parts[i] = fmt.Sprintf("%d %s", n, l)
		if n > 1 {
			parts[i] += "s"
		}
	}
	return "Hid " + strings.Join(parts, ", ") + "; strength stays editable"
}

func (e *Editor) smartRedact() {
	if e.busy {
		return
	}
	e.busy = true
	e.toast("Scanning for sensitive info…")
	img := e.m.croppedBase()
	s := settings.Load()
	kind := s.RedactStyle
	pad := 2 * e.m.doc.DPI
	e.async(func() {
		lines, err := ocr.Recognize(img)
		var findings []redact.Finding
		if err == nil {
			findings = redact.Find(lines, s.RedactRules, pad)
		}
		e.update(func() {
			e.busy = false
			if err != nil {
				e.toast(err.Error())
				return
			}
			e.toast(e.m.addFindings(findings, kind))
		})
	})
}

func (e *Editor) copyText() {
	if e.busy {
		return
	}
	e.busy = true
	e.toast("Recognizing text…")
	img := e.m.croppedBase()
	e.async(func() {
		lines, err := ocr.Recognize(img)
		text := ocr.Text(lines)
		e.update(func() {
			e.busy = false
			switch {
			case err != nil:
				e.toast(err.Error())
			case strings.TrimSpace(text) == "":
				e.toast("No text found")
			default:
				e.clipboard = &text
				e.toast(fmt.Sprintf("Copied %d words of text", len(strings.Fields(text))))
			}
		})
	})
}
