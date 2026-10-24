package trim

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wobsoriano/glint/internal/media"
)

// VideoExtensions are the files Trim & Compress opens.
var VideoExtensions = []string{"mp4", "mov", "m4v", "wmv", "avi"}

// IsVideo reports whether path has one of VideoExtensions, in any case.
func IsVideo(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	return ext != "" && slices.ContainsFunc(VideoExtensions, func(v string) bool { return strings.EqualFold(v, ext) })
}

// Source is the opened video.
type Source struct {
	Path          string
	Width, Height int
	FPS           int
	DurationMS    int64
	Bytes         int64
}

// Trim is the kept span of the source in milliseconds, at least MinTrimMS long.
type Trim struct{ StartMS, EndMS int64 }

const MinTrimMS = 250

func FullTrim(durationMS int64) Trim { return Trim{0, durationMS} }

func (t Trim) DurationMS() int64 { return t.EndMS - t.StartMS }

// WithStart moves the start, keeping MinTrimMS before the end.
func (t Trim) WithStart(ms int64) Trim {
	t.StartMS = max(min(ms, t.EndMS-MinTrimMS), 0)
	return t
}

// WithEnd moves the end, keeping MinTrimMS after the start and the end inside the source.
func (t Trim) WithEnd(ms, durationMS int64) Trim {
	lo := t.StartMS + MinTrimMS
	t.EndMS = min(max(ms, lo), max(durationMS, lo))
	return t
}

type Resolution int

const (
	Original Resolution = iota
	P1080
	P720
	P480
)

var Resolutions = []Resolution{Original, P1080, P720, P480}

func (r Resolution) Label() string {
	return [...]string{"Original", "1080p", "720p", "480p"}[r]
}

func (r Resolution) shortSide() int {
	return [...]int{0, 1080, 720, 480}[r]
}

type Quality int

const (
	High Quality = iota
	Medium
	Small
)

var Qualities = []Quality{High, Medium, Small}

func (q Quality) Label() string {
	return [...]string{"High", "Medium", "Small"}[q]
}

// bitsPerPixel is Studio's export rate for the quality: High, Medium and Low there.
func (q Quality) bitsPerPixel() float64 {
	return [...]float64{0.1, 0.065, 0.04}[q]
}

type Options struct {
	Resolution Resolution
	Quality    Quality
	Codec      media.VideoCodec
	KeepAudio  bool
}

func DefaultOptions() Options {
	return Options{Resolution: Original, Quality: Medium, Codec: media.H264, KeepAudio: true}
}

// OutputSize shrinks the short side to the resolution's, never upscales, and rounds both
// sides down to even.
func OutputSize(w, h int, r Resolution) (int, int) {
	short := min(w, h)
	if s := r.shortSide(); s > 0 && s < short {
		k := float64(s) / float64(short)
		return int(math.Round(float64(w)*k)) &^ 1, int(math.Round(float64(h)*k)) &^ 1
	}
	return w &^ 1, h &^ 1
}

// bitrate is Studio's export bitrate: bits per pixel per frame, clamped to 1.5..80 Mbps.
func bitrate(w, h, fps int, q Quality) int {
	return int(min(max(float64(w)*float64(h)*float64(fps)*q.bitsPerPixel(), 1_500_000), 80_000_000))
}

func videoBitrate(src Source, opts Options) int {
	w, h := OutputSize(src.Width, src.Height, opts.Resolution)
	return bitrate(w, h, src.FPS, opts.Quality)
}

// EstimateBytes is the output's size at the encoder's target rates.
func EstimateBytes(src Source, t Trim, opts Options, hasAudio bool) int64 {
	secs := float64(t.DurationMS()) / 1000
	bytes := float64(videoBitrate(src, opts)) / 8 * secs
	if hasAudio && opts.KeepAudio {
		bytes += media.AACBytesPerSec * secs
	}
	return int64(bytes)
}

func transcodeOptions(src Source, t Trim, opts Options) media.TranscodeOptions {
	w, h := OutputSize(src.Width, src.Height, opts.Resolution)
	return media.TranscodeOptions{
		Width: w, Height: h,
		Bitrate:   videoBitrate(src, opts),
		Codec:     opts.Codec,
		Trim:      &media.Span{StartMS: t.StartMS, EndMS: t.EndMS},
		DropAudio: !opts.KeepAudio,
	}
}

// OutputStem names the export after the source: "My clip (trimmed)".
func OutputStem(source string) string {
	stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	if stem == "" || stem == "." {
		stem = "Video"
	}
	return stem + " (trimmed)"
}

// Clock formats media time as m:ss.d, or h:mm:ss from an hour.
func Clock(ms int64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d.%d", s/60, s%60, ms%1000/100)
}

// HumanBytes is the Rust build's human_bytes: binary units, one decimal from MB.
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// sanitize makes name safe as a Windows file name, as the Rust build's shell::sanitize.
func sanitize(name string) string {
	s := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	s = strings.TrimRight(strings.TrimSpace(s), ".")
	if s == "" {
		return "Capture"
	}
	return s
}

// uniquePath is dir/stem.ext, or dir/stem (n).ext from 2 up when that exists.
func uniquePath(dir, stem, ext string) string {
	p := filepath.Join(dir, stem+"."+ext)
	for n := 2; exists(p); n++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d).%s", stem, n, ext))
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
