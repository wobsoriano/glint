# On-device transcription through Studio: a 7 s recording made by the recorder while Windows
# speech synthesis says a known sentence into the output device, then Studio's Captions pane >
# Transcribe, which downloads the model and the whisper.cpp runtime into the data directory on
# first use. -Phase start drives up to the click (in the desktop lock) and leaves Glint running;
# wait for transcript.json outside the lock; -Phase finish shows the result and quits.
# Studio is placed at 0,0 1440x900. GLINT_BIN must also hold record.exe.
param([Parameter(Mandatory)] [ValidateSet('start', 'finish')] [string] $Phase)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$sentence = 'Glint turns this narration into captions on the device.'
if ($Phase -eq 'start') {
    $dir = New-Run 'x-transcribe' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
    $lib = "$dir\data\Library"
    New-Item -ItemType Directory -Force $lib | Out-Null
    $speaker = Start-Job -ScriptBlock { param($s) Add-Type -AssemblyName System.Speech; Start-Sleep -Milliseconds 1200; $v = New-Object System.Speech.Synthesis.SpeechSynthesizer; $v.Rate = -1; $v.Speak($s) } -ArgumentList $sentence
    & "$Bin\record.exe" -source display -seconds 7 -system -out $lib | Select-Object -Last 2
    Receive-Job -Wait $speaker | Out-Null
    $rec = (Get-ChildItem $lib -Directory -Filter '*.glintrec')[0].FullName
    "session: $(Split-Path -Leaf $rec); files: $((Get-ChildItem $rec).Name -join ', ')"
    $g = Start-Glint $dir
    $g.Id | Set-Content "$dir\pid.txt"
    $win = "$((Meta $rec).title) - Glint Studio"
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    if (-not (Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000)) { Stop-Glint $g; throw "Studio did not open: $win" }
    Start-Sleep -Milliseconds 2000
    Place $win 0 0 1440 900 | Out-Null
    Keys 'Shift'; Front $win
    Start-Sleep -Milliseconds 800
    Click 1368 72 200
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-captions-pane.png"
    Click 1200 320 200
    Get-Date -Format o | Set-Content "$dir\clicked.txt"
    Start-Sleep -Seconds 3
    Shot "$dir\02-transcribing.png"
    Glide 1500 880 200
} else {
    $dir = Join-Path $env:GLINT_OUT 'x-transcribe'
    $rec = (Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec')[0].FullName
    $win = "$((Meta $rec).title) - Glint Studio"
    $g = Get-Process -Id ([int](Get-Content "$dir\pid.txt"))
    "transcript.json: $(Test-Path "$rec\transcript.json"), $([int]((Get-Item "$rec\transcript.json" -ErrorAction SilentlyContinue).LastWriteTime - [datetime](Get-Content "$dir\clicked.txt")).TotalSeconds) s after the click"
    Front $win
    Start-Sleep -Milliseconds 600
    Shot "$dir\03-transcribed.png"
    if (Test-Path "$rec\transcript.json") {
        $t = Get-Content -Raw "$rec\transcript.json" | ConvertFrom-Json
        "language: $($t.language); words: $(@($t.words).Count)"
        "transcript text: $((($t.words | ForEach-Object { $_.text }) -join ' ').Trim())"
        "said:            $sentence"
    }
    "models: $(@(Get-ChildItem "$dir\data\models" -Recurse -File -ErrorAction SilentlyContinue | ForEach-Object { "$($_.Name) ($([int]($_.Length / 1MB)) MB)" }) -join ', ')"
    Close-Win $win
    Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000 | Out-Null
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
