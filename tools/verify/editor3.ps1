# The editor's crop presets and look controls: an arrow, crop to 16:9 (undo, redo), then with
# the sidebar scrolled to its end, progressive blur, the Hero perspective, a watermark and a
# border switched on bottom to top (each reveals options below it), a solid background at the
# top, Done, and edit.json read back; the capture reopened shows the same look. The editor is
# placed at 80,40 1360x880 at 100%.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-editor3' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $true; pin = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$Ed = 'Glint ' + [char]0x2014 + ' '
function Doc { Get-Content "$item\edit.json" -ErrorAction SilentlyContinue | ConvertFrom-Json }
try {
    Capture-Area 400 300 900 600
    $item = (Items $dir)[-1].FullName
    $title = (Meta $item).title
    Hide-Stage
    if (-not (Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000)) { throw 'the editor did not open' }
    Start-Sleep -Milliseconds 700
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Front "$Ed$title"
    Start-Sleep -Milliseconds 800

    Step 'an arrow, then crop to 16:9 and apply'
    Keys 'A'; Drag 480 440 660 560 500; Start-Sleep -Milliseconds 300
    Keys 'C'; Start-Sleep -Milliseconds 500
    Click 370 112 200; Start-Sleep -Milliseconds 400
    Shot "$dir\01-crop-16x9.png"
    Click 1362 112 200; Start-Sleep -Milliseconds 600
    Shot "$dir\02-cropped.png"
    Keys 'Ctrl+S'; Start-Sleep -Milliseconds 1200
    $d = Doc
    "after crop: crop $($d.crop | ConvertTo-Json -Compress); arrow $($d.annotations[0].shape | ConvertTo-Json -Compress -Depth 4)"
    Keys 'Ctrl+Z'; Start-Sleep -Milliseconds 400
    Keys 'Ctrl+S'; Start-Sleep -Milliseconds 1200
    $d = Doc
    "after undo: crop $($d.crop | ConvertTo-Json -Compress); arrow $($d.annotations[0].shape | ConvertTo-Json -Compress -Depth 4)"
    Keys 'Ctrl+Y'; Start-Sleep -Milliseconds 400

    Step 'look controls from the bottom of the sidebar up'
    Glide 1300 500 200
    Wheel -3000; Start-Sleep -Milliseconds 500
    Check shot -out "$dir\03-sidebar-end.png" -rect '1190,130,330,780' | Out-Null
    Click 1388 779 200; Start-Sleep -Milliseconds 400
    Click 1366 384 200; Start-Sleep -Milliseconds 400
    Click 1388 301 200; Start-Sleep -Milliseconds 400
    Click 1388 221 200; Start-Sleep -Milliseconds 400
    Check shot -out "$dir\04-sidebar-after.png" -rect '1190,130,330,780' | Out-Null
    Glide 1300 500 200
    Wheel 6000; Start-Sleep -Milliseconds 500
    Click 1235 333 200; Start-Sleep -Milliseconds 600
    Shot "$dir\05-look.png"

    Step 'Done saves the look'
    Keys 'Ctrl+Enter'
    Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 5000 | Out-Null
    $d = Doc
    "background $($d.style.background | ConvertTo-Json -Compress -Depth 4); aspect $($d.style.aspect)"
    "border $($d.effects.border | ConvertTo-Json -Compress); watermark enabled $($d.effects.watermark.enabled) text '$($d.effects.watermark.text)'"
    "perspective $($d.effects.perspective | ConvertTo-Json -Compress); blur $($d.effects.blur | ConvertTo-Json -Compress)"
    "crop $($d.crop | ConvertTo-Json -Compress)"
    Check edited -dir $item

    Step 'reopening shows the same look'
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$item\image.png`"" -NoNewWindow -Wait
    Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 900
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Keys 'Shift'; Front "$Ed$title"
    Start-Sleep -Milliseconds 800
    Shot "$dir\06-reopened.png"
    Close-Win "$Ed$title"
    $closed = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 3000
    "closed without a save prompt: $closed"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
