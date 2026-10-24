package media

import (
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/wobsoriano/glint/internal/media/internal/mf"
)

// FrameReader decodes the first video track of an MP4, MOV or M4V to NV12, sequentially and
// with seeking.
type FrameReader struct {
	r          mf.SourceReader
	width      int
	height     int
	fps        float64
	durationMS int64
	matrix     yuvMatrix
	// planeRows is the decoder's luma rows per buffer, padding included; the chroma plane
	// starts after them.
	planeRows int
	skipUntil int64
}

const noSkip = -1

// OpenFrameReader opens path's first video track.
func OpenFrameReader(path string) (*FrameReader, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	attrs, err := mf.CreateAttributes(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	// Lets the reader convert any decoder output to NV12, not just decoders that emit it.
	if err := attrs.SetUINT32(&mf.MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING, 1); err != nil {
		return nil, err
	}
	r, err := mf.CreateSourceReader(path, attrs)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	fr := &FrameReader{r: r, skipUntil: noSkip}
	if err := fr.init(); err != nil {
		r.Release()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return fr, nil
}

func (fr *FrameReader) init() error {
	r := fr.r
	if err := r.SetStreamSelection(mf.AllStreams, false); err != nil {
		return err
	}
	if err := r.SetStreamSelection(mf.FirstVideoStream, true); err != nil {
		return fmt.Errorf("no video track: %w", err)
	}
	native, err := r.NativeMediaType(mf.FirstVideoStream, 0)
	if err != nil {
		return err
	}
	size, err := native.GetUINT64(&mf.MF_MT_FRAME_SIZE)
	if err == nil {
		w, h := mf.Unpack2(size)
		fr.width, fr.height = int(w), int(h)
		fr.fps = 30
		if rate, err := native.GetUINT64(&mf.MF_MT_FRAME_RATE); err == nil {
			if n, d := mf.Unpack2(rate); n > 0 {
				fr.fps = float64(n) / float64(max(d, 1))
			}
		}
	}
	native.Release()
	if err != nil {
		return err
	}
	out, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Video},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFVideoFormat_NV12},
	)
	if err != nil {
		return err
	}
	err = r.SetCurrentMediaType(mf.FirstVideoStream, out)
	out.Release()
	if err != nil {
		return fmt.Errorf("decode to NV12: %w", err)
	}
	if err := fr.readOutputType(); err != nil {
		return err
	}
	d, err := r.Duration()
	if err != nil {
		return err
	}
	fr.durationMS = d / 10_000
	return nil
}

// readOutputType picks up the decoder's current output: the plane layout and the colour
// matrix it tags.
func (fr *FrameReader) readOutputType() error {
	t, err := fr.r.CurrentMediaType(mf.FirstVideoStream)
	if err != nil {
		return err
	}
	defer t.Release()
	fr.planeRows = fr.height
	if size, err := t.GetUINT64(&mf.MF_MT_FRAME_SIZE); err == nil {
		_, h := mf.Unpack2(size)
		fr.planeRows = max(int(h), fr.height)
	}
	m, err := t.GetUINT32(&mf.MF_MT_YUV_MATRIX)
	known := err == nil
	rng, _ := t.GetUINT32(&mf.MF_MT_VIDEO_NOMINAL_RANGE)
	fr.matrix = pickMatrix(known && m == mf.MFVideoTransferMatrix_BT709, known && m == mf.MFVideoTransferMatrix_BT601,
		rng == mf.MFNominalRange_0_255, fr.height)
	return nil
}

func (fr *FrameReader) Size() (width, height int) { return fr.width, fr.height }
func (fr *FrameReader) FPS() float64              { return fr.fps }
func (fr *FrameReader) DurationMS() int64         { return fr.durationMS }

// SeekMS positions the reader so the next frame is the first one at or after ms.
func (fr *FrameReader) SeekMS(ms int64) error {
	leave, err := mf.Enter()
	if err != nil {
		return err
	}
	defer leave()
	if err := fr.r.SetPosition(ms * 10_000); err != nil {
		return err
	}
	fr.skipUntil = ms
	return nil
}

// NextFrame decodes the next frame. It returns io.EOF at the end of the track.
func (fr *FrameReader) NextFrame() (*Nv12Frame, error) {
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	halfFrame := int64(500 / max(fr.fps, 1))
	for {
		sample, t, err := fr.readSample()
		if err != nil {
			return nil, err
		}
		if fr.skipUntil != noSkip {
			if t+halfFrame < fr.skipUntil {
				sample.Release()
				continue
			}
			fr.skipUntil = noSkip
		}
		f, err := fr.copyOut(t, sample)
		sample.Release()
		return f, err
	}
}

func (fr *FrameReader) readSample() (mf.Sample, int64, error) {
	for {
		s, flags, t, err := fr.r.ReadSample(mf.FirstVideoStream)
		if err != nil {
			return s, 0, err
		}
		if flags&mf.ReaderFlagMediaTypeChanged != 0 {
			if err := fr.readOutputType(); err != nil {
				s.Release()
				return mf.Sample{}, 0, err
			}
		}
		if flags&mf.ReaderFlagEndOfStream != 0 {
			s.Release()
			return mf.Sample{}, 0, io.EOF
		}
		if flags&mf.ReaderFlagError != 0 {
			s.Release()
			return mf.Sample{}, 0, errors.New("video stream error")
		}
		if !s.IsNil() {
			return s, max(t, 0) / 10_000, nil
		}
	}
}

func (fr *FrameReader) copyOut(t int64, s mf.Sample) (*Nv12Frame, error) {
	w, h := fr.width, fr.height
	stride := (w + 1) &^ 1
	chromaRows := (h + 1) / 2
	f := &Nv12Frame{TMS: t, Width: w, Height: h, Stride: stride, matrix: fr.matrix}
	f.Data = takeBuffer(stride * (h + chromaRows))

	buf, err := s.ContiguousBuffer()
	if err != nil {
		return nil, err
	}
	defer buf.Release()
	var base unsafe.Pointer
	var pitch, avail int
	b2d, is2D := buf.As2D()
	if is2D {
		defer b2d.Release()
		scan0, p, err := b2d.Lock2D()
		if err != nil {
			return nil, err
		}
		defer b2d.Unlock2D()
		base, pitch = scan0, int(p)
		avail = pitch * fr.planeRows * 3 / 2
	} else {
		data, _, cur, err := buf.Lock()
		if err != nil {
			return nil, err
		}
		defer buf.Unlock()
		base, avail = data, int(cur)
		pitch = int(cur) * 2 / 3 / fr.planeRows
	}
	if pitch < stride || avail < pitch*(fr.planeRows+chromaRows) {
		return nil, fmt.Errorf("decoded frame has %d bytes at pitch %d, too few for %dx%d", avail, pitch, w, h)
	}
	src := unsafe.Slice((*byte)(base), avail)
	for y := 0; y < h; y++ {
		copy(f.Data[y*stride:(y+1)*stride], src[y*pitch:])
	}
	chroma := src[fr.planeRows*pitch:]
	for y := 0; y < chromaRows; y++ {
		copy(f.Data[(h+y)*stride:(h+y+1)*stride], chroma[y*pitch:])
	}
	return f, nil
}

func (fr *FrameReader) Close() {
	if fr.r.IsNil() {
		return
	}
	if leave, err := mf.Enter(); err == nil {
		fr.r.Release()
		leave()
	}
	fr.r = mf.SourceReader{}
}

// AudioReader decodes the first audio track of an MP4 to interleaved float PCM, one decoder
// chunk at a time (about 21 ms for AAC), sequentially and with seeking.
type AudioReader struct {
	r          mf.SourceReader
	sampleRate int
	channels   int
	skipUntil  int64
}

// OpenAudioReader opens path's first audio track. It returns ErrNoAudio when there is none.
func OpenAudioReader(path string) (*AudioReader, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	r, err := mf.CreateSourceReader(path, mf.Attributes{})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	ar := &AudioReader{r: r, skipUntil: noSkip}
	if err := ar.init(); err != nil {
		r.Release()
		if errors.Is(err, ErrNoAudio) {
			return nil, err
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return ar, nil
}

func (ar *AudioReader) init() error {
	r := ar.r
	if err := r.SetStreamSelection(mf.AllStreams, false); err != nil {
		return err
	}
	if err := r.SetStreamSelection(mf.FirstAudioStream, true); err != nil {
		if mf.IsHR(err, mf.MF_E_INVALIDSTREAMNUMBER) {
			return ErrNoAudio
		}
		return err
	}
	out, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Audio},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFAudioFormat_Float},
	)
	if err != nil {
		return err
	}
	err = r.SetCurrentMediaType(mf.FirstAudioStream, out)
	out.Release()
	if err != nil {
		return fmt.Errorf("decode audio to float: %w", err)
	}
	cur, err := r.CurrentMediaType(mf.FirstAudioStream)
	if err != nil {
		return err
	}
	defer cur.Release()
	rate, err := cur.GetUINT32(&mf.MF_MT_AUDIO_SAMPLES_PER_SECOND)
	if err != nil {
		return err
	}
	channels, err := cur.GetUINT32(&mf.MF_MT_AUDIO_NUM_CHANNELS)
	if err != nil {
		return err
	}
	if bits, err := cur.GetUINT32(&mf.MF_MT_AUDIO_BITS_PER_SAMPLE); err != nil || bits != 32 {
		return errors.New("audio decoder did not produce 32-bit float")
	}
	ar.sampleRate, ar.channels = int(rate), int(channels)
	return nil
}

func (ar *AudioReader) SampleRate() int { return ar.sampleRate }
func (ar *AudioReader) Channels() int   { return ar.channels }

// SeekMS positions the reader so the next chunk starts on the frame at ms. Decoding resumes
// a little earlier: an AAC block decoded without its predecessor comes out faded and the
// dropped run-up absorbs that.
func (ar *AudioReader) SeekMS(ms int64) error {
	leave, err := mf.Enter()
	if err != nil {
		return err
	}
	defer leave()
	if err := ar.r.SetPosition(max(ms-100, 0) * 10_000); err != nil {
		return err
	}
	ar.skipUntil = sampleIndex(ms, ar.sampleRate)
	return nil
}

// NextChunk appends the next decoded chunk to dst. It returns io.EOF, with dst unchanged,
// once the track has ended.
func (ar *AudioReader) NextChunk(dst []float32) ([]float32, error) {
	leave, err := mf.Enter()
	if err != nil {
		return dst, err
	}
	defer leave()
	channels := max(ar.channels, 1)
	for {
		s, flags, ts, err := ar.r.ReadSample(mf.FirstAudioStream)
		if err != nil {
			return dst, err
		}
		if flags&mf.ReaderFlagEndOfStream != 0 {
			s.Release()
			return dst, io.EOF
		}
		if flags&mf.ReaderFlagError != 0 {
			s.Release()
			return dst, errors.New("audio stream error")
		}
		if s.IsNil() {
			continue
		}
		before := len(dst)
		dst, err = appendFloats(dst, s)
		s.Release()
		if err != nil {
			return dst[:before], err
		}
		if ar.skipUntil != noSkip {
			at := (max(ts, 0)*int64(ar.sampleRate) + 5_000_000) / 10_000_000
			skip := int(max(ar.skipUntil-at, 0)) * channels
			if before+skip >= len(dst) {
				dst = dst[:before]
				continue
			}
			dst = append(dst[:before], dst[before+skip:]...)
			ar.skipUntil = noSkip
		}
		return dst, nil
	}
}

func appendFloats(dst []float32, s mf.Sample) ([]float32, error) {
	buf, err := s.ContiguousBuffer()
	if err != nil {
		return dst, err
	}
	defer buf.Release()
	data, _, n, err := buf.Lock()
	if err != nil {
		return dst, err
	}
	dst = append(dst, unsafe.Slice((*float32)(data), n/4)...)
	return dst, buf.Unlock()
}

func (ar *AudioReader) Close() {
	if ar.r.IsNil() {
		return
	}
	if leave, err := mf.Enter(); err == nil {
		ar.r.Release()
		leave()
	}
	ar.r = mf.SourceReader{}
}
