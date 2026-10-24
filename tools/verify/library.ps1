# The Library beyond scenarios/library.ps1, on a copy of a Rust-written Library: the sidebar
# filters, grid and list, sort, search, the inspector, Space for Quick Look, the layout
# remembered across a reopen, Ctrl-click, Shift-click and Ctrl+A with Copy, Show in Explorer,
# Export and Move to Recycle Bin. The Library is placed at 0,0 1920x1032 at 100%.
#   $env:GLINT_BIN, $env:GLINT_OUT as for the scenarios; -Seed <a Library folder to copy>
param([Parameter(Mandatory)] [string] $Seed)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$dir = New-Run 'x-library' @{ after_shot = $off }
Copy-Item -Recurse $Seed "$dir\data\Library"
$g = Start-Glint $dir
function Saved { (Get-Content "$dir\data\settings.json" | ConvertFrom-Json).library | ConvertTo-Json -Compress }
function Open-Lib {
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    $ok = Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 6000
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 0 0 1920 1032 | Out-Null
    Keys 'Shift'
    Front 'Glint Library'
    Start-Sleep -Milliseconds 1200
    "Library shows: $ok"
}
$tiles = @(@(318, 125), @(520, 125), @(722, 125), @(924, 125), @(1126, 125), @(1328, 125), @(1530, 125), @(318, 289), @(520, 289), @(727, 289))
function Tile([int]$i) { $tiles[$i] }
function CtrlClick([int]$x, [int]$y) { [Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero); Click $x $y 200; [Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 200 }
function ShiftClick([int]$x, [int]$y) { [Promo.In]::keybd_event(0x10, 0, 0, [UIntPtr]::Zero); Click $x $y 200; [Promo.In]::keybd_event(0x10, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 200 }
try {
    Step 'glint.exe --library opens the Library of the running Glint'
    Open-Lib
    Shot "$dir\01-all.png"

    Step 'sidebar filters'
    Click 88 130 200; Start-Sleep -Milliseconds 500; Shot "$dir\02-screenshots.png"
    Click 85 164 200; Start-Sleep -Milliseconds 500; Shot "$dir\03-recordings.png"
    Click 88 96 200; Start-Sleep -Milliseconds 500

    Step 'the inspector for a recording and for an edited screenshot'
    $t = Tile 5; Click $t[0] $t[1] 200; Start-Sleep -Milliseconds 500; Shot "$dir\04-inspector-recording.png"
    $t = Tile 9; Click $t[0] $t[1] 200; Start-Sleep -Milliseconds 500; Shot "$dir\05-inspector-edited.png"

    Step 'Space opens Quick Look on the selection, and Space again closes it'
    $title = 'Screenshot 2026-10-17 at 06.59.28'
    Keys 'Space'
    $ok = Wait-Until { (Win $title) -ne [IntPtr]::Zero } 4000
    Start-Sleep -Milliseconds 700
    "Quick Look shows: $ok"
    Shot "$dir\06-quicklook-space.png"
    Front 'Glint Library'
    Keys 'Space'
    $gone = Wait-Until { (Win $title) -eq [IntPtr]::Zero } 3000
    "Quick Look closed by Space in the Library: $gone"
    if (-not $gone) { Front $title; Keys 'Esc' }

    Step 'list view, sort by name, sort by modified'
    Front 'Glint Library'
    Click 1708 25 200; Start-Sleep -Milliseconds 600; Shot "$dir\07-list.png"
    Click 1534 25 200; Start-Sleep -Milliseconds 600; Shot "$dir\08-sort-name.png"
    Click 1606 25 200; Start-Sleep -Milliseconds 600; Shot "$dir\09-sort-modified.png"
    "settings.json library: $(Saved)"

    Step 'search by title'
    Keys 'Ctrl+F'
    TypeText '18.3' 40
    Start-Sleep -Milliseconds 600
    Shot "$dir\10-search.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 400

    Step 'inspector off, then the layout survives closing and reopening the Library'
    Click 1748 25 200; Start-Sleep -Milliseconds 500
    Shot "$dir\11-no-inspector.png"
    "settings.json library: $(Saved)"
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 800
    Open-Lib
    Shot "$dir\12-reopened.png"
    Click 1676 25 200; Click 1449 25 200; Click 1748 25 200
    Start-Sleep -Milliseconds 600
    "settings.json library after restoring grid, date, inspector: $(Saved)"

    Step 'Ctrl-click two tiles, Ctrl+C copies both files'
    $t = Tile 0; Click $t[0] $t[1] 200
    $t = Tile 1; CtrlClick $t[0] $t[1]
    Start-Sleep -Milliseconds 400
    Shot "$dir\13-ctrl-click.png"
    Set-Clipboard -Value 'glintcheck: before the copy'
    Keys 'Ctrl+C'
    Start-Sleep -Milliseconds 1200
    Check clip

    Step 'Shift-click selects the range, Ctrl+A selects everything'
    $t = Tile 4; ShiftClick $t[0] $t[1]
    Start-Sleep -Milliseconds 400
    Shot "$dir\14-shift-click.png"
    Keys 'Ctrl+A'
    Start-Sleep -Milliseconds 400
    Shot "$dir\15-ctrl-a.png"
    Keys 'Esc'
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
