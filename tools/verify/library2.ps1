# The Library's actions on a selection, after library.ps1 in the same evidence folder: Show in
# Explorer, Export into a chosen folder, Move to Recycle Bin from the button (confirmed) and
# from the Delete key (cancelled with Esc). The Library is placed at 0,0 1920x1032 at 100%.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = Join-Path $env:GLINT_OUT 'x-library'
$env:GLINT_DATA_DIR = "$dir\data"
$g = Start-Process "$Bin\glint.exe" -ArgumentList '--library' -PassThru -NoNewWindow -RedirectStandardError "$dir\glint2.err"
$tiles = @(@(318, 125), @(520, 125), @(722, 125), @(924, 125), @(1126, 125), @(1328, 125), @(1530, 125), @(318, 289), @(520, 289), @(727, 289))
function CtrlClick([int]$x, [int]$y) { [Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero); Click $x $y 200; [Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 200 }
$dest = Join-Path $dir 'exported'
New-Item -ItemType Directory -Force $dest | Out-Null
try {
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 0 0 1920 1032 | Out-Null
    Keys 'Shift'
    Front 'Glint Library'
    Start-Sleep -Milliseconds 1000

    Step 'two selected: Show in Explorer opens the Library folder'
    Click $tiles[0][0] $tiles[0][1] 200
    CtrlClick $tiles[1][0] $tiles[1][1]
    Click 1736 951 200
    $ok = Wait-Until { (Win 'Library') -ne [IntPtr]::Zero } 6000
    Start-Sleep -Milliseconds 1200
    "an Explorer window titled Library shows: $ok"
    Shot "$dir\20-show-in-explorer.png"
    (New-Object -ComObject Shell.Application).Windows() | Where-Object { $_.LocationURL -like '*x-library/data/Library' } | ForEach-Object {
        "  explorer at $($_.LocationURL); selected: $(@($_.Document.SelectedItems() | ForEach-Object { $_.Name }) -join ', ')"
        $_.Quit()
    }
    Start-Sleep -Milliseconds 500

    Step 'two selected: Export writes both into the chosen folder'
    Front 'Glint Library'
    Start-Sleep -Milliseconds 300
    Click 1800 910 200
    $ok = Wait-Until { (Win 'Export Here') -ne [IntPtr]::Zero } 5000
    Start-Sleep -Milliseconds 1000
    "folder dialog shows: $ok"
    Shot "$dir\21-export-dialog.png"
    TypeText $dest 15
    Keys 'Enter'
    Start-Sleep -Milliseconds 1000
    if ((Win 'Export Here') -ne [IntPtr]::Zero) { Keys 'Enter'; Start-Sleep -Milliseconds 1000 }
    Wait-Until { @(Get-ChildItem $dest -File).Count -ge 2 } 6000 | Out-Null
    Shot "$dir\22-exported.png"
    "exported: $((Get-ChildItem $dest -File).Name -join ', ')"

    Step 'Move to Recycle Bin asks, and Enter confirms'
    $victim = (Get-ChildItem "$dir\data\Library" -Directory | Where-Object Name -eq '2026-10-17_09-49-26_37d6').FullName
    Front 'Glint Library'
    Click $tiles[8][0] $tiles[8][1] 200
    Click 1746 991 200
    Start-Sleep -Milliseconds 900
    Shot "$dir\23-trash-ask.png"
    Check wins -pid $g.Id
    Keys 'Enter'
    $gone = Wait-Until { -not (Test-Path $victim) } 5000
    Start-Sleep -Milliseconds 500
    Shot "$dir\24-trashed.png"
    "the capture left the Library folder: $gone"
    $bin = (New-Object -ComObject Shell.Application).Namespace(10)
    "in the Recycle Bin: $(@($bin.Items() | Where-Object { $_.Name -eq '2026-10-17_09-49-26_37d6' }).Count)"

    Step 'Delete asks too, and Esc keeps the capture'
    $keep = (Get-ChildItem "$dir\data\Library" -Directory | Where-Object Name -eq '2026-10-17_14-05-00_8c62').FullName
    Front 'Glint Library'
    Click $tiles[7][0] $tiles[7][1] 200
    Keys 'Del'
    Start-Sleep -Milliseconds 900
    Shot "$dir\25-delete-ask.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 800
    "the capture is still there after Esc: $(Test-Path $keep)"
    Shot "$dir\26-delete-cancelled.png"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint2.err"
}
