package quicklook

import (
	"io"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/playback"
	"github.com/wobsoriano/glint/internal/store"
)

// openSound is what a recording sounds like: the audio track of its video when it has one,
// else its system.wav and mic.wav mixed, as the recorder writes them beside screen.mp4. A
// recording without either is silent: nil and no error.
func openSound(item store.Item) (playback.Source, func(), error) {
	if ar, err := media.OpenAudioReader(item.Deliverable()); err == nil {
		return ar, func() { ar.Close() }, nil
	}
	var tracks []*media.WavData
	for _, name := range []string{"system.wav", "mic.wav"} {
		path := filepath.Join(item.Dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		w, err := media.ReadWAV(path)
		if err != nil {
			return nil, nil, err
		}
		tracks = append(tracks, w)
	}
	if len(tracks) == 0 {
		return nil, nil, nil
	}
	return mix(tracks), func() {}, nil
}

// mix converts each track to stereo at playback.SampleRate and adds them, clipped to -1..1.
func mix(tracks []*media.WavData) *pcm {
	var out []float32
	for _, t := range tracks {
		rs := playback.NewResampler(t.SampleRate, t.Channels)
		rs.Push(t.Samples)
		buf := make([]float32, (int64(t.Frames())*playback.SampleRate/int64(max(t.SampleRate, 1))+2)*playback.Channels)
		buf = buf[:rs.Pull(buf, true)]
		if len(buf) > len(out) {
			out = append(out, make([]float32, len(buf)-len(out))...)
		}
		for i, v := range buf {
			out[i] += v
		}
	}
	for i, v := range out {
		out[i] = min(max(v, -1), 1)
	}
	return &pcm{samples: out}
}

// pcm is decoded stereo sound at playback.SampleRate, read as a playback.Source.
type pcm struct {
	samples []float32
	pos     int
}

func (p *pcm) SampleRate() int { return playback.SampleRate }
func (p *pcm) Channels() int   { return playback.Channels }

func (p *pcm) SeekMS(ms int64) error {
	p.pos = min(int(max(ms, 0)*playback.SampleRate/1000)*playback.Channels, len(p.samples))
	return nil
}

func (p *pcm) NextChunk(dst []float32) ([]float32, error) {
	if p.pos >= len(p.samples) {
		return dst, io.EOF
	}
	end := min(p.pos+4096*playback.Channels, len(p.samples))
	dst = append(dst, p.samples[p.pos:end]...)
	p.pos = end
	return dst, nil
}
