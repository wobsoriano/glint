# A card's action tip after a click: with the pointer resting on Copy, its tip shows; a click
# flashes "Copied", and once the buttons come back under the resting pointer the tip stays
# away until the pointer leaves the card. The card must look as it did on the pointer's
# arrival, before any tip showed.
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'tips' @{ after_shot = @{ preview = $true; copy = $false; save = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
function StackShot([string]$name) {
    $s = Stack $g
    Check shot -out "$dir\$name" -rect "$($s.X),$($s.Y),$($s.W),$($s.H)" | Out-Null
}
try {
    Capture-Area 400 300 900 600
    $card = Card (Stack $g)
    Glide $card.CX ($card.Y + 8) 250
    Glide $card.Copy[0] $card.Copy[1] 150
    Start-Sleep -Milliseconds 250
    StackShot '01-arrived.png'
    Start-Sleep -Milliseconds 900
    StackShot '02-tip.png'
    Step 'resting on Copy shows its tip'
    Check cmp -a "$dir\01-arrived.png" -b "$dir\02-tip.png" -differ
    Down; Start-Sleep -Milliseconds 70; Up
    Start-Sleep -Milliseconds 400
    StackShot '03-copied.png'
    Start-Sleep -Milliseconds 2600
    StackShot '04-after.png'
    Step 'after the click the buttons are back without the tip'
    Check cmp -a "$dir\01-arrived.png" -b "$dir\04-after.png" -tol 2
    Glide 1500 600 200
    Start-Sleep -Milliseconds 500
    Glide $card.CX ($card.Y + 8) 250
    Glide $card.Copy[0] $card.Copy[1] 150
    Start-Sleep -Milliseconds 1100
    StackShot '05-back.png'
    Step 'after leaving the card and coming back, the tip shows again'
    Check cmp -a "$dir\02-tip.png" -b "$dir\05-back.png" -tol 2
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
