# Space for window mode and a click on a window, then Ctrl+Shift+3, Ctrl+Shift+5,
# Ctrl+Shift+6 and Ctrl+Shift+2, with no after-capture steps so each capture stands alone.
. "$PSScriptRoot\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$dir = New-Run 'captures' @{ after_shot = $off; window_shadow = $true }
$g = Start-Glint $dir
$stage = Start-Stage
Glide 1500 880 300

Step 'Ctrl+Shift+4, Space, click the stage window'
Keys 'Ctrl+Shift+4'
Start-Sleep -Milliseconds 500
Keys 'Space'
Glide 900 420 300
Start-Sleep -Milliseconds 300
Shot "$dir\01-window-mode.png"
Click 900 420 200
Start-Sleep -Milliseconds 2000
$items = Items $dir
"items: $($items.Count)"
$m = Meta $items[-1].FullName
"source: $($m.source); $($m.width)x$($m.height)"
Probe "$($items[-1].FullName)\image.png"
Check wins -title 'Glint stage'

Step 'Ctrl+Shift+3 captures the display with the pointer'
Check shot -out "$dir\02-before-fullscreen.png"
Keys 'Ctrl+Shift+3'
Start-Sleep -Milliseconds 1500
$items = Items $dir
$m = Meta $items[-1].FullName
"items: $($items.Count); $($m.width)x$($m.height), source: $($m.source)"
Check cmp -a "$($items[-1].FullName)\image.png" -b "$dir\02-before-fullscreen.png" -arect '168,121,1084,630' -brect '168,121,1084,630'
Check cmp -a "$($items[-1].FullName)\image.png" -b "$dir\02-before-fullscreen.png" -tol 0

Step 'Ctrl+Shift+5 captures the foreground window, with its shadow'
Click 700 136 300
Start-Sleep -Milliseconds 400
Check wins -title 'Glint stage'
Keys 'Ctrl+Shift+5'
Start-Sleep -Milliseconds 1500
$items = Items $dir
$m = Meta $items[-1].FullName
"items: $($items.Count); source: $($m.source); $($m.width)x$($m.height)"
Probe "$($items[-1].FullName)\image.png"

Step 'Ctrl+Shift+6 captures the previous area again'
$a = (Get-Content "$dir\data\settings.json" | ConvertFrom-Json).last_area
$sel = "$($a[0]),$($a[1]),$($a[2]),$($a[3])"
"last_area: $sel"
Glide 1500 880 200
Check shot -out "$dir\03-before-previous.png"
Keys 'Ctrl+Shift+6'
Start-Sleep -Milliseconds 1500
$items = Items $dir
"items: $($items.Count)"
Check cmp -a "$($items[-1].FullName)\image.png" -b "$dir\03-before-previous.png" -brect $sel

Step 'Ctrl+Shift+2 copies the text of an area, shows a toast and adds nothing'
$before = (Items $dir).Count
Keys 'Ctrl+Shift+2'
Start-Sleep -Milliseconds 500
Glide 630 360 300
Down
Start-Sleep -Milliseconds 100
Glide 960 560 400
Start-Sleep -Milliseconds 200
Shot "$dir\04-text-mode.png"
Up
Start-Sleep -Milliseconds 2500
Shot "$dir\05-text-toast.png"
Check clip
"items before $before, after $((Items $dir).Count)"

Stop-Glint $g

Step 'with window_shadow off, Ctrl+Shift+5 gives the bare frame'
$dir2 = New-Run 'captures-noshadow' @{ after_shot = $off; window_shadow = $false }
$g = Start-Glint $dir2
Click 700 136 300
Start-Sleep -Milliseconds 400
Keys 'Ctrl+Shift+5'
Start-Sleep -Milliseconds 1500
$items = Items $dir2
$m = Meta $items[-1].FullName
"items: $($items.Count); source: $($m.source); $($m.width)x$($m.height)"
Probe "$($items[-1].FullName)\image.png"
Stop-Glint $g

Stop-Process -Id $stage.Id -Force
Get-Content "$dir\glint.err", "$dir2\glint.err"
