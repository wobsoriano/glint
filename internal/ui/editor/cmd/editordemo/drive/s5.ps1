. "$PSScriptRoot\common.ps1"
$saved = Pos
$lib = Get-ChildItem "$S\data4\Library" -Directory | Select-Object -First 1

$d = Start-Demo "s5a" @("-data", "$S\data4")
Start-Sleep -m 1200
WinShot "s5-reopened"
Keys "Ctrl+Enter"
Stop-Demo $d
Copy-Item "$($lib.FullName)\edit.json" "$S\s5.reopen.edit.json"

$d = Start-Demo "s5b" @("-data", "$S\data4")
Start-Sleep -m 1200
$tl = $d.A["image-tl"]; $br = $d.A["image-br"]
function IP([double]$fx, [double]$fy) { @([int]($tl[0] + ($br[0] - $tl[0]) * $fx), [int]($tl[1] + ($br[1] - $tl[1]) * $fy)) }
function DragF([double]$x0, [double]$y0, [double]$x1, [double]$y1) { $a = IP $x0 $y0; $b = IP $x1 $y1; Drag $a[0] $a[1] $b[0] $b[1] 500 }
Click ($tl[0] + 200) ($br[1] + 30) 300

Keys "X"; DragF 0.01 0.005 0.40 0.065
Keys "B"; DragF 0.45 0.005 0.95 0.065
Start-Sleep -m 600
WinShot "s5-redactions"
Drag 851 151 935 151 500
Start-Sleep -m 600
WinShot "s5-strength"

$chips = [ordered]@{ "freeform" = 729; "original" = 800; "1x1" = 853; "16x9" = 895; "9x16" = 941; "4x3" = 985; "3x2" = 1026 }
foreach ($name in $chips.Keys) {
    Keys "C"; Start-Sleep -m 300
    Click $chips[$name] 151 250
    if ($name -eq "freeform") { $h = IP 0 0; Drag $h[0] $h[1] ($h[0] + 120) ($h[1] + 80) 400 }
    Start-Sleep -m 300
    Click 1838 151 250
    Start-Sleep -m 700
    WinShot "s5-crop-$name"
    Keys "Ctrl+Z"; Start-Sleep -m 400
}
WinShot "s5-crop-undone"
Keys "C"; Start-Sleep -m 300; Click $chips["16x9"] 151 250; Click 1838 151 250; Start-Sleep -m 500
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
