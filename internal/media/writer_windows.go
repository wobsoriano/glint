package media

import (
	"errors"
	"fmt"
	"image"
	"os"
	"time"
	"unsafe"

	"github.com/wobsoriano/glint/internal/media/internal/mf"
)

// VideoWriter muxes H.264 or HEVC video and optional AAC audio into an MP4 through the
// Media Foundation sink writer.
//
// Every stream is BT.709 limited range and tagged so in the output type, so the encoder
// writes the colour space into the bitstream and decoders stop guessing it from the frame
// size (Media Foundation assumes BT.601 below 720 rows).
type VideoWriter struct {
	sink        mf.SinkWriter
	path        string
	video       VideoParams
	audio       AudioParams
	hasAudio    bool
	videoStream uint32
	audioStream uint32
	frames      int64
	audioFrames int64
	hardware    bool
}

// CreateVideoWriter starts an MP4 at path. audio is nil for a silent file.
func CreateVideoWriter(path string, video VideoParams, audio *AudioParams) (*VideoWriter, error) {
	if video.Width <= 0 || video.Height <= 0 || video.Width%2 != 0 || video.Height%2 != 0 {
		return nil, fmt.Errorf("video size must be even and non-zero, got %dx%d", video.Width, video.Height)
	}
	if video.FPS <= 0 || video.Bitrate <= 0 {
		return nil, fmt.Errorf("video needs a positive fps and bitrate, got %d fps at %d bps", video.FPS, video.Bitrate)
	}
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	var hwErr error
	if !video.Software {
		w, err := openSink(path, video, audio, true)
		if err == nil {
			return w, nil
		}
		hwErr = err
		os.Remove(path)
	}
	w, err := openSink(path, video, audio, false)
	if err != nil {
		if hwErr != nil {
			return nil, fmt.Errorf("create %s: %w (with hardware transforms: %v)", path, err, hwErr)
		}
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return w, nil
}

const amdVendor = "VEN_1002"

func openSink(path string, video VideoParams, audio *AudioParams, hardware bool) (_ *VideoWriter, err error) {
	attrs, err := mf.CreateAttributes(2)
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	if err := attrs.SetUINT32(&mf.MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS, boolU32(hardware)); err != nil {
		return nil, err
	}
	// The container comes from this, not the file name, so partial files can be written
	// under any extension.
	if err := attrs.SetGUID(&mf.MF_TRANSCODE_CONTAINERTYPE, &mf.MFTranscodeContainerType_MPEG4); err != nil {
		return nil, err
	}
	sink, err := mf.CreateSinkWriter(path, attrs)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			sink.Release()
		}
	}()
	w := &VideoWriter{sink: sink, path: path, video: video}

	subtype, profile := &mf.MFVideoFormat_H264, uint32(mf.AVEncH264VProfile_High)
	if video.Codec == HEVC {
		subtype, profile = &mf.MFVideoFormat_HEVC, mf.AVEncH265VProfile_Main_420_8
	}
	out, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Video},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: subtype},
		mf.TypeSetting{Key: &mf.MF_MT_AVG_BITRATE, Value: uint32(video.Bitrate)},
		mf.TypeSetting{Key: &mf.MF_MT_INTERLACE_MODE, Value: uint32(mf.MFVideoInterlace_Progressive)},
		mf.TypeSetting{Key: &mf.MF_MT_FRAME_SIZE, Value: mf.Pack2(uint32(video.Width), uint32(video.Height))},
		mf.TypeSetting{Key: &mf.MF_MT_FRAME_RATE, Value: mf.Pack2(uint32(video.FPS), 1)},
		mf.TypeSetting{Key: &mf.MF_MT_PIXEL_ASPECT_RATIO, Value: mf.Pack2(1, 1)},
		mf.TypeSetting{Key: &mf.MF_MT_MPEG2_PROFILE, Value: profile},
		mf.TypeSetting{Key: &mf.MF_MT_YUV_MATRIX, Value: uint32(mf.MFVideoTransferMatrix_BT709)},
		mf.TypeSetting{Key: &mf.MF_MT_VIDEO_NOMINAL_RANGE, Value: uint32(mf.MFNominalRange_16_235)},
		mf.TypeSetting{Key: &mf.MF_MT_VIDEO_PRIMARIES, Value: uint32(mf.MFVideoPrimaries_BT709)},
		mf.TypeSetting{Key: &mf.MF_MT_TRANSFER_FUNCTION, Value: uint32(mf.MFVideoTransFunc_709)},
	)
	if err != nil {
		return nil, err
	}
	w.videoStream, err = sink.AddStream(out)
	out.Release()
	if err != nil {
		return nil, fmt.Errorf("add %v stream: %w", video.Codec, err)
	}
	in, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Video},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFVideoFormat_NV12},
		mf.TypeSetting{Key: &mf.MF_MT_INTERLACE_MODE, Value: uint32(mf.MFVideoInterlace_Progressive)},
		mf.TypeSetting{Key: &mf.MF_MT_FRAME_SIZE, Value: mf.Pack2(uint32(video.Width), uint32(video.Height))},
		mf.TypeSetting{Key: &mf.MF_MT_FRAME_RATE, Value: mf.Pack2(uint32(video.FPS), 1)},
		mf.TypeSetting{Key: &mf.MF_MT_PIXEL_ASPECT_RATIO, Value: mf.Pack2(1, 1)},
		mf.TypeSetting{Key: &mf.MF_MT_ALL_SAMPLES_INDEPENDENT, Value: uint32(1)},
		mf.TypeSetting{Key: &mf.MF_MT_DEFAULT_STRIDE, Value: uint32(video.Width)},
	)
	if err != nil {
		return nil, err
	}
	// Windows' software H.264 encoder emits B-frames, and the MP4 sink then delays every
	// presentation time by a frame instead of writing an edit list, so the video would start
	// a frame after the audio. Without B-frames the first frame shows at 0.
	params, err := mf.CreateAttributes(1)
	if err != nil {
		in.Release()
		return nil, err
	}
	err = params.SetUINT32(&mf.CODECAPI_AVEncMPVDefaultBPictureCount, 0)
	if err == nil {
		err = sink.SetInputMediaType(w.videoStream, in, params)
	}
	params.Release()
	in.Release()
	if err != nil {
		return nil, fmt.Errorf("NV12 input for %v: %w", video.Codec, err)
	}
	if audio != nil {
		w.audio, w.hasAudio = *audio, true
		if w.audioStream, err = addAACStream(sink, *audio); err != nil {
			return nil, err
		}
	}

	vendor := ""
	if enc, err := encoderAttributes(sink, w.videoStream); err == nil {
		url, _ := enc.GetString(&mf.MFT_ENUM_HARDWARE_URL_Attribute)
		vendor, _ = enc.GetString(&mf.MFT_ENUM_HARDWARE_VENDOR_ID_Attribute)
		w.hardware = url != ""
		enc.Release()
	}
	if video.Codec == H264 && video.FPS > 30 && vendor == amdVendor {
		// AMD's H.264 MFT budgets bits as if every stream ran at 30 fps, so a 60 fps file
		// comes out at twice the bitrate (measured on an RX 6700 XT: 2.00x at 60, 4.06x at
		// 120, 1.00x at or below 30). Its HEVC MFT does not have the fault.
		api, err := sink.Service(w.videoStream, &mf.IID_ICodecAPI)
		if err != nil {
			return nil, fmt.Errorf("AMD H.264 encoder has no ICodecAPI: %w", err)
		}
		err = mf.CodecAPI{Unknown: api}.SetUINT32(&mf.CODECAPI_AVEncCommonMeanBitRate, uint32(int64(video.Bitrate)*30/int64(video.FPS)))
		api.Release()
		if err != nil {
			return nil, fmt.Errorf("set mean bitrate: %w", err)
		}
	}
	if err := sink.BeginWriting(); err != nil {
		return nil, err
	}
	return w, nil
}

func addAACStream(sink mf.SinkWriter, a AudioParams) (uint32, error) {
	out, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Audio},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFAudioFormat_AAC},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_BITS_PER_SAMPLE, Value: uint32(16)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_SAMPLES_PER_SECOND, Value: uint32(a.SampleRate)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_NUM_CHANNELS, Value: uint32(a.Channels)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_AVG_BYTES_PER_SECOND, Value: uint32(AACBytesPerSec)},
	)
	if err != nil {
		return 0, err
	}
	stream, err := sink.AddStream(out)
	out.Release()
	if err != nil {
		return 0, fmt.Errorf("add AAC stream: %w", err)
	}
	in, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Audio},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFAudioFormat_PCM},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_BITS_PER_SAMPLE, Value: uint32(16)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_SAMPLES_PER_SECOND, Value: uint32(a.SampleRate)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_NUM_CHANNELS, Value: uint32(a.Channels)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_BLOCK_ALIGNMENT, Value: uint32(a.Channels * 2)},
		mf.TypeSetting{Key: &mf.MF_MT_AUDIO_AVG_BYTES_PER_SECOND, Value: uint32(a.SampleRate * a.Channels * 2)},
		mf.TypeSetting{Key: &mf.MF_MT_ALL_SAMPLES_INDEPENDENT, Value: uint32(1)},
	)
	if err != nil {
		return 0, err
	}
	err = sink.SetInputMediaType(stream, in, mf.Attributes{})
	in.Release()
	if err != nil {
		return 0, fmt.Errorf("PCM input for AAC at %d Hz, %d channels: %w", a.SampleRate, a.Channels, err)
	}
	return stream, nil
}

func encoderAttributes(sink mf.SinkWriter, stream uint32) (mf.Attributes, error) {
	u, err := sink.Service(stream, &mf.IID_IMFTransform)
	if err != nil {
		return mf.Attributes{}, err
	}
	defer u.Release()
	return mf.Transform{Unknown: u}.Attributes()
}

func boolU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// Hardware reports whether a GPU encoder is writing the video.
func (w *VideoWriter) Hardware() bool { return w.hardware }

func (w *VideoWriter) Frames() int64 { return w.frames }

// WriteFrame appends the next frame, timed by its index. Its size must match the writer's.
// Alpha is ignored.
func (w *VideoWriter) WriteFrame(img *image.RGBA) error {
	if img.Rect.Dx() != w.video.Width || img.Rect.Dy() != w.video.Height {
		return fmt.Errorf("frame is %dx%d, writer expects %dx%d", img.Rect.Dx(), img.Rect.Dy(), w.video.Width, w.video.Height)
	}
	t := frameTime(w.frames, w.video.FPS)
	px := img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y):]
	return w.writePixels(px, img.Stride, orderRGBA, t, frameTime(w.frames+1, w.video.FPS)-t)
}

// WriteBGRA appends a frame of BGRA rows, stride bytes apart, shown at at. Capture
// sources such as cameras deliver this layout with their own clock.
func (w *VideoWriter) WriteBGRA(px []byte, stride int, at time.Duration) error {
	if stride < w.video.Width*4 || len(px) < stride*(w.video.Height-1)+w.video.Width*4 {
		return fmt.Errorf("BGRA frame of %d bytes at stride %d does not hold %dx%d", len(px), stride, w.video.Width, w.video.Height)
	}
	return w.writePixels(px, stride, orderBGRA, int64(at/100), frameTime(1, w.video.FPS))
}

func (w *VideoWriter) writePixels(px []byte, stride int, order pixelOrder, t, dur int64) error {
	if w.sink.IsNil() {
		return errors.New("video writer is closed")
	}
	leave, err := mf.Enter()
	if err != nil {
		return err
	}
	defer leave()
	n := w.video.Width * w.video.Height * 3 / 2
	err = w.writeSample(w.videoStream, n, t, dur, func(dst []byte) {
		toNV12(px, stride, w.video.Width, w.video.Height, order, dst)
	})
	if err != nil {
		return fmt.Errorf("write video frame %d: %w", w.frames, err)
	}
	w.frames++
	return nil
}

// writeSample fills a new n-byte buffer in place and queues it on stream.
func (w *VideoWriter) writeSample(stream uint32, n int, t, dur int64, fill func([]byte)) error {
	buf, err := mf.CreateMemoryBuffer(uint32(n))
	if err != nil {
		return err
	}
	defer buf.Release()
	data, _, _, err := buf.Lock()
	if err != nil {
		return err
	}
	fill(unsafe.Slice((*byte)(data), n))
	if err := buf.Unlock(); err != nil {
		return err
	}
	if err := buf.SetCurrentLength(uint32(n)); err != nil {
		return err
	}
	sample, err := mf.CreateSample()
	if err != nil {
		return err
	}
	defer sample.Release()
	if err := sample.AddBuffer(buf); err != nil {
		return err
	}
	if err := sample.SetSampleTime(t); err != nil {
		return err
	}
	if err := sample.SetSampleDuration(dur); err != nil {
		return err
	}
	return w.sink.WriteSample(stream, sample)
}

// WriteAudio appends interleaved PCM at the writer's sample rate and channel count.
func (w *VideoWriter) WriteAudio(interleaved []float32) error {
	if !w.hasAudio {
		return errors.New("this MP4 has no audio track")
	}
	if w.sink.IsNil() {
		return errors.New("video writer is closed")
	}
	frames := int64(len(interleaved) / w.audio.Channels)
	if frames == 0 {
		return nil
	}
	leave, err := mf.Enter()
	if err != nil {
		return err
	}
	defer leave()
	rate := int64(w.audio.SampleRate)
	t := w.audioFrames * 10_000_000 / rate
	end := (w.audioFrames + frames) * 10_000_000 / rate
	samples := interleaved[:frames*int64(w.audio.Channels)]
	err = w.writeSample(w.audioStream, len(samples)*2, t, end-t, func(dst []byte) {
		for i, s := range samples {
			v := int16(min(max(s, -1), 1) * 32767)
			dst[i*2], dst[i*2+1] = byte(v), byte(v>>8)
		}
	})
	if err != nil {
		return fmt.Errorf("write audio: %w", err)
	}
	w.audioFrames += frames
	return nil
}

// Finish completes the file and closes the writer.
func (w *VideoWriter) Finish() (WriteSummary, error) {
	if w.sink.IsNil() {
		return WriteSummary{}, errors.New("video writer is closed")
	}
	leave, err := mf.Enter()
	if err != nil {
		return WriteSummary{}, err
	}
	err = w.sink.Finalize()
	w.sink.Release()
	w.sink = mf.SinkWriter{}
	leave()
	if err != nil {
		return WriteSummary{}, fmt.Errorf("finalize %s: %w", w.path, err)
	}
	st, err := os.Stat(w.path)
	if err != nil {
		return WriteSummary{}, err
	}
	return WriteSummary{
		Frames:     w.frames,
		DurationMS: w.frames * 1000 / int64(w.video.FPS),
		Bytes:      st.Size(),
		Hardware:   w.hardware,
	}, nil
}

// Close abandons an unfinished file, leaving whatever was written. It does nothing after
// Finish.
func (w *VideoWriter) Close() {
	if w.sink.IsNil() {
		return
	}
	if leave, err := mf.Enter(); err == nil {
		w.sink.Release()
		leave()
	}
	w.sink = mf.SinkWriter{}
}
