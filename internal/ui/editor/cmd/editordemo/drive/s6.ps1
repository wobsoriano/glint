. "$PSScriptRoot\common.ps1"
$saved = Pos
$d = Start-Demo "s6" @("-data", "$S\data6", "-capture", "-size", "1600x1080")
Start-Sleep -m 1200
$tl = $d.A["image-tl"]; $br = $d.A["image-br"]
function IP([double]$fx, [double]$fy) { @([int]($tl[0] + ($br[0] - $tl[0]) * $fx), [int]($tl[1] + ($br[1] - $tl[1]) * $fy)) }
function DragF([double]$x0, [double]$y0, [double]$x1, [double]$y1) { $a = IP $x0 $y0; $b = IP $x1 $y1; Drag $a[0] $a[1] $b[0] $b[1] 500 }
Click ($tl[0] + 200) ($br[1] + 30) 300

Keys "R"; DragF 0.40 0.30 0.60 0.55
Keys "A"; DragF 0.15 0.80 0.38 0.50
Keys "C"; Start-Sleep -m 300; Click 895 151 250; Start-Sleep -m 300; WinShot "s6-crop169-edit"; Click 1838 151 250
Start-Sleep -m 700
WinShot "s6-crop169"

Drag 1696 662 1800 662 600
Drag 1692 754 1800 754 600
Drag 1712 800 1850 800 600
Start-Sleep -m 900
WinShot "s6-layout"

Click 1711 374 250; Start-Sleep -m 900; WinShot "s6-bg-solid"
Click 1832 374 250; Start-Sleep -m 1200; WinShot "s6-bg-blur"
Click 1645 404 250; Start-Sleep -m 900; WinShot "s6-bg-none"
Click 1773 374 250; Start-Sleep -m 1500; WinShot "s6-bg-wallpaper"
Click 1654 374 250; Start-Sleep -m 900

Click 1672 287 250; Start-Sleep -m 900; WinShot "s6-preset-saved"

Glide 1750 600 300
foreach ($k in 1..6) { Wheel -120; Start-Sleep -m 60 }
Start-Sleep -m 700; WinShot "s6-scroll1"
foreach ($k in 1..6) { Wheel -120; Start-Sleep -m 60 }
Start-Sleep -m 700; WinShot "s6-scroll2"
foreach ($k in 1..30) { Wheel -120; Start-Sleep -m 30 }
Start-Sleep -m 700; WinShot "s6-scroll3"
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
