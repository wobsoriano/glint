. "$PSScriptRoot\common.ps1"
$saved = Pos
$env:MYGO_FRAME_STATS = "all"
$marks = "$S\s12.marks.txt"
Remove-Item $marks -ErrorAction SilentlyContinue
$d = Start-Demo "s12" @("-data", "$S\data11", "-frames", "$S\s12.frames.txt")
Start-Sleep -m 1500
$tl = $d.A["image-tl"]; $br = $d.A["image-br"]
function IP([double]$fx, [double]$fy) { @([int]($tl[0] + ($br[0] - $tl[0]) * $fx), [int]($tl[1] + ($br[1] - $tl[1]) * $fy)) }
Click 880 105 250
Keys "V"; Keys "Ctrl+A"; Start-Sleep -m 400
$p = IP 0.50 0.22
for ($run = 0; $run -lt 2; $run++) {
    Glide $p[0] $p[1] 400; Start-Sleep -m 150; Down; Start-Sleep -m 100
    $t0 = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
    for ($k = 0; $k -lt 3; $k++) {
        Glide ($p[0] - 150) ($p[1] + 60) 500
        Glide ($p[0] + 50) ($p[1] + 140) 500
        Glide ($p[0]) ($p[1]) 500
    }
    $t1 = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
    Start-Sleep -m 100; Up
    "redactdrag$run $t0 $t1" | Out-File -Append $marks
    Start-Sleep -m 800
}
WinShot "s12-dragged"
Glide $p[0] $p[1] 300
[Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero)
foreach ($k in 1..6) { Wheel 120; Start-Sleep -m 80 }
[Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
Start-Sleep -m 1200; WinShot "s12-zoomed-selection"
Keys "Ctrl+1"; Start-Sleep -m 500
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
