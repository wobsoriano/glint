//! Times the Media Foundation H.264 sink writer on the same NV12 frames with and without
//! `CODECAPI_AVEncMPVDefaultBPictureCount = 0` handed to `SetInputMediaType`, the one encoder
//! setting the Go writer in `internal/media` adds over `Mp4Sink`. Runs alternate between the
//! two so GPU clock changes hit both.
//!
//! cargo run --release -p glint-media --example bframes -- <out_dir> [width height fps frames runs]

#[cfg(not(windows))]
fn main() {}

#[cfg(windows)]
fn main() -> anyhow::Result<()> {
    win::main()
}

#[cfg(windows)]
mod win {
    use std::path::Path;
    use std::time::Instant;
    use windows::core::{Interface, GUID, HSTRING, PCWSTR};
    use windows::Win32::Media::MediaFoundation::*;
    use windows::Win32::System::Com::{CoInitializeEx, COINIT_MULTITHREADED};

    fn pack2(a: u32, b: u32) -> u64 {
        (a as u64) << 32 | b as u64
    }

    pub fn main() -> anyhow::Result<()> {
        let args: Vec<String> = std::env::args().skip(1).collect();
        let out = Path::new(&args[0]);
        let num = |i: usize, d: u32| args.get(i).and_then(|v| v.parse().ok()).unwrap_or(d);
        let (w, h, fps, frames, runs) = (num(1, 1212), num(2, 720), num(3, 60), num(4, 510), num(5, 5));
        std::fs::create_dir_all(out)?;
        unsafe {
            CoInitializeEx(None, COINIT_MULTITHREADED).ok()?;
            MFStartup(MF_VERSION, MFSTARTUP_NOSOCKET)?;
        }
        let pictures = frames_nv12(w as usize, h as usize, 8);
        let mut results: [Vec<f64>; 2] = [Vec::new(), Vec::new()];
        for run in 0..runs {
            for (k, bframes_off) in [false, true].into_iter().enumerate() {
                let path = out.join(format!("bframes-{k}.mp4"));
                let started = Instant::now();
                let hardware = encode(&path, w, h, fps, frames, &pictures, bframes_off)?;
                let rate = frames as f64 / started.elapsed().as_secs_f64();
                println!("run {run}: B-frame count {}: {rate:.1} frames/s, hardware {hardware}", if bframes_off { "0" } else { "default" });
                results[k].push(rate);
            }
        }
        for (k, name) in ["encoder default", "B-frame count 0"].iter().enumerate() {
            let mut r = results[k].clone();
            r.sort_by(f64::total_cmp);
            println!("{name}: median {:.1} frames/s, range {:.1} to {:.1} over {} runs", r[r.len() / 2], r[0], r[r.len() - 1], r.len());
        }
        Ok(())
    }

    /// n frames of a gradient that moves and a block that sweeps, so every frame differs.
    fn frames_nv12(w: usize, h: usize, n: usize) -> Vec<Vec<u8>> {
        (0..n)
            .map(|i| {
                let mut f = vec![0u8; w * h * 3 / 2];
                for y in 0..h {
                    for x in 0..w {
                        let block = x / 64 == (i * 7 + y / 64) % (w / 64);
                        f[y * w + x] = if block { 235 } else { (16 + (x + y + i * 13) % 200) as u8 };
                    }
                }
                for (j, v) in f[w * h..].iter_mut().enumerate() {
                    *v = (96 + (j / 2 + i * 5) % 64) as u8;
                }
                f
            })
            .collect()
    }

    fn encode(path: &Path, w: u32, h: u32, fps: u32, frames: u32, pictures: &[Vec<u8>], bframes_off: bool) -> anyhow::Result<bool> {
        unsafe {
            let mut attrs = None;
            MFCreateAttributes(&mut attrs, 2)?;
            let attrs = attrs.unwrap();
            attrs.SetUINT32(&MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS, 1)?;
            attrs.SetGUID(&MF_TRANSCODE_CONTAINERTYPE, &MFTranscodeContainerType_MPEG4)?;
            let url = HSTRING::from(path.as_os_str());
            let writer = MFCreateSinkWriterFromURL(PCWSTR(url.as_ptr()), None, &attrs)?;

            let out = MFCreateMediaType()?;
            out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            out.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_H264)?;
            out.SetUINT32(&MF_MT_AVG_BITRATE, (w as f64 * h as f64 * fps as f64 * 0.1) as u32)?;
            out.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
            out.SetUINT64(&MF_MT_FRAME_SIZE, pack2(w, h))?;
            out.SetUINT64(&MF_MT_FRAME_RATE, pack2(fps, 1))?;
            out.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack2(1, 1))?;
            out.SetUINT32(&MF_MT_MPEG2_PROFILE, eAVEncH264VProfile_High.0 as u32)?;
            out.SetUINT32(&MF_MT_YUV_MATRIX, MFVideoTransferMatrix_BT709.0 as u32)?;
            out.SetUINT32(&MF_MT_VIDEO_NOMINAL_RANGE, MFNominalRange_16_235.0 as u32)?;
            out.SetUINT32(&MF_MT_VIDEO_PRIMARIES, MFVideoPrimaries_BT709.0 as u32)?;
            out.SetUINT32(&MF_MT_TRANSFER_FUNCTION, MFVideoTransFunc_709.0 as u32)?;
            let stream = writer.AddStream(&out)?;

            let input = MFCreateMediaType()?;
            input.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            input.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_NV12)?;
            input.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
            input.SetUINT64(&MF_MT_FRAME_SIZE, pack2(w, h))?;
            input.SetUINT64(&MF_MT_FRAME_RATE, pack2(fps, 1))?;
            input.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack2(1, 1))?;
            input.SetUINT32(&MF_MT_ALL_SAMPLES_INDEPENDENT, 1)?;
            input.SetUINT32(&MF_MT_DEFAULT_STRIDE, w)?;
            if bframes_off {
                let mut params = None;
                MFCreateAttributes(&mut params, 1)?;
                let params = params.unwrap();
                params.SetUINT32(&CODECAPI_AVEncMPVDefaultBPictureCount, 0)?;
                writer.SetInputMediaType(stream, &input, &params)?;
            } else {
                writer.SetInputMediaType(stream, &input, None)?;
            }
            let mut hardware = false;
            let mut raw = std::ptr::null_mut();
            if writer.GetServiceForStream(stream, &GUID::zeroed(), &IMFTransform::IID, &mut raw).is_ok() && !raw.is_null() {
                if let Ok(enc) = IMFTransform::from_raw(raw).GetAttributes() {
                    hardware = enc.GetStringLength(&MFT_ENUM_HARDWARE_URL_Attribute).is_ok();
                }
            }
            writer.BeginWriting()?;
            let len = (w * h * 3 / 2) as usize;
            let dur = 10_000_000 / fps as i64;
            for i in 0..frames as usize {
                let buffer = MFCreateMemoryBuffer(len as u32)?;
                let mut ptr = std::ptr::null_mut();
                buffer.Lock(&mut ptr, None, None)?;
                std::ptr::copy_nonoverlapping(pictures[i % pictures.len()].as_ptr(), ptr, len);
                buffer.Unlock()?;
                buffer.SetCurrentLength(len as u32)?;
                let sample = MFCreateSample()?;
                sample.AddBuffer(&buffer)?;
                sample.SetSampleTime(i as i64 * 10_000_000 / fps as i64)?;
                sample.SetSampleDuration(dur)?;
                writer.WriteSample(stream, &sample)?;
            }
            writer.Finalize()?;
            Ok(hardware)
        }
    }
}
