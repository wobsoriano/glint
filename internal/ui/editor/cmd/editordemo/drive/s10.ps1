. "$PSScriptRoot\common.ps1"
$saved = Pos
$d = Start-Demo "s10" @("-data", "$S\data4")
Start-Sleep -m 1500
$t = $d.A["ann8"]
Click 880 105 250
Keys "V"
DClick $t[0] $t[1] 300; Start-Sleep -m 300
TypeText " now" 40; Keys "Enter"; Start-Sleep -m 600
WinShot "s10-text-edited"

Click $t[0] $t[1] 300; Start-Sleep -m 400
Click 1227 151 250; Start-Sleep -m 500
Click 1382 151 250; Start-Sleep -m 500
Click 1602 151 250; Start-Sleep -m 500
Drag 1690 151 1745 151 500; Start-Sleep -m 700
WinShot "s10-text-styled"
Click 923 151 250; Start-Sleep -m 600; WinShot "s10-font-menu"
Keys "Down"; Keys "Down"; Keys "Enter"; Start-Sleep -m 800
WinShot "s10-font-changed"

Click 880 105 250
Keys "Ctrl+0"; Start-Sleep -m 900; WinShot "s10-zoom100"
foreach ($k in 1..8) { Click 743 927 150 }
Start-Sleep -m 900; WinShot "s10-zoom1000"
Keys "Ctrl+1"; Start-Sleep -m 900; WinShot "s10-fit"
Glide $t[0] $t[1] 300
[Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero)
foreach ($k in 1..4) { Wheel 120; Start-Sleep -m 80 }
[Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
Start-Sleep -m 900; WinShot "s10-ctrl-wheel"
foreach ($k in 1..4) { Wheel -120; Start-Sleep -m 80 }
Start-Sleep -m 900; WinShot "s10-wheel-pan"

Keys "Ctrl+W"; Start-Sleep -m 1500; WinShot "s10-close-prompt"
Keys "Esc"; Start-Sleep -m 600
Click 880 105 250
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
