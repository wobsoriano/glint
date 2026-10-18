fn main() {
    // GPUI loads icon resource 1 from the exe for window and taskbar icons.
    println!("cargo:rerun-if-changed=assets/glint.rc");
    println!("cargo:rerun-if-changed=assets/glint.ico");
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("windows") {
        embed_resource::compile("assets/glint.rc", embed_resource::NONE).manifest_optional().unwrap();
    }
}
