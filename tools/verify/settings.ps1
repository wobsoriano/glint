# The Settings window pane by pane, and a shortcut that another process holds: rebinding the
# area capture to it must say why it failed and keep the old shortcut working. Settings is
# placed at 80,60 860x640 at 100%, as scenarios/prefs.ps1 places it.
param([int]$IconX = 1642, [int]$IconY = 955)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-settings' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false } }
$holder = Start-Process powershell -ArgumentList '-NoProfile', '-File', "`"$PSScriptRoot\holdkey.ps1`"", '-Status', "`"$dir\holder.txt`"" -PassThru -WindowStyle Hidden
if (-not (Wait-Until { (Get-Content "$dir\holder.txt" -ErrorAction SilentlyContinue) -eq 'held: True' } 8000)) { throw 'the other process could not take Ctrl+Alt+J' }
$g = Start-Glint $dir
function Saved { Get-Content "$dir\data\settings.json" | ConvertFrom-Json }
try {
    Choose 11
    $ok = Wait-Until { (Win 'Glint Settings') -ne [IntPtr]::Zero } 5000
    if (-not $ok) { throw 'Settings did not open' }
    Start-Sleep -Milliseconds 800
    Place 'Glint Settings' 80 60 860 640 | Out-Null
    Start-Sleep -Milliseconds 600

    Step 'each pane'
    $i = 0
    foreach ($y in 122, 158, 194, 230, 266, 302, 338, 374) {
        Click 155 $y 150
        Start-Sleep -Milliseconds 400
        Check shot -out ("$dir\01-pane-{0}.png" -f $i) -rect '80,60,860,640' | Out-Null
        Glide 600 400 100
        Wheel -600
        Start-Sleep -Milliseconds 300
        Check shot -out ("$dir\01-pane-{0}b.png" -f $i) -rect '80,60,860,640' | Out-Null
        $i++
    }

    Step 'rebind the area shortcut to Ctrl+Alt+J, which another process holds'
    Click 160 266 200
    Start-Sleep -Milliseconds 300
    Click 851 200 200
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+Alt+J'
    Start-Sleep -Milliseconds 800
    Check shot -out "$dir\02-rebind-held.png" -rect '80,60,860,640' | Out-Null
    "settings.json hotkeys.area: $((Saved).hotkeys.area)"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300

    Step 'rebind it to Ctrl+Shift+3, which full screen uses'
    Click 851 200 200
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+Shift+3'
    Start-Sleep -Milliseconds 800
    Check shot -out "$dir\03-rebind-conflict.png" -rect '80,60,860,640' | Out-Null
    "settings.json hotkeys.area: $((Saved).hotkeys.area)"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300

    Step 'rebind it to F12, which Windows keeps for debuggers'
    Click 851 200 200
    Start-Sleep -Milliseconds 300
    [Promo.In]::keybd_event(0x7B, 0, 0, [UIntPtr]::Zero); Start-Sleep -Milliseconds 40; [Promo.In]::keybd_event(0x7B, 0, 2, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 800
    Check shot -out "$dir\04-rebind-f12.png" -rect '80,60,860,640' | Out-Null
    "settings.json hotkeys.area: $((Saved).hotkeys.area)"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300
    Close-Win 'Glint Settings'
    Start-Sleep -Milliseconds 500
    Step 'the old shortcut still captures'
    $n = (Items $dir).Count
    Capture-Area 500 400 800 600
    "items $n -> $((Items $dir).Count)"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $holder.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
