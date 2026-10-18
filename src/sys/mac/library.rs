use objc2_foundation::{NSFileManager, NSString, NSURL};
use std::path::PathBuf;

/// Moves `paths` to the Trash, where Finder's Put Back can restore them.
pub fn recycle(paths: &[PathBuf]) -> anyhow::Result<()> {
    let files = NSFileManager::defaultManager();
    for p in paths {
        let url = NSURL::fileURLWithPath(&NSString::from_str(&p.to_string_lossy()));
        files
            .trashItemAtURL_resultingItemURL_error(&url, None)
            .map_err(|e| anyhow::anyhow!("moving {} to the Trash: {}", p.display(), e.localizedDescription()))?;
    }
    Ok(())
}
