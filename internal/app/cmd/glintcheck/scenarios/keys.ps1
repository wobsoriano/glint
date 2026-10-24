# The overlay's keys and first frame: the loupe shows at the pointer before it moves, the
# loupe at a corner of the display, Enter takes the whole display, Esc cancels.
. "$PSScriptRoot\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$dir = New-Run 'keys' @{ after_shot = $off }
$g = Start-Glint $dir
$stage = Start-Stage
try {
    Step 'the loupe and the window under the pointer show before the pointer moves'
    Glide 900 420 300
    Start-Sleep -Milliseconds 300
    Check shot -out "$dir\01-before.png"
    Keys 'Ctrl+Shift+4'
    Start-Sleep -Milliseconds 500
    Shot "$dir\02-first-frame.png"

    Step 'the loupe at the top-left corner'
    Glide 2 2 300
    Start-Sleep -Milliseconds 300
    Shot "$dir\03-corner-loupe.png"

    Step 'Enter takes the whole display'
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    $items = Items $dir
    "items: $($items.Count)"
    Check cmp -a "$($items[-1].FullName)\image.png" -arect '168,121,1084,630' -b "$dir\01-before.png" -brect '168,121,1084,630'

    Step 'Esc cancels an overlay a shortcut opened'
    Keys 'Ctrl+Shift+4'
    Start-Sleep -Milliseconds 500
    Glide 700 400 200
    Keys 'Esc'
    Start-Sleep -Milliseconds 500
    Shot "$dir\04-after-esc.png"
    "items: $((Items $dir).Count)"
    Check wins -pid $g.Id
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force
}
