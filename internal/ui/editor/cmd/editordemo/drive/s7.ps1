. "$PSScriptRoot\common.ps1"
$saved = Pos
$d = Start-Demo "s7" @("-data", "$S\data6")
Start-Sleep -m 1500
function Bottom { Glide 1750 600 200; foreach ($k in 1..30) { Wheel -120; Start-Sleep -m 25 }; Start-Sleep -m 600 }

Click 1832 374 250; Start-Sleep -m 700
Click 1684 811 250; Start-Sleep -m 900; WinShot "s7-aspect"
Click 1684 869 250; Start-Sleep -m 900; WinShot "s7-align"

Bottom; Click 1864 262 250; Start-Sleep -m 900; WinShot "s7-border"
Bottom; Click 1864 341 250; Start-Sleep -m 900; Bottom; WinShot "s7-watermark"
Click 1842 424 250; Start-Sleep -m 1500; WinShot "s7-perspective"
Click 1864 819 250; Start-Sleep -m 700; Bottom; Start-Sleep -m 1500; WinShot "s7-blur"
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
