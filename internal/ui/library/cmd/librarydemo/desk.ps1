# Helpers for driving one demo window on the real desktop, dot-sourced by drive.ps1 here and
# in internal/ui/prefs/cmd/prefsdemo. Run the caller inside the desktop lock. Coordinates are
# client DIPs at 100% scale. lib.ps1 sets $Data, $Exe and $Root of its own, so callers name
# none of those.
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

# Only the window of the demo Start-Demo started: other workers may have Glint windows open.
$script:Proc = $null
$script:Title = ""
function Hwnd { if ($script:Proc) { [LP.W]::Find([uint32]$script:Proc.Id, $script:Title) } else { [IntPtr]::Zero } }
function Origin { $p = New-Object LP.W+POINT; [LP.W]::ClientToScreen((Hwnd), [ref]$p) | Out-Null; $p }
function At([int]$x, [int]$y) { $o = Origin; Click ($o.X + $x) ($o.Y + $y) 250 }
function DAt([int]$x, [int]$y) { $o = Origin; DClick ($o.X + $x) ($o.Y + $y) 250 }
function DragAt([int]$x1, [int]$y1, [int]$x2, [int]$y2) { $o = Origin; Drag ($o.X + $x1) ($o.Y + $y1) ($o.X + $x2) ($o.Y + $y2) 600 }
function Hold([string]$k, [scriptblock]$f) {
    $vk = @{ Ctrl = 0x11; Shift = 0x10 }[$k]
    [Promo.In]::keybd_event($vk, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 60
    & $f
    [Promo.In]::keybd_event($vk, 0, 2, [UIntPtr]::Zero); Start-Sleep -m 60
}
function Snap([string]$dir, [string]$name) { Start-Sleep -m 500; Shot "$dir\$name.png"; "shot $name" }
# Windows lets a background process take the foreground after a key press of its own.
function Front {
    [Promo.In]::keybd_event(0x12, 0, 0, [UIntPtr]::Zero); [Promo.In]::keybd_event(0x12, 0, 2, [UIntPtr]::Zero)
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::new(-1), 0, 0, 0, 0, 0x43) | Out-Null
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::new(-2), 0, 0, 0, 0, 0x43) | Out-Null
    [LP.W]::SetForegroundWindow((Hwnd)) | Out-Null
    Start-Sleep -m 300
}
function Start-Demo([string]$exe, [string]$title, [string]$dataDir, [int]$w, [int]$h, [string[]]$demoArgs = @()) {
    $env:GLINT_DATA_DIR = $dataDir
    $script:Title = $title
    $err = "$exe.err"
    if ($demoArgs.Count -gt 0) {
        $script:Proc = Start-Process $exe -ArgumentList $demoArgs -PassThru -NoNewWindow -RedirectStandardError $err
    } else {
        $script:Proc = Start-Process $exe -PassThru -NoNewWindow -RedirectStandardError $err
    }
    for ($i = 0; $i -lt 50 -and (Hwnd) -eq [IntPtr]::Zero; $i++) { Start-Sleep -m 100 }
    if ((Hwnd) -eq [IntPtr]::Zero) {
        "no window; exited $($script:Proc.HasExited): $(Get-Content -Raw $err)"
        Stop-Process -Id $script:Proc.Id -Force -ErrorAction SilentlyContinue
        exit 1
    }
    [LP.W]::SetWindowPos((Hwnd), [IntPtr]::Zero, 100, 60, $w, $h, 0x40) | Out-Null
    Front
    Start-Sleep -m 1200
}
function Stop-Demo {
    [LP.W]::PostMessageW((Hwnd), 0x10, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null
    if (-not $script:Proc.WaitForExit(3000)) { Stop-Process -Id $script:Proc.Id -Force }
    "closed"
}
