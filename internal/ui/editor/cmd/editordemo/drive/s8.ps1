. "$PSScriptRoot\common.ps1"
$saved = Pos
$T = "$S\files"
New-Item -ItemType Directory -Force $T | Out-Null
Remove-Item "$T\*" -Force -ErrorAction SilentlyContinue
$log = "$S\s8.check.txt"
"" | Out-File $log
$item = (Get-ChildItem "$S\data6\Library" -Directory | Select-Object -First 1).FullName
$d = Start-Demo "s8" @("-data", "$S\data6")
Start-Sleep -m 1500

Click 1684 799 250; Start-Sleep -m 900; WinShot "s8-aspect"
Click 1684 857 250; Start-Sleep -m 900; WinShot "s8-align"

Click 1841 275 250
TypeInto "Export presets" "$T\looks.glintpreset" $log
Click 880 105 250
WinShot "s8-exported"

Keys "Ctrl+P"; Start-Sleep -m 2500; WinShot "s8-pinned"
$pin = Win "editordemo pin"
"pin window: $pin" | Out-File -Append $log
if ($pin -ne [IntPtr]::Zero) { [Promo.In]::SetForegroundWindow($pin) | Out-Null; Start-Sleep -m 300; Keys "Esc"; Start-Sleep -m 500 }
Click 880 105 250

New-Item -ItemType Directory "$item\edit.json.tmp" | Out-Null
Keys "V"; Keys "Ctrl+A"; Keys "Right"; Keys "Right"
Keys "Ctrl+Enter"; Start-Sleep -m 2500
WinShot "s8-save-failed"
"alive after failed save: $(-not $d.Proc.HasExited)" | Out-File -Append $log
Remove-Item "$item\edit.json.tmp" -Force -ErrorAction SilentlyContinue
Keys "Ctrl+Enter"
Stop-Demo $d
"exited after Done: $($d.Proc.HasExited)" | Out-File -Append $log
Glide $saved[0] $saved[1] 200
