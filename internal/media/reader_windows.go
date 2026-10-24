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

	d3d     *mf.Device
	staging mf.Texture2D
	// gpuFrames counts frames that arrived as GPU textures.
	gpuFrames int
}

const noSkip = -1

// OpenFrameReader opens path's first video track. With a Direct3D 11 device the decoder
// runs on the GPU when the codec has a hardware decoder; without one it runs in software.
func OpenFrameReader(path string) (*FrameReader, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	if d3d, err := mf.CreateDevice(); err == nil {
		if fr, err := openFrameReader(path, d3d); err == nil {
			return fr, nil
		}
		d3d.Release()
	}
	return openFrameReader(path, nil)
}

func openFrameReader(path string, d3d *mf.Device) (*FrameReader, error) {
	attrs, err := mf.CreateAttributes(2)
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	if d3d != nil {
		err = attrs.SetUnknown(&mf.MF_SOURCE_READER_D3D_MANAGER, d3d.Manager)
		if err == nil {
			err = attrs.SetUINT32(&mf.MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS, 1)
		}
	} else {
		// Lets the reader convert any decoder output to NV12, not just decoders that emit it.
		err = attrs.SetUINT32(&mf.MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING, 1)
	}
	if err != nil {
		return nil, err
	}
	r, err := mf.CreateSourceReader(path, attrs)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	fr := &FrameReader{r: r, skipUntil: noSkip, d3d: d3d}
	if err := fr.init(); err != nil {
		r.Release()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return fr, nil
}

// Hardware reports whether the reader has a GPU device, so Media Foundation decodes on the
// GPU when the codec has a hardware decoder.
func (fr *FrameReader) Hardware() bool { return fr.d3d != nil }

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
	var err error
	if fr.d3d != nil {
		err = fr.copyFromTexture(s, f)
	} else {
		err = fr.copyFromMemory(s, f)
	}
	if err != nil {
		f.Release()
		return nil, err
	}
	return f, nil
}

func (fr *FrameReader) copyFromTexture(s mf.Sample, f *Nv12Frame) error {
	buf, err := s.BufferByIndex(0)
	if err != nil {
		return err
	}
	defer buf.Release()
	dxgi, ok := buf.AsDXGI()
	if !ok {
		// A codec without a hardware decoder decodes in software even with a device.
		return fr.copyFromMemory(s, f)
	}
	defer dxgi.Release()
	tex, err := dxgi.Texture()
	if err != nil {
		return err
	}
	defer tex.Release()
	sub, err := dxgi.SubresourceIndex()
	if err != nil {
		return err
	}
	desc := tex.Desc()
	if !fr.staging.IsNil() {
		if d := fr.staging.Desc(); d.Width != desc.Width || d.Height != desc.Height {
			fr.staging.Release()
			fr.staging = mf.Texture2D{}
		}
	}
	if fr.staging.IsNil() {
		if fr.staging, err = fr.d3d.StagingTexture(desc); err != nil {
			return err
		}
	}
	fr.d3d.CopySubresource(fr.staging, tex, sub)
	data, pitch, err := fr.d3d.Map(fr.staging)
	if err != nil {
		return err
	}
	defer fr.d3d.Unmap(fr.staging)
	rows := int(desc.Height)
	fr.gpuFrames++
	return copyPlanes(unsafe.Slice((*byte)(data), int(pitch)*rows*3/2), int(pitch), rows, f)
}

func (fr *FrameReader) copyFromMemory(s mf.Sample, f *Nv12Frame) error {
	buf, err := s.ContiguousBuffer()
	if err != nil {
		return err
	}
	defer buf.Release()
	if b2d, ok := buf.As2D(); ok {
		defer b2d.Release()
		scan0, pitch, err := b2d.Lock2D()
		if err != nil {
			return err
		}
		defer b2d.Unlock2D()
		n := int(pitch) * fr.planeRows * 3 / 2
		return copyPlanes(unsafe.Slice((*byte)(scan0), n), int(pitch), fr.planeRows, f)
	}
	data, _, cur, err := buf.Lock()
	if err != nil {
		return err
	}
	defer buf.Unlock()
	pitch := int(cur) * 2 / 3 / fr.planeRows
	return copyPlanes(unsafe.Slice((*byte)(data), cur), pitch, fr.planeRows, f)
}

// copyPlanes copies the visible part of a decoder's NV12 buffer, whose planes are pitch
// bytes wide and whose luma plane has rows rows, padding included.
func copyPlanes(src []byte, pitch, rows int, f *Nv12Frame) error {
	chromaRows := (f.Height + 1) / 2
	if pitch < f.Stride || rows < f.Height || len(src) < pitch*(rows+chromaRows) {
		return fmt.Errorf("decoded frame has %d bytes at pitch %d and %d rows, too few for %dx%d", len(src), pitch, rows, f.Width, f.Height)
	}
	for y := 0; y < f.Height; y++ {
		copy(f.Data[y*f.Stride:(y+1)*f.Stride], src[y*pitch:])
	}
	chroma := src[rows*pitch:]
	for y := 0; y < chromaRows; y++ {
		copy(f.Data[(f.Height+y)*f.Stride:(f.Height+y+1)*f.Stride], chroma[y*pitch:])
	}
	return nil
}

func (fr *FrameReader) Close() {
	if fr.r.IsNil() {
		return
	}
	if leave, err := mf.Enter(); err == nil {
		fr.r.Release()
		fr.staging.Release()
		if fr.d3d != nil {
			fr.d3d.Release()
		}
		leave()
	}
	fr.r, fr.staging, fr.d3d = mf.SourceReader{}, mf.Texture2D{}, nil
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
