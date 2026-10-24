. "$PSScriptRoot\common.ps1"
$saved = Pos
$d = Start-Demo "s4" @("-data", "$S\data4", "-capture")
Start-Sleep -m 1200
$tl = $d.A["image-tl"]; $br = $d.A["image-br"]
function IP([double]$fx, [double]$fy) { @([int]($tl[0] + ($br[0] - $tl[0]) * $fx), [int]($tl[1] + ($br[1] - $tl[1]) * $fy)) }
function DragF([double]$x0, [double]$y0, [double]$x1, [double]$y1) { $a = IP $x0 $y0; $b = IP $x1 $y1; Drag $a[0] $a[1] $b[0] $b[1] 500 }
function ClickF([double]$x, [double]$y) { $a = IP $x $y; Click $a[0] $a[1] 300 }

# Focus the window by clicking empty canvas below the image.
Click ($tl[0] + 200) ($br[1] + 30) 300
Keys "R"; DragF 0.06 0.08 0.24 0.30
Keys "F"; DragF 0.30 0.08 0.46 0.30
Keys "O"; DragF 0.52 0.08 0.70 0.30
Keys "L"; DragF 0.76 0.10 0.94 0.28
Keys "A"; DragF 0.06 0.62 0.24 0.40
Keys "P"; $a = IP 0.30 0.40; Glide $a[0] $a[1] 300; Down; foreach ($k in 1..12) { $b = IP (0.30 + 0.015 * $k) (0.40 + 0.05 * [math]::Sin($k)); Glide $b[0] $b[1] 60 }; Up; Start-Sleep -m 150
Keys "N"; ClickF 0.56 0.45; ClickF 0.62 0.45
Keys "T"; ClickF 0.70 0.45; TypeText "Glint editor" 40; Keys "Enter"
Keys "H"; DragF 0.06 0.70 0.24 0.92
Keys "X"; DragF 0.30 0.70 0.46 0.92
Keys "B"; DragF 0.52 0.70 0.70 0.92
Start-Sleep -m 600
WinShot "s4-tools"

# Select, move by the edge, resize by the corner handle.
Keys "V"
DragF 0.06 0.19 0.10 0.27
Start-Sleep -m 300
$h = IP 0.28 0.38; Drag $h[0] $h[1] ($h[0] + 40) ($h[1] + 30) 400
Start-Sleep -m 500
WinShot "s4-moved"

# Marquee the line and the ellipse, delete, undo, redo, undo.
DragF 0.50 0.03 0.99 0.34
Start-Sleep -m 300
WinShot "s4-marquee"
Keys "Del"; Start-Sleep -m 400; WinShot "s4-deleted"
Keys "Ctrl+Z"; Start-Sleep -m 400; WinShot "s4-undone"
Keys "Ctrl+Y"; Start-Sleep -m 400; WinShot "s4-redone"
Keys "Ctrl+Z"; Start-Sleep -m 400

# The pixelate redaction selected, to see the style bar's strength controls.
ClickF 0.38 0.81
Start-Sleep -m 400
WinShot "s4-redaction"

# The crop bar.
Keys "C"; Start-Sleep -m 500; WinShot "s4-crop"; Keys "Esc"
Start-Sleep -m 300
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
