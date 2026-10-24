package studio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/transcribe"
)

// Tracks lists the media files present in a session. The screen master always exists; the
// others are empty when absent.
type Tracks struct {
	Screen, Camera, Mic, System string
}

func findTracks(dir string) (Tracks, error) {
	screen := filepath.Join(dir, "screen.mp4")
	if !isFile(screen) {
		return Tracks{}, fmt.Errorf("%s has no screen.mp4", dir)
	}
	opt := func(name string) string {
		if p := filepath.Join(dir, name); isFile(p) {
			return p
		}
		return ""
	}
	return Tracks{Screen: screen, Camera: opt("camera.mp4"), Mic: opt("mic.wav"), System: opt("system.wav")}, nil
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// Session is an opened recording package. It holds everything Studio reads from the
// directory except decoded media and cursor sprites, which belong to the compositor.
type Session struct {
	Dir        string
	Project    Project
	Input      input.Timeline
	Captions   []CaptionWord
	Tracks     Tracks
	Transcript *transcribe.Transcript
	// SourceW and SourceH are the pixel size of the screen master.
	SourceW, SourceH uint32
	SourceDurationMs float64
	// Notices holds one sentence for the user per sidecar file that could not be read and was
	// renamed to `<name>.broken`, so the session opened with defaults in its place.
	Notices []string
}

// OpenSession opens a session directory given the screen master's size and duration, which
// the caller reads from screen.mp4. Without a readable project.json the project covers the
// whole recording with auto zooms generated from the recorded clicks.
func OpenSession(dir string, sourceW, sourceH uint32, sourceDurationMs float64) (*Session, error) {
	tracks, err := findTracks(dir)
	if err != nil {
		return nil, err
	}
	s := &Session{Dir: dir, Tracks: tracks, SourceW: sourceW, SourceH: sourceH, SourceDurationMs: sourceDurationMs}
	transcript, err := readSidecar(dir, transcribe.TranscriptFile, transcribe.Parse, &s.Notices)
	if err != nil {
		return nil, err
	}
	s.Transcript = transcript
	if err := s.loadAssets(); err != nil {
		return nil, err
	}
	project, err := readSidecar(dir, ProjectFile, func(b []byte) (Project, error) {
		return ParseProject(b, sourceDurationMs)
	}, &s.Notices)
	if err != nil {
		return nil, err
	}
	if project != nil {
		s.Project = *project
	} else {
		s.Project = NewProject(sourceDurationMs)
		s.Project.ApplyAutoZoom(&s.Input, sourceDurationMs)
	}
	if len(s.Project.CaptionCues) == 0 && s.Transcript != nil {
		s.Project.CaptionCues = CaptionCues(*s.Transcript)
	}
	return s, nil
}

func (s *Session) SaveProject() error { return s.Project.Save(s.Dir) }

// SetTranscript saves a new transcript and reloads the caption words from it. Give the
// project cues from CaptionCues to show it.
func (s *Session) SetTranscript(t transcribe.Transcript) error {
	if err := t.Save(filepath.Join(s.Dir, transcribe.TranscriptFile)); err != nil {
		return err
	}
	s.Transcript = &t
	return s.loadAssets()
}

// RenderDir holds the export cache and thumbnails; it is safe to delete.
func (s *Session) RenderDir() string { return filepath.Join(s.Dir, "render") }

func (s *Session) loadAssets() error {
	s.Captions = nil
	if s.Transcript != nil {
		s.Captions = CaptionWords(*s.Transcript)
	}
	if !isFile(filepath.Join(s.Dir, input.File)) {
		s.Input = input.Timeline{Version: 1, Size: [2]uint32{s.SourceW, s.SourceH}}
		return nil
	}
	in, err := input.Load(s.Dir)
	if err != nil {
		return fmt.Errorf("loading session assets in %s: %w", s.Dir, err)
	}
	s.Input = in
	return nil
}

func CaptionWords(t transcribe.Transcript) []CaptionWord {
	out := make([]CaptionWord, len(t.Words))
	for i, w := range t.Words {
		out[i] = CaptionWord{Text: w.Text, StartMs: float64(w.StartMs), EndMs: float64(w.EndMs)}
	}
	return out
}

func CaptionCues(t transcribe.Transcript) []CaptionCue {
	var out []CaptionCue
	for _, l := range transcribe.CaptionLines(t.Words, transcribe.DefaultCaptionOptions()) {
		out = append(out, CaptionCue{StartMs: float64(l.StartMs), EndMs: float64(l.EndMs), Text: l.Text})
	}
	return out
}

// readSidecar returns nil when the file is absent. A file that fails to parse is renamed to
// `<name>.broken` with a notice, so one bad edit never locks the user out of a recording.
func readSidecar[T any](dir, name string, parse func([]byte) (T, error), notices *[]string) (*T, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	v, perr := parse(data)
	if perr == nil {
		return &v, nil
	}
	broken := filepath.Join(dir, name+".broken")
	if err := os.Rename(path, broken); err != nil {
		return nil, fmt.Errorf("renaming %s to %s: %w", path, broken, err)
	}
	*notices = append(*notices, fmt.Sprintf("%s could not be read (%v), so it was renamed %s.broken and defaults are used", name, perr, name))
	return nil, nil
}
