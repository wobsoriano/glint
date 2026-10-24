# Studio on the transcribed session from transcribe.ps1: the transcript's Edit video tab (a
# word cut and filler removal), a clip's speed set to 2x, the clip's end trimmed by dragging,
# undo and redo, a Studio preset saved, and an export whose length shows the cuts. Studio is
# placed at 0,0 1440x900; screenshots record every step.
param([switch] $Explore)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = Join-Path $env:GLINT_OUT 'x-transcribe'
$env:GLINT_DATA_DIR = "$dir\data"
$rec = (Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec')[0].FullName
$win = "$((Meta $rec).title) - Glint Studio"
$g = Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -PassThru -NoNewWindow -RedirectStandardError "$dir\glint3.err"
function Project { Get-Content "$rec\project.json" | ConvertFrom-Json }
function Summary { $p = Project; "clips: $(($p.clips | ForEach-Object { "$($_.start_ms)-$($_.end_ms)x$($_.speed)" }) -join ' '); zooms: $(@($p.zooms).Count)" }
try {
    if (-not (Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000)) { throw "Studio did not open: $win" }
    Start-Sleep -Milliseconds 2000
    Place $win 0 0 1440 900 | Out-Null
    Keys 'Shift'; Front $win
    Start-Sleep -Milliseconds 800
    Click 1368 72 150; Start-Sleep -Milliseconds 400
    Click 1240 160 200; Start-Sleep -Milliseconds 600
    Shot "$dir\10-edit-video.png"
    if ($Explore) {
        Click 1400 72 150; Start-Sleep -Milliseconds 400; Shot "$dir\11-presets.png"
        Click 300 815 200; Start-Sleep -Milliseconds 400; Shot "$dir\12-clip-selected.png"
        return
    }
    "start: $(Summary)"
    Step 'transcript: click "this", shift-click "into", Cut selection'
    Click 1246 275 200
    [Promo.In]::keybd_event(0x10, 0, 0, [UIntPtr]::Zero); Click 1339 275 200; [Promo.In]::keybd_event(0x10, 0, 2, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 400
    Shot "$dir\13-selected-words.png"
    Click 1210 326 200
    Start-Sleep -Milliseconds 2000
    Shot "$dir\14-cut.png"
    "after the cut: $(Summary)"
    Step 'Trim silences longer than 1 s'
    Click 1276 547 200
    Start-Sleep -Milliseconds 2000
    Shot "$dir\15-silences.png"
    "after trimming silences: $(Summary)"
    Step 'undo, then redo'
    Keys 'Ctrl+Z'; Start-Sleep -Milliseconds 2000
    "after undo: $(Summary)"
    Keys 'Ctrl+Y'; Start-Sleep -Milliseconds 2000
    "after redo: $(Summary)"
    Step 'the first clip at 1.25x'
    Click 60 815 200; Start-Sleep -Milliseconds 300
    Click 324 742 200; Start-Sleep -Milliseconds 500
    Shot "$dir\16-speed-menu.png"
    Click 336 874 200; Start-Sleep -Milliseconds 2000
    "after the speed change: $(Summary)"
    Step "drag the last clip's end 100 px left"
    Drag 1105 815 1005 815 600
    Start-Sleep -Milliseconds 2000
    Shot "$dir\17-trimmed.png"
    "after the trim: $(Summary)"
    Step 'save a Studio preset'
    Click 1400 72 150; Start-Sleep -Milliseconds 400
    Click 1230 237 200; TypeText 'Verify preset' 30
    Click 1230 276 200; Start-Sleep -Milliseconds 800
    Shot "$dir\18-preset.png"
    "data dir now: $((Get-ChildItem "$dir\data" -File).Name -join ', ')"
    Step 'export: its length follows the cuts'
    $n = @(Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue).Count
    Keys 'Ctrl+E'; Start-Sleep -Milliseconds 700; Keys 'Enter'
    Wait-Until { @(Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue).Count -gt $n } 120000 | Out-Null
    Start-Sleep -Milliseconds 800
    Get-ChildItem "$dir\exports\*.mp4" | Sort-Object LastWriteTime | Select-Object -Last 1 | ForEach-Object { Check probe -file $_.FullName }
    $p = Project
    "clips add up to $([int](($p.clips | ForEach-Object { ($_.end_ms - $_.start_ms) / $_.speed } | Measure-Object -Sum).Sum)) ms"
    Close-Win $win
    Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000 | Out-Null
} finally {
    Glide 1500 880 200
    if ($Explore) { Close-Win $win; Start-Sleep -Milliseconds 1500 }
    Stop-Glint $g
}
