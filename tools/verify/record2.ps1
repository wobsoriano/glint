# Follow-ups to record.ps1: the control bar's delete confirmed with its Delete button, a window
# recording chosen by a click and started with Enter, and the microphone chip's panel.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$recBase = @{ target = 'Display'; area = $null; camera = $null; mic = $null; system_audio = $false; countdown = 0; teleprompter = $false; script = ''; prompter_lines = 3 }
function Rec([hashtable]$over) { $r = $recBase.Clone(); foreach ($k in $over.Keys) { $r[$k] = $over[$k] }; $r }
$afterRec = @{ preview = $true; copy = $false; save = $false; studio = $false }
function Recordings([string]$d) { @(Get-ChildItem "$d\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue | Sort-Object Name) }
function Picker { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint recording"') }
function Open-Picker { Keys 'Ctrl+Shift+7'; Wait-Until { Picker } 3000 | Out-Null; Start-Sleep -Milliseconds 500 }
function Describe([string]$rec) {
    $m = Meta $rec
    "  $(Split-Path -Leaf $rec): $($m.width)x$($m.height), $($m.duration_ms) ms, source '$($m.source)'; files: $((Get-ChildItem $rec).Name -join ', ')"
}
$g = $null
$stage = Start-Stage
try {
    Step 'delete, confirmed with the Delete button, leaves nothing behind'
    $dir = New-Run 'x-record-delete' @{ after_recording = $afterRec; record = (Rec @{}) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    Click 1070 980 200
    Start-Sleep -Milliseconds 700
    Click 1067 980 200
    Start-Sleep -Milliseconds 2500
    Shot "$dir\01-deleted.png"
    "recordings: $((Recordings $dir).Count); folders in the Library: $(@(Get-ChildItem "$dir\data\Library" -Directory -Force -ErrorAction SilentlyContinue).Name -join ', ')"
    $bar = [bool](& $Check wins -pid $g.Id | Select-String -SimpleMatch 'visible=true affinity=0x0 exstyle=0x8200088 rect=(810,956')
    "control bar still showing: $bar"

    Step 'Window target: Enter, click the stage, Enter records it'
    Stop-Glint $g
    $dir = New-Run 'x-record-window' @{ after_recording = $afterRec; record = (Rec @{ target = 'Window' }) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 900
    Glide 900 420 300
    Start-Sleep -Milliseconds 400
    Shot "$dir\01-hover-window.png"
    Click 900 420 150
    Start-Sleep -Milliseconds 900
    Shot "$dir\02-chosen.png"
    if (-not (Get-ChildItem "$dir\data\Library" -Directory -Force -ErrorAction SilentlyContinue)) { Keys 'Enter' }
    Start-Sleep -Milliseconds 2500
    Shot "$dir\03-recording.png"
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 800
    if ((Recordings $dir).Count) { Describe (Recordings $dir)[-1].FullName } else { 'no recording' }

    Step 'the microphone chip opens its device list'
    Open-Picker
    Click 963 976 200
    Start-Sleep -Milliseconds 800
    Shot "$dir\04-mic-panel.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300
    Keys 'Esc'
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
