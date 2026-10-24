# Drives librarydemo on the real desktop, one step per run, inside the desktop lock:
#   go run ./tools/desklock -- powershell -NoProfile -File internal/ui/library/cmd/librarydemo/drive.ps1 -Step open -Copy <copy of the data dir> -Out <dir>
# Coordinates are client DIPs of the 1180x760 window at 100% scale.
param([string]$Step, [string]$Copy, [string]$Out, [string]$Arg = "")
# lib.ps1 sets $Data, $Exe and $Root of its own, so this script names none of those.
. "C:\Users\wobsoriano\Documents\dev\glint\promo\real\lib.ps1"

if (-not ("LP.W" -as [type])) {
Add-Type -Namespace LP -Name W -MemberDefinition @'
public delegate bool EnumProc(IntPtr h, IntPtr l);
[DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc p, IntPtr l);
[DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
[DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, System.Text.StringBuilder s, int n);
public static IntPtr Find(uint pid, string title) {
    IntPtr found = IntPtr.Zero;
    EnumWindows((h, l) => {
        uint p; GetWindowThreadProcessId(h, out p);
        var t = new System.Text.StringBuilder(256); GetWindowTextW(h, t, 256);
        if (p == pid && IsWindowVisible(h) && t.ToString() == title) { found = h; return false; }
        return true;
    }, IntPtr.Zero);
    return found;
}
[DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint f);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
[DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
[DllImport("user32.dll")] public static extern bool PostMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);
public struct POINT { public int X; public int Y; }
'@
}

$Title = "Glint Library"
# Only the demo this script started: other workers may have Glint windows open.
$script:Proc = $null
function Hwnd { if ($script:Proc) { [LP.W]::Find([uint32]$script:Proc.Id, $Title) } else { [IntPtr]::Zero } }
function Origin { $p = New-Object LP.W+POINT; [LP.W]::ClientToScreen((Hwnd), [ref]$p) | Out-Null; $p }
function At([int]$x, [int]$y) { $o = Origin; Click ($o.X + $x) ($o.Y + $y) 250 }
function DAt([int]$x, [int]$y) { $o = Origin; DClick ($o.X + $x) ($o.Y + $y) 250 }
function Hold([string]$k, [scriptblock]$f) {
    $vk = @{ Ctrl = 0x11; Shift = 0x10 }[$k]
    [Promo.In]::keybd_event($vk, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 60
    & $f
    [Promo.In]::keybd_event($vk, 0, 2, [UIntPtr]::Zero); Start-Sleep -m 60
}
function Snap([string]$name) { Start-Sleep -m 500; Shot "$Out\$name.png"; "shot $name" }
# Windows lets a background process take the foreground after a key press of its own.
function Front {
    [Promo.In]::keybd_event(0x12, 0, 0, [UIntPtr]::Zero); [Promo.In]::keybd_event(0x12, 0, 2, [UIntPtr]::Zero)
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::new(-1), 0, 0, 0, 0, 0x43) | Out-Null
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::new(-2), 0, 0, 0, 0, 0x43) | Out-Null
    [LP.W]::SetForegroundWindow((Hwnd)) | Out-Null
    Start-Sleep -m 300
}
function OpenLib {
    $env:GLINT_DATA_DIR = $Copy
    $script:Proc = Start-Process "$Out\librarydemo.exe" -PassThru -RedirectStandardError "$Out\librarydemo.err"
    for ($i = 0; $i -lt 50 -and (Hwnd) -eq [IntPtr]::Zero; $i++) { Start-Sleep -m 100 }
    if ((Hwnd) -eq [IntPtr]::Zero) {
        "no window; exited $($script:Proc.HasExited): $(Get-Content -Raw "$Out\librarydemo.err")"
        Stop-Process -Id $script:Proc.Id -Force -ErrorAction SilentlyContinue
        exit 1
    }
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::Zero, 100, 60, 1180, 760, 0x40) | Out-Null
    Front
    Start-Sleep -m 1200
}
function CloseLib {
    [LP.W]::PostMessageW((Hwnd), 0x10, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null
    if (-not $script:Proc.WaitForExit(3000)) { Stop-Process -Id $script:Proc.Id -Force }
    "closed, exit $($script:Proc.ExitCode)"
}

OpenLib
switch ($Step) {
    "look" { Snap "lib-open" }
    "prefs" {
        At 786 24; At 960 24; At 80 130
        At 322 24; TypeText "22.36" 40
        Snap "lib-prefs"
        Keys "Esc"
        CloseLib
        OpenLib
        Snap "lib-prefs-reopened"
    }
    "select" {
        At 322 124
        Hold Ctrl { At 547 124 }
        Hold Ctrl { At 322 452 }
        Snap "lib-select"
        Keys "Ctrl+C"
        Start-Sleep -m 300
        Add-Type -AssemblyName System.Windows.Forms
        "clipboard: " + (([System.Windows.Forms.Clipboard]::GetFileDropList()) -join " | ")
    }
    "quicklook" {
        DAt 772 124
        Keys "Space"; Keys "Space"
        Snap "lib-quicklook"
    }
    "rename" {
        At 547 124
        Keys "Enter"; Start-Sleep -m 200
        Keys "Ctrl+A"; TypeText "Renamed by test" 30; Keys "Enter"
        Snap "lib-rename"
    }
    "trash" {
        At 772 452
        Keys "Del"
        Snap "lib-trash-ask"
        Keys "Enter"
        Snap "lib-trash-done"
    }
    "live" {
        Snap "lib-live-before"
        $staged = "$Out\2026-10-23_21-00-00_beef"
        Remove-Item -Recurse -Force $staged -ErrorAction SilentlyContinue
        Copy-Item -Recurse $Arg $staged
        $meta = Get-Content -Raw "$staged\meta.json" | ConvertFrom-Json
        $meta.title = "Live added"
        [IO.File]::WriteAllText("$staged\meta.json", ($meta | ConvertTo-Json))
        Move-Item $staged "$Copy\Library\"
        Start-Sleep -m 800
        Snap "lib-live-after"
    }
}
CloseLib
