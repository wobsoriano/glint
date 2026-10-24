package mf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestGUIDsMatchSDK rereads every GUID this package uses from the Windows SDK headers. A
// wrong attribute key is not an error in Media Foundation; the setting is silently ignored.
func TestGUIDsMatchSDK(t *testing.T) {
	roots, _ := filepath.Glob(`C:\Program Files (x86)\Windows Kits\10\Include\10.*`)
	if len(roots) == 0 {
		t.Skip("Windows SDK headers are not installed")
	}
	root := roots[len(roots)-1]
	var src strings.Builder
	for _, dir := range []string{"um", "shared"} {
		files, _ := filepath.Glob(filepath.Join(root, dir, "*.h"))
		for _, f := range files {
			switch strings.ToLower(filepath.Base(f)) {
			case "mfapi.h", "mfidl.h", "mfreadwrite.h", "mftransform.h", "codecapi.h", "mfobjects.h",
				"icodecapi.h", "d3d11.h", "d3d11_4.h", "mmreg.h", "d3d9types.h":
				b, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				src.Write(b)
			}
		}
	}
	h := src.String()
	guids := map[string]*GUID{
		"MF_MT_MAJOR_TYPE": &MF_MT_MAJOR_TYPE, "MF_MT_SUBTYPE": &MF_MT_SUBTYPE, "MF_MT_AVG_BITRATE": &MF_MT_AVG_BITRATE,
		"MF_MT_INTERLACE_MODE": &MF_MT_INTERLACE_MODE, "MF_MT_FRAME_SIZE": &MF_MT_FRAME_SIZE, "MF_MT_FRAME_RATE": &MF_MT_FRAME_RATE,
		"MF_MT_PIXEL_ASPECT_RATIO": &MF_MT_PIXEL_ASPECT_RATIO, "MF_MT_MPEG2_PROFILE": &MF_MT_MPEG2_PROFILE,
		"MF_MT_YUV_MATRIX": &MF_MT_YUV_MATRIX, "MF_MT_VIDEO_NOMINAL_RANGE": &MF_MT_VIDEO_NOMINAL_RANGE,
		"MF_MT_VIDEO_PRIMARIES": &MF_MT_VIDEO_PRIMARIES, "MF_MT_TRANSFER_FUNCTION": &MF_MT_TRANSFER_FUNCTION,
		"MF_MT_DEFAULT_STRIDE": &MF_MT_DEFAULT_STRIDE, "MF_MT_ALL_SAMPLES_INDEPENDENT": &MF_MT_ALL_SAMPLES_INDEPENDENT,
		"MF_MT_AUDIO_BITS_PER_SAMPLE": &MF_MT_AUDIO_BITS_PER_SAMPLE, "MF_MT_AUDIO_SAMPLES_PER_SECOND": &MF_MT_AUDIO_SAMPLES_PER_SECOND,
		"MF_MT_AUDIO_NUM_CHANNELS": &MF_MT_AUDIO_NUM_CHANNELS, "MF_MT_AUDIO_AVG_BYTES_PER_SECOND": &MF_MT_AUDIO_AVG_BYTES_PER_SECOND,
		"MF_MT_AUDIO_BLOCK_ALIGNMENT":             &MF_MT_AUDIO_BLOCK_ALIGNMENT,
		"MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS": &MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS,
		"MF_TRANSCODE_CONTAINERTYPE":              &MF_TRANSCODE_CONTAINERTYPE, "MFTranscodeContainerType_MPEG4": &MFTranscodeContainerType_MPEG4,
		"MF_SINK_WRITER_D3D_MANAGER": &MF_SINK_WRITER_D3D_MANAGER, "MF_SOURCE_READER_D3D_MANAGER": &MF_SOURCE_READER_D3D_MANAGER,
		"MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING": &MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING,
		"MF_PD_DURATION": &MF_PD_DURATION, "MFT_ENUM_HARDWARE_URL_Attribute": &MFT_ENUM_HARDWARE_URL_Attribute,
		"MFT_ENUM_HARDWARE_VENDOR_ID_Attribute": &MFT_ENUM_HARDWARE_VENDOR_ID_Attribute,
		"MFMediaType_Video":                     &MFMediaType_Video, "MFMediaType_Audio": &MFMediaType_Audio,
		"MFT_CATEGORY_VIDEO_ENCODER": &MFT_CATEGORY_VIDEO_ENCODER, "MFT_CATEGORY_VIDEO_DECODER": &MFT_CATEGORY_VIDEO_DECODER,
		"AVEncMPVDefaultBPictureCount": &CODECAPI_AVEncMPVDefaultBPictureCount, "AVEncCommonMeanBitRate": &CODECAPI_AVEncCommonMeanBitRate,
		"IMFTransform": &IID_IMFTransform, "ICodecAPI": &IID_ICodecAPI, "IMF2DBuffer": &IID_IMF2DBuffer,
		"IMFDXGIBuffer": &IID_IMFDXGIBuffer, "ID3D11Texture2D": &IID_ID3D11Texture2D, "ID3D11Multithread": &IID_ID3D11Multithread,
	}
	for name, got := range guids {
		want, err := sdkGUID(h, name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if *got != want {
			t.Errorf("%s is %v here, %v in the SDK", name, *got, want)
		}
	}
	subtypes := map[string]*GUID{
		"MFVideoFormat_H264": &MFVideoFormat_H264, "MFVideoFormat_HEVC": &MFVideoFormat_HEVC, "MFVideoFormat_NV12": &MFVideoFormat_NV12,
		"MFVideoFormat_ARGB32": &MFVideoFormat_ARGB32, "MFAudioFormat_AAC": &MFAudioFormat_AAC, "MFAudioFormat_PCM": &MFAudioFormat_PCM,
		"MFAudioFormat_Float": &MFAudioFormat_Float,
	}
	for name, got := range subtypes {
		m := regexp.MustCompile(`DEFINE_MEDIATYPE_GUID\(\s*` + name + `\s*,\s*(FCC\('....'\)|\w+)`).FindStringSubmatch(h)
		if m == nil {
			t.Errorf("%s: not in the SDK", name)
			continue
		}
		code, err := sdkNumber(h, m[1])
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if want := formatGUID(code); *got != want {
			t.Errorf("%s is %v here, %v in the SDK", name, *got, want)
		}
	}
}

var hexNum = `(0x[0-9A-Fa-f]+)`

func sdkGUID(h, name string) (GUID, error) {
	list := `\s*` + hexNum + `\s*,\s*` + hexNum + `\s*,\s*` + hexNum + strings.Repeat(`\s*,\s*\{?\s*`+hexNum, 8)
	patterns := []string{
		`(?:DEFINE_GUID|EXTERN_GUID)\(\s*` + name + `\s*,` + list,
		`DEFINE_CODECAPI_GUID\(\s*` + name + `\s*,\s*"[^"]*"\s*,` + list,
		`GUID\s+` + name + `\s*=\s*\{` + list,
	}
	for _, p := range patterns {
		if m := regexp.MustCompile(p).FindStringSubmatch(h); m != nil {
			var v [11]uint64
			for i := range v {
				v[i], _ = strconv.ParseUint(m[i+1][2:], 16, 64)
			}
			var d4 [8]byte
			for i := range d4 {
				d4[i] = byte(v[3+i])
			}
			return newGUID(uint32(v[0]), uint16(v[1]), uint16(v[2]), d4), nil
		}
	}
	if m := regexp.MustCompile(`MIDL_INTERFACE\("([0-9A-Fa-f-]+)"\)\s*` + name + `\b`).FindStringSubmatch(h); m != nil {
		return parseGUIDString(m[1])
	}
	return GUID{}, fmt.Errorf("not in the SDK")
}

func parseGUIDString(s string) (GUID, error) {
	p := strings.Split(s, "-")
	if len(p) != 5 {
		return GUID{}, fmt.Errorf("bad GUID %q", s)
	}
	d1, _ := strconv.ParseUint(p[0], 16, 32)
	d2, _ := strconv.ParseUint(p[1], 16, 16)
	d3, _ := strconv.ParseUint(p[2], 16, 16)
	var d4 [8]byte
	tail := p[3] + p[4]
	for i := range d4 {
		b, _ := strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		d4[i] = byte(b)
	}
	return newGUID(uint32(d1), uint16(d2), uint16(d3), d4), nil
}

// sdkNumber evaluates a media subtype's code: FCC('xxxx'), a number, or a name the headers
// define as one.
func sdkNumber(h, expr string) (uint32, error) {
	if m := regexp.MustCompile(`^FCC\('(.)(.)(.)(.)'\)$`).FindStringSubmatch(expr); m != nil {
		return uint32(m[1][0]) | uint32(m[2][0])<<8 | uint32(m[3][0])<<16 | uint32(m[4][0])<<24, nil
	}
	if v, err := strconv.ParseUint(expr, 0, 32); err == nil {
		return uint32(v), nil
	}
	m := regexp.MustCompile(`(?:#define\s+` + expr + `\s+|\b` + expr + `\s*=\s*)(0x[0-9A-Fa-f]+|\d+)`).FindStringSubmatch(h)
	if m == nil {
		return 0, fmt.Errorf("cannot evaluate %q", expr)
	}
	v, err := strconv.ParseUint(m[1], 0, 32)
	return uint32(v), err
}
