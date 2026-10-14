//! Dumps one decoded frame of an MP4 to a PNG: `frame <video.mp4> <t_ms> <out.png>`.

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [video, t_ms, out] = args.as_slice() else { anyhow::bail!("usage: frame <video.mp4> <t_ms> <out.png>") };
    let mut reader = glint_media::VideoReader::open(std::path::Path::new(video))?;
    reader.seek(t_ms.parse()?)?;
    let frame = reader.next_frame()?.ok_or_else(|| anyhow::anyhow!("no frame at {t_ms} ms"))?;
    frame.image.save(out)?;
    println!("{}x{} frame at {} ms -> {out}", frame.image.width(), frame.image.height(), frame.t_ms);
    Ok(())
}
