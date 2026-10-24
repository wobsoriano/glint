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
# The words on the transcript's first line as [left, right, selected] screen columns, read from
# the screen because where each word sits depends on the transcribed text. A word is a run of
# white text columns; its selection shows as a blue background.
function Transcript-Words {
    $bmp = New-Object Drawing.Bitmap 270, 19
    $gr = [Drawing.Graphics]::FromImage($bmp); $gr.CopyFromScreen(1140, 266, 0, 0, $bmp.Size); $gr.Dispose()
    $words = @(); $start = -1; $end = 0; $gap = 0
    for ($x = 0; $x -le $bmp.Width; $x++) {
        $ink = $false
        for ($y = 0; $x -lt $bmp.Width -and $y -lt $bmp.Height -and -not $ink; $y++) {
            $c = $bmp.GetPixel($x, $y)
            $ink = (0.299 * $c.R + 0.587 * $c.G + 0.114 * $c.B) -gt 150 -and [math]::Abs([int]$c.R - [int]$c.B) -lt 60
        }
        if ($ink) { if ($start -lt 0) { $start = $x }; $end = $x; $gap = 0; continue }
        if ($start -ge 0 -and (++$gap -ge 5 -or $x -eq $bmp.Width)) {
            $blue = @($start..$end | Where-Object { $c = $bmp.GetPixel($_, 2); $c.B -gt 150 -and $c.R -lt 80 }).Count
            $words += , @(($start + 1140), ($end + 1140), ($blue -gt 3))
            $start = -1
        }
    }
    $bmp.Dispose()
    , $words
}
function Mid($word) { [int](($word[0] + $word[1]) / 2) }
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
    $words = Transcript-Words
    if ($words.Count -lt 5) { throw "the transcript's first line shows $($words.Count) words" }
    Click (Mid $words[2]) 275 200
    [Promo.In]::keybd_event(0x10, 0, 0, [UIntPtr]::Zero); Click (Mid $words[4]) 275 200; [Promo.In]::keybd_event(0x10, 0, 2, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 400
    Shot "$dir\13-selected-words.png"
    $now = Transcript-Words
    "selected words of the first line: $((0..($now.Count - 1) | Where-Object { $now[$_][2] }) -join ', ') of $($now.Count)"
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
    Keys 'Ctrl+E'; Start-Sleep -Milliseconds 700; Shot "$dir\19-export.png"; Keys 'Enter'
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
