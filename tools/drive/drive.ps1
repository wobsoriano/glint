# Desktop driver helpers for verifying Glint on a real desktop. Dot-source this file, then use
# Glide, Click, DClick, Drag, Down, Up, Wheel, Keys, TypeText, Shot, Win, Place and Front.
# Coordinates are physical pixels of the primary display. Run every drive inside the desktop
# lock: go run ./tools/desklock -- powershell -NoProfile -File <your script>.ps1
# This file defines no other globals than $ScreenW, $ScreenH and $VK, so it cannot change a
# caller's variables.

if (-not ("Promo.In" -as [type])) {
Add-Type -Namespace Promo -Name In -MemberDefinition @'
[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
[DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
[DllImport("user32.dll")] public static extern bool GetCursorPos(out POINT p);
[DllImport("user32.dll")] public static extern void mouse_event(uint f, int dx, int dy, int data, UIntPtr extra);
[DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint f, UIntPtr extra);
[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern short VkKeyScanW(char c);
[DllImport("user32.dll")] public static extern IntPtr FindWindow(string c, string t);
[DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint f);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
[DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
[DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern uint SendInput(uint n, INPUT[] inputs, int size);
public struct POINT { public int X; public int Y; }
public struct RECT { public int L; public int T; public int R; public int B; }
[StructLayout(LayoutKind.Sequential)] public struct KEYBDINPUT { public ushort vk; public ushort scan; public uint flags; public uint time; public UIntPtr extra; }
[StructLayout(LayoutKind.Explicit)] public struct INPUT { [FieldOffset(0)] public uint type; [FieldOffset(8)] public KEYBDINPUT ki; [FieldOffset(8)] public long pad0; [FieldOffset(16)] public long pad1; [FieldOffset(24)] public long pad2; [FieldOffset(32)] public long pad3; }
'@
}
[Promo.In]::SetProcessDpiAwarenessContext([IntPtr]-4) | Out-Null
Add-Type -AssemblyName System.Drawing, System.Windows.Forms
$ScreenW = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Width
$ScreenH = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds.Height

function Pos { $p = New-Object Promo.In+POINT; [Promo.In]::GetCursorPos([ref]$p) | Out-Null; @($p.X, $p.Y) }

# Eased glide to (x, y). $ms sets the pace; human moves are 350-800 ms.
function Glide([int]$x, [int]$y, [int]$ms = 550) {
    $from = Pos; $steps = [math]::Max(8, [int]($ms / 8))
    $sw = [Diagnostics.Stopwatch]::StartNew()
    for ($i = 1; $i -le $steps; $i++) {
        $t = $i / $steps; $e = if ($t -lt .5) { 4 * $t * $t * $t } else { 1 - [math]::Pow(-2 * $t + 2, 3) / 2 }
        $cx = $from[0] + ($x - $from[0]) * $e; $cy = $from[1] + ($y - $from[1]) * $e
        [Promo.In]::mouse_event(0x8001, [int](($cx + 0.5) * 65535 / ($ScreenW - 1)), [int](($cy + 0.5) * 65535 / ($ScreenH - 1)), 0, [UIntPtr]::Zero)
        while ($sw.ElapsedMilliseconds -lt $ms * $t) { }
    }
}
function Down { [Promo.In]::mouse_event(0x2, 0, 0, 0, [UIntPtr]::Zero) }
function Up { [Promo.In]::mouse_event(0x4, 0, 0, 0, [UIntPtr]::Zero) }
function RDown { [Promo.In]::mouse_event(0x8, 0, 0, 0, [UIntPtr]::Zero) }
function RUp { [Promo.In]::mouse_event(0x10, 0, 0, 0, [UIntPtr]::Zero) }
function Wheel([int]$d) { [Promo.In]::mouse_event(0x800, 0, 0, $d, [UIntPtr]::Zero) }
function Click([int]$x, [int]$y, [int]$ms = 550) { Glide $x $y $ms; Start-Sleep -m 90; Down; Start-Sleep -m 70; Up; Start-Sleep -m 150 }
function DClick([int]$x, [int]$y, [int]$ms = 550) { Glide $x $y $ms; Start-Sleep -m 90; Down; Up; Start-Sleep -m 60; Down; Up; Start-Sleep -m 150 }
function Drag([int]$x1, [int]$y1, [int]$x2, [int]$y2, [int]$ms = 800) {
    Glide $x1 $y1; Start-Sleep -m 120; Down; Start-Sleep -m 90; Glide $x2 $y2 $ms; Start-Sleep -m 120; Up; Start-Sleep -m 200
}

$VK = @{ Ctrl = 0x11; Shift = 0x10; Alt = 0x12; Win = 0x5B; Enter = 0x0D; Esc = 0x1B; Tab = 0x09; Space = 0x20; Del = 0x2E; Back = 0x08; Left = 0x25; Up = 0x26; Right = 0x27; Down = 0x28 }
function VkOf([string]$k) { if ($VK.ContainsKey($k)) { $VK[$k] } else { [byte][char]$k.ToUpper() } }
# Chord like "Ctrl+Shift+4".
function Keys([string]$chord) {
    $ks = $chord -split '\+' | ForEach-Object { VkOf $_ }
    foreach ($k in $ks) { [Promo.In]::keybd_event($k, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 25 }
    [array]::Reverse($ks)
    foreach ($k in $ks) { [Promo.In]::keybd_event($k, 0, 2, [UIntPtr]::Zero); Start-Sleep -m 25 }
    Start-Sleep -m 120
}
# Types text with virtual-key events at a human pace. GPUI ignores VK_PACKET Unicode input.
function TypeText([string]$text, [int]$ms = 55) {
    foreach ($c in $text.ToCharArray()) {
        $scan = [Promo.In]::VkKeyScanW($c); $vk = [byte]($scan -band 0xFF); $shift = ($scan -band 0x100) -ne 0
        if ($shift) { [Promo.In]::keybd_event(0x10, 0, 0, [UIntPtr]::Zero) }
        [Promo.In]::keybd_event($vk, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 15
        [Promo.In]::keybd_event($vk, 0, 2, [UIntPtr]::Zero)
        if ($shift) { [Promo.In]::keybd_event(0x10, 0, 2, [UIntPtr]::Zero) }
        Start-Sleep -m ($ms + (Get-Random -Maximum 40))
    }
}

function Shot([string]$path) {
    Add-Type -AssemblyName System.Windows.Forms, System.Drawing
    $vs = [System.Windows.Forms.SystemInformation]::VirtualScreen
    $b = New-Object Drawing.Bitmap $vs.Width, $vs.Height
    $g = [Drawing.Graphics]::FromImage($b); $g.CopyFromScreen($vs.X, $vs.Y, 0, 0, $b.Size); $g.Dispose()
    $b.Save($path); $b.Dispose()
}

if (-not ("Promo.EW" -as [type])) {
Add-Type -Namespace Promo -Name EW -MemberDefinition @'
public delegate bool P(IntPtr h, IntPtr l);
[DllImport("user32.dll")] public static extern bool EnumWindows(P p, IntPtr l);
[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h, System.Text.StringBuilder s, int n);
[DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
public static IntPtr Find(string prefix) {
    IntPtr found = IntPtr.Zero;
    EnumWindows((h, l) => {
        if (!IsWindowVisible(h)) return true;
        var t = new System.Text.StringBuilder(256); GetWindowText(h, t, 256);
        if (t.ToString().StartsWith(prefix)) { found = h; return false; }
        return true;
    }, IntPtr.Zero);
    return found;
}
'@
}
# The first visible top-level window whose title starts with $title.
function Win([string]$title) { [Promo.EW]::Find($title) }
function Place([string]$title, [int]$x, [int]$y, [int]$w, [int]$h) {
    $h0 = Win $title
    if ($h0 -ne [IntPtr]::Zero) { [Promo.In]::ShowWindow($h0, 9) | Out-Null; [Promo.In]::SetWindowPos($h0, [IntPtr]::Zero, $x, $y, $w, $h, 0x40) | Out-Null }
    $h0
}
function Front([string]$title) { $h0 = Win $title; if ($h0 -ne [IntPtr]::Zero) { [Promo.In]::SetForegroundWindow($h0) | Out-Null } }
