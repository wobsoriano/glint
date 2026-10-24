# Studio's camera bubble, cursor and keystroke panes, and the screen crop, on a copy of the
# transcribe.ps1 session given a synthetic camera.mp4 (this machine has no camera). The panes'
# controls are clicked at their places in a 1440x900 Studio at 0,0; project.json is read back
# after closing.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$src = (Get-ChildItem "$env:GLINT_OUT\x-transcribe\data\Library" -Directory -Filter '*.glintrec')[0].FullName
$dir = New-Run 'x-studio4' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
New-Item -ItemType Directory -Force "$dir\data\Library" | Out-Null
$rec = "$dir\data\Library\$(Split-Path -Leaf $src)"
Copy-Item -Recurse $src $rec
Remove-Item "$rec\project.json", "$rec\transcript.json" -ErrorAction SilentlyContinue
Check video -out "$rec\camera.mp4" -w 640 -h 480 -seconds 7 | Out-Null
$win = "$((Meta $rec).title) - Glint Studio"
$g = Start-Glint $dir
function Pane([int]$x, [string]$name) { Click $x 72 150; Start-Sleep -Milliseconds 500; Check shot -out "$dir\$name.png" -rect '1120,40,320,860' | Out-Null }
try {
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    if (-not (Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000)) { throw "Studio did not open: $win" }
    Start-Sleep -Milliseconds 2000
    Place $win 0 0 1440 900 | Out-Null
    Keys 'Shift'; Front $win
    Start-Sleep -Milliseconds 800
    Shot "$dir\01-with-camera.png"
    Pane 1272 '02-camera-pane'
    Step 'cursor: size by its slider, click highlights off'
    Pane 1208 '03-cursor-before'
    Click 1300 238 200; Start-Sleep -Milliseconds 300
    Click 1385 312 200; Start-Sleep -Milliseconds 300
    Check shot -out "$dir\03-cursor-after.png" -rect '1120,40,320,860' | Out-Null
    Step 'keystrokes: Top left'
    Click 1240 72 150; Start-Sleep -Milliseconds 400
    Click 1178 264 200; Start-Sleep -Milliseconds 300
    Check shot -out "$dir\04-keystrokes.png" -rect '1120,40,320,860' | Out-Null
    Step 'Layout: Crop video'
    Click 1176 72 150; Start-Sleep -Milliseconds 400
    Click 1200 342 200; Start-Sleep -Milliseconds 700
    Shot "$dir\05-crop.png"
    Keys 'Esc'; Start-Sleep -Milliseconds 400
    Close-Win $win
    Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000 | Out-Null
    $p = Get-Content "$rec\project.json" | ConvertFrom-Json
    "camera $($p.design.camera | ConvertTo-Json -Compress); cursor $($p.design.cursor | ConvertTo-Json -Compress); click_highlights $($p.click_highlights); keystrokes $($p.keystrokes | ConvertTo-Json -Compress); crop $($p.crop | ConvertTo-Json -Compress)"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
}
