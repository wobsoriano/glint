# Recording through the running app. Ctrl+Shift+7 opens the picker, Enter records the display
# with system audio while a tone plays, and Ctrl+Shift+7 stops: the recording joins the
# Library, its card shows, and After recording > Copy puts Studio's render of it, an MP4 with the
# sound mixed in, on the clipboard as a file.
# glint.exe --record and the tray's Record Screen toggle the same picker. Quit Glint from the
# tray while recording finishes the recording into the Library before Glint exits.
param([int]$IconX = 1642, [int]$IconY = 955, [int]$Seconds = 3)
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'record' @{
    after_recording = @{ preview = $true; copy = $true; save = $false; studio = $false }
    record          = @{ target = 'Display'; system_audio = $true; countdown = 0 }
}
$g = Start-Glint $dir
function Recordings { @(Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue | Sort-Object Name) }
function Picker { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint recording"') }
function Tone([int]$ms) { Start-Job -ScriptBlock { param($t) [Console]::Beep(660, $t) } -ArgumentList $ms | Out-Null }

try {
    Step 'Ctrl+Shift+7 opens the picker, and again closes it'
    Keys 'Ctrl+Shift+7'
    $open = Wait-Until { Picker } 3000
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-picker.png"
    Keys 'Ctrl+Shift+7'
    $closed = Wait-Until { -not (Picker) } 3000
    "picker opened: $open; closed by the shortcut again: $closed"

    Step 'glint.exe --record and the tray menu open it too'
    Start-Process "$Bin\glint.exe" -ArgumentList '--record' -NoNewWindow -Wait
    $cli = Wait-Until { Picker } 3000
    Keys 'Ctrl+Shift+7'
    Wait-Until { -not (Picker) } 3000 | Out-Null
    Choose 7
    $tray = Wait-Until { Picker } 3000
    "picker from --record: $cli; from the tray: $tray"

    Step "Enter records the display for $Seconds s with a tone playing, Ctrl+Shift+7 stops"
    Check loopback -seconds 1 -silent
    Keys 'Enter'
    Start-Sleep -Milliseconds 800
    Tone ($Seconds * 1000 - 600)
    Start-Sleep -Milliseconds 700
    Shot "$dir\02-recording.png"
    Start-Sleep -Milliseconds ($Seconds * 1000 - 700)
    Set-Clipboard -Value 'glintcheck: before the recording'
    Keys 'Ctrl+Shift+7'
    $done = Wait-Until { (Recordings).Count -ge 1 } 15000
    $rec = (Recordings)[-1].FullName
    "recording in the Library: $done ($(Split-Path -Leaf $rec)); files: $((Get-ChildItem $rec).Name -join ', ')"
    Meta $rec | ConvertTo-Json -Compress
    Check probe -file "$rec\screen.mp4"

    Step 'the card shows, and the clipboard holds the MP4 as a file'
    Wait-Until { (& $Check clip) -match '^file: .*\.mp4' } 30000 | Out-Null
    Start-Sleep -Milliseconds 500
    $s = Stack $g
    "stack: $(if ($s) { "$($s.X),$($s.Y) $($s.W)x$($s.H)" } else { 'none' })"
    Shot "$dir\03-finished.png"
    $clip = & $Check clip
    $clip
    $file = ($clip | Select-String '^file: (.*)$').Matches | ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
    if ($file) {
        Check probe -file $file
        $render = Get-ChildItem "$rec\render\export-*.mp4" -ErrorAction SilentlyContinue | Select-Object -First 1
        "the copied file is Studio's render of the recording ($($render.Name)): $($render -and (Get-FileHash $file).Hash -eq (Get-FileHash $render.FullName).Hash)"
    }

    Step 'Quit Glint from the tray while recording finishes the recording first'
    $n = (Recordings).Count
    Keys 'Ctrl+Shift+7'
    Wait-Until { Picker } 3000 | Out-Null
    Keys 'Enter'
    Start-Sleep -Seconds 2
    Shot "$dir\04-recording-again.png"
    Choose 12
    $exited = Wait-Until { $g.HasExited } 15000
    $last = (Recordings)[-1].FullName
    "glint exited: $exited; recordings $n -> $((Recordings).Count); staging folders left: $(@(Get-ChildItem "$dir\data\Library" -Directory -Force -Filter '.*.tmp').Count)"
    "files: $((Get-ChildItem $last).Name -join ', ')"
    Check probe -file "$last\screen.mp4"
} finally {
    Glide 1500 880 200
    Get-Job | Remove-Job -Force
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
