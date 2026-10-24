# Drives Studio through the acceptance pass on the real desktop, one phase per run so each
# holds the desktop lock for a minute or two. Run the phases in order on a new work dir:
#
#   powershell -File internal/ui/studio/cmd/studiodemo/drive.ps1 -Work <dir> -Phase make
#   go run ./tools/desklock -- powershell -NoProfile -File internal/ui/studio/cmd/studiodemo/drive.ps1 -Work <dir> -Phase play
#   ... -Phase edit, -Phase look, -Phase captions, -Phase export (each inside the lock)
#   powershell -File internal/ui/studio/cmd/studiodemo/drive.ps1 -Work <dir> -Phase check
#
# Clicks are window coordinates of a 1440x900 window at the top left of a 1920x1080 display at
# 100% scale, on the synthetic session the make phase writes; the edit, look and captions
# phases expect the phases before them to have run. Screenshots and logs land in <dir>\shots.
param([Parameter(Mandatory)][string]$Work, [Parameter(Mandatory)][string]$Phase)
$ErrorActionPreference = "Stop"
$Repo = (Resolve-Path "$PSScriptRoot\..\..\..\..\..").Path
$SD = $Work
$Sess = "$SD\sess60"
$env:GLINT_DATA_DIR = "$SD\data"

if ($Phase -eq "make") {
    New-Item -ItemType Directory -Force "$SD\data", "$SD\out", "$SD\shots" | Out-Null
    Push-Location $Repo
    go build -o "$SD\studiodemo.exe" ./internal/ui/studio/cmd/studiodemo
    Pop-Location
    & "$SD\studiodemo.exe" -make $Sess -speech -width 1920 -height 1080 -fps 60
    return
}
if ($Phase -eq "check") {
    & "$SD\studiodemo.exe" -info $Sess
    foreach ($f in Get-ChildItem "$SD\out" -Filter "*.mp4") {
        "== $($f.Name)"
        ffprobe -v error -show_entries "format=duration:stream=codec_type,codec_name,width,height,r_frame_rate,nb_frames,duration" -of compact $f.FullName
    }
    Get-Content "$SD\shots\*.log" | Select-String "run:|exported:|copied:|toast:"
    Get-Content "$SD\shots\*.txt"
    return
}

. "$Repo\tools\drive\drive.ps1"
$ErrorActionPreference = "Continue"

function Press([byte]$vk) { [Promo.In]::keybd_event($vk, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 30; [Promo.In]::keybd_event($vk, 0, 2, [UIntPtr]::Zero); Start-Sleep -m 150 }
function ShiftClick([int]$x, [int]$y) {
    Glide $x $y 400; Start-Sleep -m 90
    [Promo.In]::keybd_event(0x10, 0, 0, [UIntPtr]::Zero); Start-Sleep -m 60
    Down; Start-Sleep -m 70; Up; Start-Sleep -m 60
    [Promo.In]::keybd_event(0x10, 0, 2, [UIntPtr]::Zero); Start-Sleep -m 150
}
function Snap([string]$name) { Shot "$SD\shots\$name.png" }
function Logged([string]$log, [string]$pattern) { @(Get-Content "$SD\shots\$log.log" -ErrorAction SilentlyContinue) -match $pattern }
function StartStudio([string]$session, [string]$log) {
    $proc = Start-Process -FilePath "$SD\studiodemo.exe" -ArgumentList @("-open", "`"$session`"", "-savedir", "`"$SD\out`"") -RedirectStandardOutput "$SD\shots\$log.log" -RedirectStandardError "$SD\shots\$log.err" -PassThru
    $title = (Split-Path $session -Leaf) + " - Glint Studio"
    for ($i = 0; $i -lt 50 -and (Win $title) -eq [IntPtr]::Zero; $i++) { Start-Sleep -m 200 }
    Place $title 0 0 1440 900 | Out-Null
    Start-Sleep -m 300
    Front $title
    Start-Sleep -m 700
    $proc
}
function StopStudio($proc) {
    Start-Sleep -m 900
    $proc.CloseMainWindow() | Out-Null
    if (-not $proc.WaitForExit(5000)) { Stop-Process -Id $proc.Id -Force }
}

switch ($Phase) {
    "play" {
        $app = StartStudio $Sess "play"
        Press 0x24
        Keys "Space"
        Start-Sleep -m 11000
        Keys "Space"
        Start-Sleep -m 800
        Snap "play-1-after"
        Click 262 780
        Start-Sleep -m 600
        Snap "play-2-scrub5s"
        Drag 262 780 900 780 1500
        Start-Sleep -m 600
        Snap "play-3-scrub-drag"
        StopStudio $app
    }
    "edit" {
        $app = StartStudio $Sess "edit"
        Click 262 780
        Keys "S"
        Start-Sleep -m 500
        Snap "edit-1-split"
        Click 322 742
        Start-Sleep -m 500
        Snap "edit-2-speed-menu"
        for ($i = 0; $i -lt 6; $i++) { Keys "Down" }
        Keys "Enter"
        Start-Sleep -m 600
        Snap "edit-3-speed2x"
        Drag 23 820 75 820 900
        Start-Sleep -m 600
        Snap "edit-4-trim"
        Click 120 820
        Keys "Del"
        Start-Sleep -m 600
        Snap "edit-5-delete"
        Keys "Ctrl+Z"
        Start-Sleep -m 600
        Snap "edit-6-undo"
        Keys "Ctrl+Y"
        Start-Sleep -m 600
        Snap "edit-7-redo"
        Keys "Ctrl+Z"
        Start-Sleep -m 600
        Click 873 869
        Drag 873 869 700 869 900
        Start-Sleep -m 500
        Drag 755 869 820 869 700
        Start-Sleep -m 600
        Snap "edit-8-zoom-retimed"
        Click 1286 269 300
        Start-Sleep -m 500
        Drag 560 380 820 230 700
        Start-Sleep -m 500
        Snap "edit-9-zoom-focus"
        Keys "Enter"
        StopStudio $app
    }
    "look" {
        $app = StartStudio $Sess "look"
        Click 540 780
        Click 1144 72 300
        Click 1239 195 300
        Start-Sleep -m 500
        Snap "look-1-solid"
        Click 1181 195 300
        Click 1339 500 300
        Click 1339 546 300
        Click 1400 592 300
        Start-Sleep -m 500
        Snap "look-2-frame"
        Click 1176 72 300
        Click 1274 195 300
        Click 1166 229 300
        Start-Sleep -m 500
        Snap "look-3-9x16-fill"
        Click 1180 195 300
        Click 1204 343 300
        Start-Sleep -m 500
        Drag 182 170 330 260 700
        Click 440 81 300
        Start-Sleep -m 500
        Snap "look-4-crop-1x1"
        Click 797 81 300
        Start-Sleep -m 500
        Snap "look-5-cropped"
        Click 1240 72 300
        Click 1285 263 300
        Start-Sleep -m 600
        Snap "look-6-keys-top-right"
        StopStudio $app
    }
    "captions" {
        $app = StartStudio $Sess "captions"
        Click 1368 72 300
        Click 1201 319 300
        Start-Sleep -m 1500
        Snap "captions-1-downloading"
        $sw = [Diagnostics.Stopwatch]::StartNew()
        while ($sw.Elapsed.TotalSeconds -lt 100 -and -not (Test-Path "$Sess\transcript.json")) { Start-Sleep -m 250 }
        "transcript after {0:N1} s" -f $sw.Elapsed.TotalSeconds | Out-File "$SD\shots\captions-timing.txt"
        Start-Sleep -m 800
        Click 1385 264 300
        Click 1177 529 300
        Keys "Shift+Right"
        Start-Sleep -m 600
        Snap "captions-2-karaoke"
        Click 1240 160 300
        Click 1328 294 300
        Start-Sleep -m 500
        Snap "captions-3-word-seek"
        Click 1203 333 300
        ShiftClick 1168 352
        Keys "Del"
        Start-Sleep -m 800
        Snap "captions-4-cut-words"
        Click 1276 505 300
        Start-Sleep -m 800
        Snap "captions-5-fillers"
        Click 1276 625 300
        Start-Sleep -m 2500
        Snap "captions-6-silences"
        StopStudio $app
    }
    "export" {
        Remove-Item "$SD\out\*", "$Sess\render\export-*" -ErrorAction SilentlyContinue
        $app = StartStudio $Sess "export"
        Keys "Ctrl+E"
        Start-Sleep -m 500
        Click 705 361 300
        Click 650 466 300
        Snap "export-1-dialog-720p30"
        Click 756 361 300
        Click 704 466 300
        Snap "export-2-dialog-1080p60"
        Click 877 578 300
        Start-Sleep -m 1000
        Snap "export-3-progress"
        Click 886 472 300
        Start-Sleep -m 1200
        Snap "export-4-cancelled"
        "after cancel: [" + ((Get-ChildItem "$SD\out", "$Sess\render" -Filter "*.mp4" | ForEach-Object { $_.Name }) -join ", ") + "]" | Out-File "$SD\shots\export-cancel.txt"
        Keys "Ctrl+E"
        Start-Sleep -m 500
        $sw = [Diagnostics.Stopwatch]::StartNew()
        Click 877 578 300
        while ($sw.Elapsed.TotalSeconds -lt 150 -and @(Logged "export" "^exported:").Count -lt 1) { Start-Sleep -m 100 }
        "export took {0:N1} s" -f $sw.Elapsed.TotalSeconds | Out-File "$SD\shots\export-timing.txt"
        Start-Sleep -m 500
        Snap "export-5-saved"
        $sw = [Diagnostics.Stopwatch]::StartNew()
        Keys "Ctrl+S"
        while ($sw.Elapsed.TotalSeconds -lt 30 -and @(Logged "export" "^exported:").Count -lt 2) { Start-Sleep -m 50 }
        "save from the cached render took {0:N2} s" -f $sw.Elapsed.TotalSeconds | Out-File -Append "$SD\shots\export-timing.txt"
        Keys "Ctrl+C"
        Start-Sleep -m 1500
        StopStudio $app
        (Get-FileHash "$Sess\project.json").Hash | Out-File "$SD\shots\export-hash-before.txt"
        $app = StartStudio $Sess "reopen"
        Snap "export-6-reopened"
        StopStudio $app
        (Get-FileHash "$Sess\project.json").Hash | Out-File "$SD\shots\export-hash-after.txt"
    }
}
