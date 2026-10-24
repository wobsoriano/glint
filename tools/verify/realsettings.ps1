# The Rust-written settings.json as the Go build reads it: every Settings pane on a copy of
# it, then Ctrl+Shift+3 to show that the after-capture steps it names (preview and copy) run,
# and whether opening Settings without a change rewrote the file.
# Settings is placed at 80,60 860x640 at 100%.
param([Parameter(Mandatory)] [string] $Data, [int]$IconX = 1642, [int]$IconY = 955)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = Join-Path $env:GLINT_OUT 'x-realsettings'
New-Item -ItemType Directory -Force $dir | Out-Null
$before = (Get-FileHash "$Data\settings.json").Hash
$env:GLINT_DATA_DIR = $Data
$g = Start-Process "$Bin\glint.exe" -ArgumentList '--background' -PassThru -NoNewWindow -RedirectStandardError "$dir\glint.err"
Start-Sleep -Milliseconds 1500
try {
    Choose 11
    if (-not (Wait-Until { (Win 'Glint Settings') -ne [IntPtr]::Zero } 5000)) { throw 'Settings did not open' }
    Start-Sleep -Milliseconds 800
    Place 'Glint Settings' 80 60 860 640 | Out-Null
    Start-Sleep -Milliseconds 500
    $i = 0
    foreach ($y in 122, 158, 194, 230, 266, 302) {
        Click 155 $y 150
        Start-Sleep -Milliseconds 400
        Check shot -out ("$dir\01-pane-{0}.png" -f $i) -rect '80,60,860,640' | Out-Null
        $i++
    }
    Close-Win 'Glint Settings'
    Start-Sleep -Milliseconds 600
    "settings.json rewritten by opening Settings: $((Get-FileHash "$Data\settings.json").Hash -ne $before)"
    Step 'Ctrl+Shift+3 runs the after-capture steps the file names: a card and a copy'
    Set-Clipboard -Value 'glintcheck: before'
    $n = (Get-ChildItem "$Data\Library" -Directory).Count
    Keys 'Ctrl+Shift+3'
    Start-Sleep -Milliseconds 2500
    "captures $n -> $((Get-ChildItem "$Data\Library" -Directory).Count); card: $(if (Stack $g) { 'shown' } else { 'none' })"
    Check clip
    Shot "$dir\02-after-capture.png"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
}
