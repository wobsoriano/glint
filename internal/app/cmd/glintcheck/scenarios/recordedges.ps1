# The recording UI's edges through the running app, on a 1920x1080 display at 100%:
# - the control bar asks before deleting with its whole question;
# - a remembered camera and microphone that are missing warn clear of the picker and of the
#   bar, and the warning after the recording names each device once;
# - Glint starting leaves alone a paused recording the recorder (record.exe, from
#   go build -o record.exe ./internal/media/cmd/record, in GLINT_BIN) is making in its Library;
# - a recording Glint is killed during leaves nothing in the Library once Glint starts again,
#   and one that stopped but never reached the Library is recovered;
# - Trim & Compress estimates a different size for each quality of a small clip.
# GLINT_CAPTURE_CHROME lets the screenshots see the picker and the bar.
. "$PSScriptRoot\common.ps1"
$env:GLINT_CAPTURE_CHROME = '1'
$recBase = @{ target = 'Display'; area = $null; camera = $null; mic = $null; system_audio = $false; countdown = 0; teleprompter = $false; script = ''; prompter_lines = 3 }
function Rec([hashtable]$over) { $r = $recBase.Clone(); foreach ($k in $over.Keys) { $r[$k] = $over[$k] }; $r }
$afterRec = @{ preview = $true; copy = $false; save = $false; studio = $false }
function Recordings([string]$d) { @(Get-ChildItem "$d\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue | Sort-Object Name) }
function Folders([string]$d) { (@(Get-ChildItem "$d\data\Library" -Directory -Force -ErrorAction SilentlyContinue).Name) -join ', ' }
function Picker { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint recording"') }
function Open-Picker { Keys 'Ctrl+Shift+7'; Wait-Until { Picker } 3000 | Out-Null; Start-Sleep -Milliseconds 500 }
function Shown { & $Check wins -pid $g.Id | Select-String 'visible=true' | ForEach-Object { "  $($_.Line.Trim())" } }
function Binned([string]$name) {
    $bin = (New-Object -ComObject Shell.Application).NameSpace(10)
    [bool]($bin.Items() | Where-Object { $_.Name -eq $name })
}
$g = $null
try {
    Step 'the control bar asks before deleting, with its whole question'
    $dir = New-Run 'edges-bar' @{ after_recording = $afterRec; record = (Rec @{}) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    Click 1070 980 200
    Start-Sleep -Milliseconds 700
    Glide 1500 880 150
    Start-Sleep -Milliseconds 300
    Shot "$dir\01-delete.png"
    Shown
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Stop-Glint $g

    Step 'a missing camera and microphone warn clear of the picker and the bar, each once'
    $dir = New-Run 'edges-missing' @{ after_recording = $afterRec; record = (Rec @{ camera = 'glint-missing-camera'; mic = 'glint-missing-mic' }) }
    $g = Start-Glint $dir
    Open-Picker
    Shot "$dir\01-picker.png"
    Shown
    Keys 'Enter'
    Start-Sleep -Milliseconds 1200
    Shot "$dir\02-recording.png"
    Shown
    Start-Sleep -Milliseconds 1500
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 1000
    Shot "$dir\03-finished.png"
    Shown
    $finished = (Recordings $dir)[-1].FullName
    Stop-Glint $g
    Get-Content "$dir\glint.err" | Select-String 'warnings|toast|notice'

    Step 'a paused recording another process is making is left alone when Glint starts'
    $dir = New-Run 'edges-live' @{}
    $rec = Start-Process "$Bin\record.exe" -ArgumentList "-source display -seconds 3 -pause-at 1s -pause-for 5s -out `"$dir\data\Library`"" -PassThru -NoNewWindow -RedirectStandardOutput "$dir\record.out"
    Start-Sleep -Seconds 2
    "while it is paused: [$(Folders $dir)]"
    $g = Start-Glint $dir
    Start-Sleep -Seconds 1
    "after Glint started: [$(Folders $dir)]"
    $rec.WaitForExit(30000) | Out-Null
    "recorder exited $($rec.ExitCode): [$(Folders $dir)]"
    Stop-Glint $g

    Step 'Glint killed 2 s into a recording, then started again'
    $dir = New-Run 'edges-kill' @{ after_recording = $afterRec; record = (Rec @{}) }
    $g = Start-Glint $dir
    Open-Picker
    Keys 'Enter'
    Start-Sleep -Seconds 2
    Stop-Process -Id $g.Id -Force
    $g.WaitForExit(5000) | Out-Null
    $orphan = @(Get-ChildItem "$dir\data\Library" -Directory -Force -Filter '.*.tmp')[0]
    "after the kill: $(Folders $dir); $($orphan.Name) holds $((Get-ChildItem $orphan.FullName | ForEach-Object { "$($_.Name) $($_.Length) B" }) -join ', ')"
    $g = Start-Glint $dir
    Wait-Until { -not (Test-Path $orphan.FullName) } 10000 | Out-Null
    "after starting again: [$(Folders $dir)]; $($orphan.Name) in the Recycle Bin: $(Binned $orphan.Name)"
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Start-Sleep -Milliseconds 600
    Shot "$dir\01-library-after-kill.png"
    Close-Win 'Glint Library'
    Stop-Glint $g
    $g = Start-Glint $dir
    "a third start changes nothing: [$(Folders $dir)]"
    Stop-Glint $g

    Step 'a recording that stopped but never reached the Library is recovered at the next start'
    $staged = "$dir\data\Library\.2026-10-24_09-00-00_beef.tmp"
    New-Item -ItemType Directory -Force $staged | Out-Null
    Copy-Item "$finished\screen.mp4" "$staged\screen.mp4"
    "before: [$(Folders $dir)]"
    $g = Start-Glint $dir
    Wait-Until { (Recordings $dir).Count -ge 1 } 10000 | Out-Null
    Shot "$dir\02-recovered.png"
    "after: [$(Folders $dir)]"
    $rec = (Recordings $dir)[-1].FullName
    (Meta $rec) | ConvertTo-Json -Compress
    Check probe -file "$rec\screen.mp4"
    Stop-Glint $g
    Get-Content "$dir\glint.err" | Select-String 'recovered|discarding|discard'

    Step 'Trim & Compress estimates each quality of a small clip apart'
    $dir = New-Run 'edges-trim' @{}
    $g = Start-Glint $dir
    Check video -out "$dir\clip.mp4" -seconds 3
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$dir\clip.mp4`"" -NoNewWindow -Wait
    Wait-Until { (Win 'Trim & Compress') -ne [IntPtr]::Zero } 5000 | Out-Null
    Start-Sleep -Milliseconds 1500
    Keys 'Shift'
    Front 'Trim & Compress'
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-trim.png"
    Close-Win 'Trim & Compress'
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Remove-Item Env:GLINT_CAPTURE_CHROME -ErrorAction SilentlyContinue
}
