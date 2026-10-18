use crate::writer::VideoCodec;

/// VideoToolbox encodes H.264 and HEVC on every Mac that runs macOS 13.
pub fn available_codecs() -> Vec<VideoCodec> {
    vec![VideoCodec::H264, VideoCodec::Hevc]
}

pub fn decodable_codecs() -> Vec<VideoCodec> {
    vec![VideoCodec::H264, VideoCodec::Hevc]
}
