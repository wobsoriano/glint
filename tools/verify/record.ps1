# Recording beyond scenarios/record.ps1: the countdown cancelled with Esc and with Cancel, the
# control bar's pause, restart and delete, a remembered camera and microphone that are missing,
# an area recording with the teleprompter, and a window recording. Coordinates are the picker
# and control bar at the bottom centre of a 1920x1080 display at 100%.
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
$stage = $null
try {
    Step 'countdown 3 s: Esc during the countdown records nothing'
    $dir = New-Run 'x-record-countdown' @{ after_recording = $afterRec; record = (Rec @{ countdown = 3 }) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1000
    Shot "$dir\01-countdown.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 3500
    Shot "$dir\02-after-esc.png"
    "recordings after Esc: $((Recordings $dir).Count); picker still open: $(Picker)"
    Step 'and the Cancel button does the same'
    if (-not (Picker)) { Open-Picker }
    Keys 'Enter'
    Start-Sleep -Milliseconds 900
    Shot "$dir\03-countdown-cancel-button.png"
    Click 1281 976 200
    Start-Sleep -Milliseconds 3500
    "recordings after Cancel: $((Recordings $dir).Count); picker still open: $(Picker)"
    if (Picker) { Keys 'Esc'; Start-Sleep -Milliseconds 400 }
    "picker closed by Esc: $(-not (Picker))"
    Stop-Glint $g

    Step 'pause excludes time: 1.5 s, pause 2.5 s, resume 1.5 s, stop'
    $dir = New-Run 'x-record-bar' @{ after_recording = $afterRec; record = (Rec @{}) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    Click 962 980 200
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-paused.png"
    Start-Sleep -Milliseconds 2000
    Click 962 980 200
    Start-Sleep -Milliseconds 1500
    Click 1034 980 200
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 800
    Describe (Recordings $dir)[-1].FullName
    Step 'restart: 2 s, restart, 1 s, stop gives one short recording'
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 2000
    Click 997 980 200
    Start-Sleep -Milliseconds 600
    Shot "$dir\02-restarted.png"
    Start-Sleep -Milliseconds 1000
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 2 } 15000 | Out-Null
    Start-Sleep -Milliseconds 800
    "recordings: $((Recordings $dir).Count)"
    Describe (Recordings $dir)[-1].FullName
    Step 'delete throws the recording away'
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    Click 1070 980 200
    Start-Sleep -Milliseconds 800
    Shot "$dir\03-delete.png"
    Check wins -pid $g.Id
    Start-Sleep -Milliseconds 2500
    "recordings after delete: $((Recordings $dir).Count); staging folders: $(@(Get-ChildItem "$dir\data\Library" -Directory -Force -Filter '.*').Name -join ', ')"
    Stop-Glint $g

    Step 'a remembered camera and microphone that are missing warn, and the recording goes on'
    $dir = New-Run 'x-record-missing' @{ after_recording = $afterRec; record = (Rec @{ camera = 'glint-missing-camera'; mic = 'glint-missing-mic' }) }
    $g = Start-Glint $dir
    Open-Picker
    Shot "$dir\01-picker.png"
    Keys 'Enter'
    Start-Sleep -Milliseconds 1200
    Shot "$dir\02-recording.png"
    Start-Sleep -Milliseconds 1500
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 1200
    Shot "$dir\03-finished.png"
    Describe (Recordings $dir)[-1].FullName
    Stop-Glint $g

    Step 'an area recording with the teleprompter'
    $script = "First line of the script`nSecond line keeps going`nThird line ends it`nFourth line"
    $dir = New-Run 'x-record-area' @{ after_recording = $afterRec; record = (Rec @{ target = 'Area'; area = @(300, 200, 800, 500); teleprompter = $true; script = $script }) }
    $g = Start-Glint $dir
    Open-Picker
    Shot "$dir\01-picker.png"
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\02-area-recording.png"
    Start-Sleep -Milliseconds 1500
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 1000
    Shot "$dir\03-finished.png"
    if ((Recordings $dir).Count) { Describe (Recordings $dir)[-1].FullName } else { 'no recording' }
    Stop-Glint $g

    Step 'a window recording of the stage'
    $stage = Start-Stage
    $dir = New-Run 'x-record-window' @{ after_recording = $afterRec; record = (Rec @{ target = 'Window' }) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1000
    Shot "$dir\01-choose-window.png"
    Click 900 420 250
    Start-Sleep -Milliseconds 1500
    Shot "$dir\02-window-recording.png"
    Start-Sleep -Milliseconds 1500
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 1000
    if ((Recordings $dir).Count) { Describe (Recordings $dir)[-1].FullName } else { 'no recording' }
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    if ($stage) { Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue }
}
