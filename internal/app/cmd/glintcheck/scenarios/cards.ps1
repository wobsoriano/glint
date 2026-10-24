# The preview card of an area capture and its actions through the running app: Copy, Save,
# Quick Look and Pin on the first capture, Delete on the second, and dragging the third into
# an Explorer window. After capture only the card shows, so the clipboard and the export
# folder change only by what the card does.
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'cards' @{ after_shot = @{ preview = $true; copy = $false; save = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$dest = Join-Path $dir 'dropped'
New-Item -ItemType Directory -Force $dest | Out-Null
$explorer = $null
try {
    Step 'Ctrl+Shift+4 capture: a preview card shows bottom-left'
    Capture-Area 400 300 900 600
    $items = Items $dir
    $first = $items[-1].FullName
    "items: $($items.Count)"
    $s = Stack $g
    "stack: $($s.X),$($s.Y) $($s.W)x$($s.H)"
    $card = Card $s
    Shot "$dir\01-card.png"

    Step 'Copy puts image.png on the clipboard'
    Set-Clipboard -Value 'glintcheck: not the capture'
    Press $card $card.Copy
    Start-Sleep -Milliseconds 300
    Shot "$dir\02-copied.png"
    Check clip -png "$dir\02-clipboard.png"
    Check cmp -a "$dir\02-clipboard.png" -b "$first\image.png"
    Start-Sleep -Milliseconds 1500

    Step 'Save writes the capture into the export folder'
    Press $card $card.Save
    Wait-Until { Test-Path "$dir\exports\*.png" } | Out-Null
    Start-Sleep -Milliseconds 300
    Shot "$dir\03-saved.png"
    $saved = @(Get-ChildItem "$dir\exports\*.png")
    "exports: $($saved.Name -join ', ')"
    if ($saved) { Check cmp -a $saved[0].FullName -b "$first\image.png" }
    Start-Sleep -Milliseconds 1500

    Step 'More > Quick Look opens the capture large'
    Hide-Stage
    MenuRow $card 'QuickLook'
    $title = (Meta $first).title
    $ok = Wait-Until { (Win $title) -ne [IntPtr]::Zero }
    Start-Sleep -Milliseconds 600
    "Quick Look window '$title' shows: $ok"
    Shot "$dir\04-quicklook.png"
    Front $title
    Keys 'Esc'
    Start-Sleep -Milliseconds 500
    "Quick Look closed by Esc: $((Win $title) -eq [IntPtr]::Zero)"
    Show-Stage

    Step 'Pin floats the capture; the card leaves the stack'
    Press $card $card.Pin
    $ok = Wait-Until { (Win 'Pinned screenshot') -ne [IntPtr]::Zero }
    Start-Sleep -Milliseconds 500
    $pin = WinRect 'Pinned screenshot'
    "pin window shows: $ok at $($pin.X),$($pin.Y) $($pin.W)x$($pin.H); stack after: $(if (Stack $g) { 'shown' } else { 'gone' })"
    Glide 1500 880 200
    Start-Sleep -Milliseconds 300
    Check shot -out "$dir\05-pin.png" -rect "$($pin.X),$($pin.Y),$($pin.W),$($pin.H)"
    $m = Meta $first
    "pin is the capture's size: $($pin.W -eq $m.width -and $pin.H -eq $m.height) ($($m.width)x$($m.height))"
    # The pin draws the capture inside its 1 px border, a hair smaller, so noise differs pixel
    # by pixel; the text half of the capture must match closely.
    $tx = [int]($m.width * 0.55); $tw = $m.width - $tx - 4
    Check cmp -a "$dir\05-pin.png" -arect "$tx,4,$tw,$($m.height - 8)" -b "$first\image.png" -brect "$tx,4,$tw,$($m.height - 8)" -maxmean 6
    Shot "$dir\05-pin-screen.png"
    Front 'Pinned screenshot'
    Keys 'Esc'
    Start-Sleep -Milliseconds 400
    "pin closed by Esc: $((Win 'Pinned screenshot') -eq [IntPtr]::Zero)"

    Step 'More > Delete on a second capture moves it out of the Library'
    Capture-Area 450 330 850 560
    $second = (Items $dir)[-1].FullName
    "items: $((Items $dir).Count), deleting $(Split-Path -Leaf $second)"
    $card = Card (Stack $g)
    MenuRow $card 'Delete'
    $gone = Wait-Until { -not (Test-Path $second) }
    Start-Sleep -Milliseconds 300
    Shot "$dir\06-deleted.png"
    "deleted item left the Library: $gone; items: $((Items $dir).Count); stack after: $(if (Stack $g) { 'shown' } else { 'gone' })"

    Step 'dragging a third card into Explorer copies its PNG'
    Capture-Area 420 320 880 580
    $third = (Items $dir)[-1].FullName
    $card = Card (Stack $g)
    Start-Process explorer.exe $dest
    $leaf = Split-Path -Leaf $dest
    Wait-Until { (Win $leaf) -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place $leaf 1270 160 640 600 | Out-Null
    Front $leaf
    Start-Sleep -Milliseconds 600
    Drag ($card.X + 30) ($card.Y + $card.H - 30) 1590 480 900
    $dropped = Wait-Until { Test-Path "$dest\*.png" } 5000
    Start-Sleep -Milliseconds 600
    Shot "$dir\07-dropped.png"
    $files = @(Get-ChildItem "$dest\*.png")
    "dropped: $dropped ($($files.Name -join ', ')); stack after: $(if (Stack $g) { 'shown' } else { 'gone' })"
    if ($files) { Check cmp -a $files[0].FullName -b "$third\image.png" }
} finally {
    (New-Object -ComObject Shell.Application).Windows() | Where-Object { $_.LocationURL -like '*dropped' } | ForEach-Object { $_.Quit() }
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
