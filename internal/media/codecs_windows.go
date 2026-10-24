package media

import "github.com/wobsoriano/glint/internal/media/internal/mf"

// AvailableCodecs lists the codecs this machine can encode, on the GPU or in software.
// H.264 is always there on Windows; HEVC needs a hardware encoder or the HEVC Video
// Extensions.
func AvailableCodecs() []VideoCodec { return codecsWith(mf.MFT_CATEGORY_VIDEO_ENCODER) }

// DecodableCodecs lists the codecs this machine can decode. A machine can encode HEVC on its
// GPU and still lack the HEVC decoder package, so check this before reading an HEVC file.
func DecodableCodecs() []VideoCodec { return codecsWith(mf.MFT_CATEGORY_VIDEO_DECODER) }

func codecsWith(category mf.GUID) []VideoCodec {
	if mf.Startup() != nil {
		return nil
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil
	}
	defer leave()
	const flags = mf.MFT_ENUM_FLAG_HARDWARE | mf.MFT_ENUM_FLAG_SYNCMFT | mf.MFT_ENUM_FLAG_ASYNCMFT | mf.MFT_ENUM_FLAG_SORTANDFILTER
	var out []VideoCodec
	codecs := []struct {
		codec VideoCodec
		sub   mf.GUID
	}{{H264, mf.MFVideoFormat_H264}, {HEVC, mf.MFVideoFormat_HEVC}}
	for _, c := range codecs {
		info := &mf.TypeInfo{Major: mf.MFMediaType_Video, Sub: c.sub}
		in, outInfo := info, (*mf.TypeInfo)(nil)
		if category == mf.MFT_CATEGORY_VIDEO_ENCODER {
			in, outInfo = nil, info
		}
		if n, err := mf.CountTransforms(category, flags, in, outInfo); err == nil && n > 0 {
			out = append(out, c.codec)
		}
	}
	return out
}
