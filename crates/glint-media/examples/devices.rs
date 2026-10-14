//! Prints every display, microphone and camera the engine can see, and the codec support.

fn main() -> anyhow::Result<()> {
    for d in glint_media::displays()? {
        println!("display {} {:?} rect={:?} scale={} hmonitor={:#x} primary={}", d.index, d.name, d.rect, d.scale, d.hmonitor, d.primary);
    }
    for m in glint_media::microphones()? {
        println!("mic {:?} id={}", m.name, m.id);
    }
    let cams = glint_media::cameras()?;
    println!("cameras: {}", cams.len());
    for c in cams {
        println!("camera {:?} id={}", c.name, c.id);
    }
    println!("encode: {:?}", glint_media::available_codecs());
    println!("decode: {:?}", glint_media::decodable_codecs());
    Ok(())
}
