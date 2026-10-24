. "$PSScriptRoot\common.ps1"
$saved = Pos
$T = "$S\files"
Remove-Item "$T\saveas.png*" -Force -ErrorAction SilentlyContinue
$log = "$S\s9.check.txt"
"" | Out-File $log
$d = Start-Demo "s9" @("-data", "$S\data6")
Start-Sleep -m 1500

Click 1764 275 250
TypeInto "Import presets" "$T\looks.glintpreset" $log
Click 880 105 250
WinShot "s9-imported"
Click 1645 392 250; Start-Sleep -m 1000; WinShot "s9-bg-none"
Click 1650 237 250; Start-Sleep -m 1200; WinShot "s9-preset-applied"

Click 880 105 250
Keys "Ctrl+C"; Start-Sleep -m 2500; WinShot "s9-copied"
Keys "Ctrl+S"; Start-Sleep -m 3000; WinShot "s9-saved"
Keys "Ctrl+Shift+S"
TypeInto "Save as" "$T\saveas.png" $log
Click 880 105 250
WinShot "s9-savedas"
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
