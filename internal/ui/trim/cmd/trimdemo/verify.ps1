# Checks what drive.ps1 left in -Out: the play run's sync, the scrub, the trimmed export (with
# ffprobe and the clip's frame codes) and that the cancelled export left nothing. It needs no
# desktop.
#
#   powershell -NoProfile -File verify.ps1 -Demo trimdemo.exe -Out out
param([string]$Demo, [string]$Out)
$ErrorActionPreference = 'Stop'
$failed = 0
function Check([bool]$ok, [string]$what) {
    if ($ok) { "PASS $what" } else { "FAIL $what"; $script:failed++ }
}

$log = Get-Content "$Out\demo.log"
$run = $log | Select-String ' run: (\d+) frames from (\d+) to (\d+) ms, audio clock (\w+);.*worst ([\d.]+) ms; clock advanced (\d+) ms over (\d+) ms' | Select-Object -First 1
Check ($null -ne $run) 'the demo logged a play run'
if ($run) {
    $g = $run.Matches[0].Groups
    $span = [int]$g[3].Value - [int]$g[2].Value
    Check ($g[4].Value -eq 'true') 'the run followed the audio device clock'
    Check ($span -ge 10000) "the run played $span ms of video"
    Check ([double]$g[5].Value -lt 50) "worst |frame time - audio clock| is $($g[5].Value) ms"
    Check ([math]::Abs([int]$g[6].Value - [int]$g[7].Value) -lt 50) "the audio clock advanced $($g[6].Value) ms over $($g[7].Value) ms of wall time"
}
# Loopback dates a click when the audio engine mixes it, a little before the speakers play it,
# and the preview shows a frame a refresh or so after it is handed over: the offset is a bound
# on what a viewer meets, not an exact lip-sync figure.
$loop = $log | Select-String 'loopback: (\d+) whole-second frames.*mean ([-\d.]+) ms, min ([-\d.]+), max ([-\d.]+) ms' | Select-Object -First 1
Check ($null -ne $loop) 'the demo paired whole-second frames with clicks heard in the loopback'
if ($loop) {
    $g = $loop.Matches[0].Groups
    Check ([int]$g[1].Value -ge 8) "$($g[1].Value) whole-second frames were paired with clicks"
    Check ([math]::Max([math]::Abs([double]$g[3].Value), [math]::Abs([double]$g[4].Value)) -lt 50) "click heard minus frame shown: mean $($g[2].Value) ms, from $($g[3].Value) to $($g[4].Value) ms"
}
$seek = $log | Select-String 'shown (\d+) ms \(paused\)' | ForEach-Object { [int]$_.Matches[0].Groups[1].Value }
Check ($seek -contains 8000) 'scrubbing to 8 s showed the frame at 8000 ms'

$drive = Get-Content "$Out\drive.log"
$steps = ($drive | Select-String 'progress seen: (.*)').Matches[0].Groups[1].Value -split ', '
Check ($steps.Count -ge 5 -and $steps[-1] -eq 'Exporting 100%') "the progress bar showed $($steps.Count) steps up to $($steps[-1])"

$files = @(Get-ChildItem "$Out\exports" | ForEach-Object Name)
Check ($files.Count -eq 1 -and $files[0] -eq 'clip (trimmed).mp4') "the exports folder holds only the finished export: $($files -join ', ')"

$export = "$Out\exports\clip (trimmed).mp4"
$probe = ffprobe -v error -show_entries 'stream=codec_type,codec_name,width,height,r_frame_rate,bit_rate,sample_rate,channels:format=duration,bit_rate' -of json $export | ConvertFrom-Json
$probe | ConvertTo-Json -Depth 4 | Set-Content "$Out\ffprobe.json"
$v = $probe.streams | Where-Object codec_type -eq 'video'
$a = $probe.streams | Where-Object codec_type -eq 'audio'
$duration = [double]$probe.format.duration
Check ($v.codec_name -eq 'h264' -and $v.width -eq 1280 -and $v.height -eq 720) "ffprobe: video $($v.codec_name) $($v.width)x$($v.height) $($v.r_frame_rate)"
Check ([math]::Abs($duration - 3.0) -lt 0.1) "ffprobe: duration $duration s"
Check ($a.codec_name -eq 'aac' -and $a.sample_rate -eq '48000') "ffprobe: audio $($a.codec_name) $($a.sample_rate) Hz $($a.channels) ch"
Check ([int]$v.bit_rate -lt 2500000) "ffprobe: video bit rate $($v.bit_rate) b/s, the source's is $(((ffprobe -v error -select_streams v -show_entries stream=bit_rate -of csv=p=0 "$Out\..\clip.mp4")))"

$codes = & $Demo -check $export
$codes
$m = [regex]::Match($codes, 'first frame (\d+), last frame (\d+)')
$first, $last = [int]$m.Groups[1].Value, [int]$m.Groups[2].Value
Check ([math]::Abs($first - 60) -le 2) "the export starts on source frame $first (2 s is frame 60)"
Check ([math]::Abs($last - 149) -le 2) "the export ends on source frame $last (5 s is frame 150)"

$saved = "$Out\saved\saved as.mp4"
$probe = ffprobe -v error -show_entries 'stream=codec_type,codec_name,width,height:format=duration' -of json $saved | ConvertFrom-Json
$v = @($probe.streams | Where-Object codec_type -eq 'video')
$a = @($probe.streams | Where-Object codec_type -eq 'audio')
Check ($v.Count -eq 1 -and $v[0].codec_name -eq 'hevc' -and $v[0].width -eq 852 -and $v[0].height -eq 480) "ffprobe: Save As wrote video $($v[0].codec_name) $($v[0].width)x$($v[0].height)"
Check ($a.Count -eq 0) "ffprobe: Save As with audio removed has $($a.Count) audio streams"
Check ([math]::Abs([double]$probe.format.duration - 20) -lt 0.1) "ffprobe: Save As kept the whole $($probe.format.duration) s"

foreach ($p in 1..10) { Check (@(Get-ChildItem "$Out\$p-*.png").Count -eq 1) "screenshot $p" }
"$failed check(s) failed"
exit $failed
