use anyhow::{bail, Context as _, Result};
use std::path::PathBuf;

pub fn recycle(paths: &[PathBuf]) -> Result<()> {
    use windows::Win32::System::Com::{
        CoCreateInstance, CoInitializeEx, CoUninitialize, CLSCTX_ALL, COINIT_APARTMENTTHREADED,
    };
    use windows::Win32::UI::Shell::{
        FileOperation, IFileOperation, IShellItem, SHCreateItemFromParsingName, FOFX_RECYCLEONDELETE,
        FOF_ALLOWUNDO, FOF_NOCONFIRMATION, FOF_NOERRORUI, FOF_SILENT,
    };
    if paths.is_empty() {
        return Ok(());
    }
    unsafe {
        let inited = CoInitializeEx(None, COINIT_APARTMENTTHREADED).is_ok();
        let result = (|| -> Result<()> {
            let op: IFileOperation = CoCreateInstance(&FileOperation, None, CLSCTX_ALL).context("creating IFileOperation")?;
            op.SetOperationFlags(
                FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_SILENT | FOF_NOERRORUI | FOFX_RECYCLEONDELETE,
            )?;
            for p in paths {
                let item: IShellItem = SHCreateItemFromParsingName(&windows::core::HSTRING::from(p.as_os_str()), None)
                    .with_context(|| format!("locating {}", p.display()))?;
                op.DeleteItem(&item, None)?;
            }
            op.PerformOperations().context("moving to the Recycle Bin")?;
            if op.GetAnyOperationsAborted()?.as_bool() {
                bail!("moving to the Recycle Bin was cancelled");
            }
            Ok(())
        })();
        if inited {
            CoUninitialize();
        }
        result
    }
}
