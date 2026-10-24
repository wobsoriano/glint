# Drives the Trim & Compress window on the real desktop: scrub, play 10 s with sound, trim to
# 2-5 s at 720p Small, export, then start a full-size export and cancel it. Controls are found
# by name through UI Automation. Run it inside the desktop lock; verify.ps1 checks the results.
#
#   go run ./tools/desklock -- powershell -NoProfile -File drive.ps1 -Demo trimdemo.exe -Clip clip.mp4 -Out out
param([string]$Demo, [string]$Clip, [string]$Out)
$ErrorActionPreference = 'Stop'
. 'C:\Users\wobsoriano\Documents\dev\glint\promo\real\lib.ps1'
# lib.ps1 sets $Exe and other names to the Rust build's; launch only the demo, which never
# touches the user's Library.
if ((Split-Path $Demo -Leaf) -ne 'trimdemo.exe') { throw "refusing to launch $Demo" }
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$Scope = [System.Windows.Automation.TreeScope]

New-Item -ItemType Directory -Force "$Out\exports" | Out-Null
Get-ChildItem "$Out\exports" | Remove-Item
$proc = Start-Process -FilePath $Demo -ArgumentList @('-open', $Clip, '-savedir', "$Out\exports", '-loopback') -PassThru -RedirectStandardOutput "$Out\demo.log" -RedirectStandardError "$Out\demo.err"

function Note([string]$s) { "{0} {1}" -f (Get-Date -Format 'HH:mm:ss.fff'), $s | Tee-Object -FilePath "$Out\drive.log" -Append }

function Window {
    $c = New-Object System.Windows.Automation.PropertyCondition($A::ProcessIdProperty, $proc.Id)
    foreach ($e in $A::RootElement.FindAll($Scope::Children, $c)) { if ($e.Current.Name -like "Trim*") { return $e } }
}
# Find returns the first control whose name matches the wildcard pattern, or $null. A walk
# that meets a control the window just removed is retried.
function Find([string]$pattern) {
    for ($try = 0; $try -lt 5; $try++) {
        try {
            foreach ($e in $script:win.FindAll($Scope::Descendants, [System.Windows.Automation.Condition]::TrueCondition)) {
                if ($e.Current.Name -like $pattern) { return $e }
            }
            return $null
        }
        catch [System.Windows.Automation.ElementNotAvailableException] { Start-Sleep -m 10 }
        catch [System.Management.Automation.MethodInvocationException] { Start-Sleep -m 10 }
    }
    $null
}
function Wait-For([string]$pattern, [int]$ms = 10000) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt $ms) {
        $e = Find $pattern
        if ($e) { return $e }
        Start-Sleep -m 30
    }
    throw "no control named '$pattern' after $ms ms"
}
function Center($e) { $r = $e.Current.BoundingRectangle; @([int]($r.X + $r.Width / 2), [int]($r.Y + $r.Height / 2)) }
function Press-Named([string]$pattern) { $c = Center (Wait-For $pattern); Click $c[0] $c[1] 300; Note "clicked '$pattern'" }
Add-Type -Namespace TrimDemo -Name Win -MemberDefinition '[DllImport("user32.dll")] public static extern bool PrintWindow(System.IntPtr h, System.IntPtr dc, uint flags);'
# WindowShot saves what the window draws, whatever covers it: other workers' windows may be up.
function WindowShot([string]$name) {
    $r = $script:win.Current.BoundingRectangle
    $b = New-Object Drawing.Bitmap ([int]$r.Width), ([int]$r.Height)
    $g = [Drawing.Graphics]::FromImage($b); $dc = $g.GetHdc()
    $ok = [TrimDemo.Win]::PrintWindow([IntPtr]$script:win.Current.NativeWindowHandle, $dc, 2)
    $g.ReleaseHdc($dc); $g.Dispose()
    $b.Save("$Out\$name.png"); $b.Dispose()
    Note "shot $name.png (PrintWindow $ok)"
}

try {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while (-not ($script:win = Window)) {
        if ($sw.ElapsedMilliseconds -gt 10000) { throw "no window for pid $($proc.Id), exited $($proc.HasExited) code $($proc.ExitCode): $(Get-Content (Join-Path $Out 'demo.err'))" }
        Start-Sleep -m 50
    }
    [Promo.In]::SetForegroundWindow([IntPtr]$script:win.Current.NativeWindowHandle) | Out-Null
    $tl = (Wait-For 'Timeline').Current.BoundingRectangle
    Start-Sleep -m 2500
    WindowShot '1-ready'

    # The strip sits inside the timeline's 24 px padding; times map linearly across it.
    $x0 = $tl.X + 24; $w = $tl.Width - 48; $y = [int]($tl.Y + $tl.Height / 2)
    function X([double]$ms) { [int]($x0 + $w * $ms / 20000) }

    Drag (X 5000) $y (X 8000) $y 600
    Note 'scrubbed to 8 s'
    Start-Sleep -m 500
    WindowShot '2-scrubbed'

    Keys 'Space'
    Note 'play'
    Start-Sleep -m 5000
    WindowShot '3-playing'
    Start-Sleep -m 5300
    Keys 'Space'
    Note 'pause'
    Start-Sleep -m 1200

    Drag ($x0 - 6) $y (X 2000) $y 500
    Drag ($x0 + $w + 6) $y (X 5000) $y 500
    Note 'trimmed to 2-5 s'
    Press-Named '720p'
    Press-Named 'Small*'
    Start-Sleep -m 300
    WindowShot '4-settings'

    Press-Named 'Export'
    $seen = @()
    $shot = $false
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while (-not (Find 'Saved*')) {
        $e = Find 'Exporting*'
        if ($e -and $seen[-1] -ne $e.Current.Name) { $seen += $e.Current.Name }
        if ($e -and -not $shot) { WindowShot '5-exporting'; $shot = $true }
        if ($sw.ElapsedMilliseconds -gt 60000) { throw 'export did not finish' }
    }
    Note ("progress seen: " + ($seen -join ', '))
    Start-Sleep -m 300
    WindowShot '6-exported'

    Drag ((X 2000) - 6) $y ($x0 - 40) $y 400
    Drag ((X 5000) + 6) $y ($x0 + $w + 40) $y 400
    Press-Named 'Original'
    Press-Named 'High*'
    Press-Named 'Export'
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        $e = Find 'Exporting*'
        if ($e -and [int]($e.Current.Name -replace '\D', '') -ge 10) { break }
        if ($sw.ElapsedMilliseconds -gt 30000) { throw 'the second export never reached 10%' }
        Start-Sleep -m 20
    }
    WindowShot '7-cancelling'
    Note ("cancel at " + $e.Current.Name)
    Press-Named 'Cancel'
    Wait-For 'Export' | Out-Null
    Start-Sleep -m 300
    WindowShot '8-cancelled'
    Note ("exports folder: " + ((Get-ChildItem "$Out\exports" | ForEach-Object Name) -join ', '))
}
catch {
    Note "FAILED: $_"
    throw
}
finally {
    try { $script:win.GetCurrentPattern([System.Windows.Automation.WindowPattern]::Pattern).Close() } catch { Note "close: $_" }
    if (-not $proc.WaitForExit(5000)) { Stop-Process -Id $proc.Id }
}
