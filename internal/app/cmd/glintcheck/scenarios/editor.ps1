# The editor through the running app, from every place that opens it: after a capture, a
# card's Edit, the Library's Edit, Quick Look's Edit, the tray's Edit Last Capture and
# glint.exe <image>. One window opens per capture. An arrow and Done save edit.json,
# image.png and thumb.png, which glintcheck edited checks against RenderDoc, and the card shows
# the new thumbnail; Copy saves the edit too, then copies it. Coordinates are for the editor
# placed at 80,40 1360x880 and the Library at 80,60 1180x760, at 100%.
param([int]$IconX = 1642, [int]$IconY = 955)
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'editor' @{ after_shot = @{ preview = $true; edit = $true; copy = $false; save = $false }; format = 'Jpeg'; jpeg_quality = 70 }
$g = Start-Glint $dir
$stage = Start-Stage
$Ed = 'Glint ' + [char]0x2014 + ' '
function Editors([string]$title) { @(& $Check wins -pid $g.Id | Select-String "visible=true.*Glint .{1,3} $([regex]::Escape($title))`"") }
# Open-Editor waits for the editor of the capture titled $title and places it.
function Open-Editor([string]$title) {
    $ok = Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000
    Start-Sleep -Milliseconds 700
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Front "$Ed$title"
    Start-Sleep -Milliseconds 500
    "editor for '$title' shows: $ok; editor windows: $((Editors $title).Count)"
}
function Close-Editor([string]$title) {
    Close-Win "$Ed$title"
    $gone = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 3000
    "editor closed: $gone"
}
function Arrow([int]$dx) {
    Keys 'A'
    Drag (480 + $dx) 440 (660 + $dx) 560 600
    Start-Sleep -Milliseconds 300
}
function CardShot([string]$name) {
    $c = Card (Stack $g)
    Glide 1500 880 150
    Start-Sleep -Milliseconds 400
    Check shot -out "$dir\$name" -rect "$($c.X),$($c.Y),$($c.W),$($c.H)" | Out-Null
}

try {
    Step 'after a capture with Settings > After capture > Open editor, the editor opens'
    Capture-Area 400 300 900 600
    $item = (Items $dir)[-1].FullName
    $title = (Meta $item).title
    Hide-Stage
    Open-Editor $title
    CardShot '01-card-before.png'
    Front "$Ed$title"
    Shot "$dir\01-editor.png"

    Step 'an arrow, then Done (Ctrl+Enter), saves the edit into the capture'
    Arrow 0
    Shot "$dir\02-arrow.png"
    Keys 'Ctrl+Enter'
    $closed = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 4000
    "Done closed the editor: $closed"
    Check edited -dir $item
    Step 'the card shows the edited thumbnail'
    Start-Sleep -Milliseconds 500
    CardShot '03-card-after.png'
    Check cmp -a "$dir\01-card-before.png" -b "$dir\03-card-after.png" -differ
    Shot "$dir\03-after-done.png"

    Step "the card's Edit opens it again, and a second Edit focuses the same window"
    $card = Card (Stack $g)
    Press $card $card.Edit
    Open-Editor $title
    Press $card $card.Edit
    Start-Sleep -Milliseconds 800
    "editor windows after a second Edit: $((Editors $title).Count); foreground: $((& $Check wins -title $Ed | Select-Object -First 1))"

    Step 'Copy (Ctrl+C) saves the edit, then puts it on the clipboard'
    Front "$Ed$title"
    Start-Sleep -Milliseconds 300
    Arrow 120
    Set-Clipboard -Value 'glintcheck: before the copy'
    $before = (Get-Item "$item\edit.json").LastWriteTime
    Keys 'Ctrl+C'
    Wait-Until { (Get-Item "$item\edit.json").LastWriteTime -ne $before } 4000 | Out-Null
    Start-Sleep -Milliseconds 600
    Shot "$dir\04-copied.png"
    "edit.json rewritten by Copy: $((Get-Item "$item\edit.json").LastWriteTime -ne $before)"
    Check edited -dir $item
    Check clip -png "$dir\04-clipboard.png"
    Check cmp -a "$dir\04-clipboard.png" -b "$item\image.png"

    Step 'Save (Ctrl+S) writes a JPEG of quality 70 into the export folder, as the settings say'
    Keys 'Ctrl+S'
    Wait-Until { Test-Path "$dir\exports\*.jpg" } 5000 | Out-Null
    Start-Sleep -Milliseconds 400
    $jpg = Get-ChildItem "$dir\exports\*.jpg" | Select-Object -First 1
    "exported: $($jpg.Name), $($jpg.Length) bytes"
    if ($jpg) { Check encoded -dir $item -file $jpg.FullName -quality 70 }

    Step 'Pin (Ctrl+P) floats the edit'
    Front "$Ed$title"
    Keys 'Ctrl+P'
    $pinned = Wait-Until { (Win 'Pinned screenshot') -ne [IntPtr]::Zero } 4000
    Start-Sleep -Milliseconds 500
    $p = WinRect 'Pinned screenshot'
    "pin shows: $pinned at $($p.X),$($p.Y) $($p.W)x$($p.H)"
    Shot "$dir\04-pinned.png"
    Front 'Pinned screenshot'
    Keys 'Esc'
    Start-Sleep -Milliseconds 400
    Close-Editor $title

    Step "the Library's Edit opens the selected capture"
    Keys 'Ctrl+Shift+L'
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Start-Sleep -Milliseconds 500
    Click 410 185 200
    Start-Sleep -Milliseconds 400
    Click 1035 658 200
    Open-Editor $title
    Shot "$dir\05-from-library.png"
    Close-Editor $title

    Step "Quick Look's Edit opens it"
    Front 'Glint Library'
    DClick 410 185 300
    Wait-Until { (Win $title) -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 700
    $q = WinRect $title
    Click ($q.X + $q.W - 60) ($q.Y + $q.H - 33) 200
    Open-Editor $title
    Shot "$dir\06-from-quicklook.png"
    Close-Editor $title
    if ((Win $title) -ne [IntPtr]::Zero) { Front $title; Keys 'Esc' }
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 400

    Step "the tray's Edit Last Capture opens the newest capture"
    Choose 10
    Open-Editor $title
    Shot "$dir\07-from-tray.png"
    Close-Editor $title

    Step 'glint.exe <image> imports a copy into the Library and opens it'
    Check shot -out "$dir\outside.png" -rect '200,200,320,200' | Out-Null
    $n = (Items $dir).Count
    $q = Start-Process "$Bin\glint.exe" -ArgumentList "`"$dir\outside.png`"" -PassThru -NoNewWindow -Wait
    Wait-Until { (Items $dir).Count -gt $n } 4000 | Out-Null
    $imported = (Items $dir) | Where-Object { (Meta $_.FullName).imported_from } | Select-Object -Last 1
    $m = Meta $imported.FullName
    "second instance exited $($q.ExitCode); items $n -> $((Items $dir).Count); imported_from: $($m.imported_from)"
    Keys 'Shift'
    Open-Editor $m.title
    Shot "$dir\08-from-command-line.png"
    Check cmp -a "$dir\outside.png" -b "$($imported.FullName)\base.png"
    Close-Editor $m.title
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
