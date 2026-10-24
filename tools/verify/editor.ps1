# Every editor tool through its key on a 500x301 area capture, undo and redo, selection with
# the select tool, delete, canvas zoom keys, crop mode, and Done, after which glintcheck edited
# lists the annotation kinds in edit.json and compares image.png with RenderDoc. The editor is
# placed at 80,40 1360x880 at 100%, where the capture sits at 354,346 500x301 on the canvas.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-editor' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $true; pin = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$Ed = 'Glint ' + [char]0x2014 + ' '
function Tool([string]$key, [int]$x1, [int]$y1, [int]$x2, [int]$y2) {
    Keys $key
    Start-Sleep -Milliseconds 150
    Drag $x1 $y1 $x2 $y2 400
    Start-Sleep -Milliseconds 250
}
try {
    Capture-Area 400 300 900 600
    $item = (Items $dir)[-1].FullName
    $title = (Meta $item).title
    Hide-Stage
    $ok = Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000
    Start-Sleep -Milliseconds 700
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Front "$Ed$title"
    Start-Sleep -Milliseconds 800
    "editor shows: $ok"
    Shot "$dir\01-editor.png"

    Step 'each tool by its key'
    Tool 'R' 370 360 430 400
    Tool 'F' 450 360 510 400
    Tool 'O' 530 360 590 400
    Tool 'L' 610 360 670 400
    Tool 'A' 690 360 750 400
    Keys 'P'
    Glide 770 360 200; Down; Glide 800 400 200; Glide 830 370 200; Up
    Start-Sleep -Milliseconds 250
    Keys 'N'; Click 380 450 200; Click 420 450 200
    Keys 'T'; Click 470 450 200; Start-Sleep -Milliseconds 300; TypeText 'Hi there' 40; Keys 'Esc'
    Start-Sleep -Milliseconds 300
    # Not over the text just typed: a press on the selected annotation moves it with any tool.
    Tool 'H' 620 590 840 635
    Tool 'X' 370 520 460 600
    Tool 'B' 480 520 570 600
    Glide 1000 800 200
    Start-Sleep -Milliseconds 500
    Shot "$dir\02-all-tools.png"

    Step 'Ctrl+Z takes the blur away, Ctrl+Y puts it back'
    Keys 'Ctrl+Z'
    Start-Sleep -Milliseconds 400
    Shot "$dir\03-undo.png"
    Keys 'Ctrl+Y'
    Start-Sleep -Milliseconds 400
    Shot "$dir\04-redo.png"

    Step 'select tool: a click selects the rectangle, a drag moves it, Ctrl+Z moves it back'
    Keys 'V'
    Click 370 380 200
    Start-Sleep -Milliseconds 300
    Shot "$dir\05-selected.png"
    Drag 400 360 400 300 400
    Start-Sleep -Milliseconds 300
    Shot "$dir\06-moved.png"
    Keys 'Ctrl+Z'
    Start-Sleep -Milliseconds 300

    Step 'a marquee selects the pixelate and blur boxes; Delete removes them; Ctrl+Z brings them back'
    Drag 360 515 620 640 500
    Start-Sleep -Milliseconds 300
    Shot "$dir\07-marquee.png"
    Keys 'Del'
    Start-Sleep -Milliseconds 300
    Shot "$dir\08-deleted.png"
    Keys 'Ctrl+Z'
    Start-Sleep -Milliseconds 300
    Shot "$dir\09-restored.png"

    Step 'Ctrl+0 shows actual size, Ctrl+1 fits, Ctrl+scroll zooms around the pointer'
    Keys 'Ctrl+0'; Start-Sleep -Milliseconds 400; Shot "$dir\10-actual.png"
    Glide 600 500 200
    [Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero); Wheel 120; Wheel 120; Wheel 120; [Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero)
    Start-Sleep -Milliseconds 400; Shot "$dir\11-ctrl-scroll.png"
    Keys 'Ctrl+1'; Start-Sleep -Milliseconds 400; Shot "$dir\12-fit.png"

    Step 'C enters crop, Esc leaves it'
    Keys 'C'; Start-Sleep -Milliseconds 500; Shot "$dir\13-crop.png"
    Keys 'Esc'; Start-Sleep -Milliseconds 300

    Step 'Done saves every annotation'
    Keys 'Ctrl+Enter'
    $closed = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 5000
    "Done closed the editor: $closed"
    Check edited -dir $item
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
