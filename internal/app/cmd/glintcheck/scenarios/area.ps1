# Background launch, a second launch forwarding --capture area, Esc, and an area dragged with
# Ctrl+Shift+4: the overlay shows the frozen screen 1:1 and the capture equals the screen.
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'area'
$g = Start-Glint $dir
$stage = Start-Stage
Glide 1500 880 300

Step 'background launch: no visible windows'
Check wins -pid $g.Id
Shot "$dir\01-background.png"

Step 'a second glint.exe --capture area forwards and exits'
$sw = [Diagnostics.Stopwatch]::StartNew()
$q = Start-Process "$Bin\glint.exe" -ArgumentList '--capture', 'area' -PassThru -NoNewWindow -Wait
"second instance exited $($q.ExitCode) after $($sw.ElapsedMilliseconds) ms; first instance alive: $(-not $g.HasExited)"
Start-Sleep -Milliseconds 600
Shot "$dir\02-forwarded-overlay.png"
Check wins -pid $g.Id
Keys 'Esc'
Start-Sleep -Milliseconds 500
Shot "$dir\03-after-esc.png"
"items after Esc: $((Items $dir).Count)"

Step 'Ctrl+Shift+4, drag 400,300 to 900,600'
Check shot -out "$dir\04-before.png"
Keys 'Ctrl+Shift+4'
Start-Sleep -Milliseconds 500
Glide 700 450 300
Start-Sleep -Milliseconds 300
Shot "$dir\05-overlay-idle.png"
Glide 400 300 300
Start-Sleep -Milliseconds 150
Down
Start-Sleep -Milliseconds 100
Glide 900 600 500
Start-Sleep -Milliseconds 400
Check shot -out "$dir\06-dragging.png"
Up
Start-Sleep -Milliseconds 2000
Shot "$dir\07-after-release.png"
$a = (Get-Content "$dir\data\settings.json" | ConvertFrom-Json).last_area
$sel = "$($a[0]),$($a[1]),$($a[2]),$($a[3])"
$inner = "$($a[0] + 3),$($a[1] + 3),$($a[2] - 6),$($a[3] - 6)"
"picked area (last_area): $sel"
$items = Items $dir
"items: $($items.Count)"
Check library -dir "$dir\data"
$item = $items[-1].FullName
Step 'the capture equals the screen read just before the shortcut'
Check cmp -a "$item\image.png" -b "$dir\04-before.png" -brect $sel
Step 'mid-drag the overlay shows the frozen pixels 1:1 inside the selection, dimmed outside'
Check cmp -a "$dir\06-dragging.png" -arect $inner -b "$item\image.png" -brect "3,3,$($a[2] - 6),$($a[3] - 6)"
Check dim -frozen "$dir\04-before.png" -shown "$dir\06-dragging.png" -sel $sel
Step 'after capture: copy (the default) put the capture on the clipboard'
Check clip -png "$dir\08-clipboard.png"
Check cmp -a "$dir\08-clipboard.png" -b "$item\image.png"

Stop-Glint $g
Stop-Process -Id $stage.Id -Force
Get-Content "$dir\glint.err"
