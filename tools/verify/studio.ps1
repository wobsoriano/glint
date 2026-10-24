# Recording Studio on a copy of a Rust-written recording: every sidebar pane, split at the
# playhead, delete a clip, undo, add a zoom, change the background, then close and read the
# saved project.json; reopen and check it kept the edits. Studio is placed at 0,0 1440x900.
param([Parameter(Mandatory)] [string] $Session)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-studio' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
New-Item -ItemType Directory -Force "$dir\data\Library" | Out-Null
Copy-Item -Recurse $Session "$dir\data\Library\$(Split-Path -Leaf $Session)"
$rec = (Get-ChildItem "$dir\data\Library" -Directory)[0].FullName
$win = "$((Meta $rec).title) - Glint Studio"
$g = Start-Glint $dir
function Project { Get-Content "$rec\project.json" -ErrorAction SilentlyContinue | ConvertFrom-Json }
function Summary {
    $p = Project
    if (-not $p) { return 'no project.json' }
    "clips: $(($p.clips | ForEach-Object { "$($_.start_ms)-$($_.end_ms)x$($_.speed)" }) -join ' '); zooms: $(@($p.zooms).Count); aspect: $($p.aspect); background: $($p.design.background | ConvertTo-Json -Compress -Depth 4)"
}
function Open-Studio {
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    $ok = Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000
    Start-Sleep -Milliseconds 2000
    Place $win 0 0 1440 900 | Out-Null
    Keys 'Shift'
    Front $win
    Start-Sleep -Milliseconds 800
    if (-not $ok) { throw "Studio did not open: $win" }
    "Studio shows: $ok"
}
function Home { [Promo.In]::keybd_event(0x24, 0, 0, [UIntPtr]::Zero); [Promo.In]::keybd_event(0x24, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 120 }
try {
    Open-Studio
    Step 'every sidebar pane'
    $i = 0
    foreach ($x in 1144, 1176, 1208, 1240, 1272, 1304, 1336, 1368, 1400) {
        Click $x 72 150
        Start-Sleep -Milliseconds 400
        Check shot -out ("$dir\01-pane-{0}.png" -f $i) -rect '1120,40,320,860' | Out-Null
        $i++
    }
    Click 1144 72 150

    Step 'split at 2 s, delete the second clip, undo the delete, add a zoom at 1 s'
    Glide 600 400 150
    Home; Keys 'Shift+Right'; Keys 'Shift+Right'
    Keys 'S'
    Start-Sleep -Milliseconds 400
    Shot "$dir\02-split.png"
    Click 900 815 200
    Start-Sleep -Milliseconds 300
    Keys 'Del'
    Start-Sleep -Milliseconds 400
    Shot "$dir\03-deleted.png"
    Keys 'Ctrl+Z'
    Start-Sleep -Milliseconds 400
    Shot "$dir\04-undone.png"
    Home; Keys 'Shift+Right'
    Keys 'Z'
    Start-Sleep -Milliseconds 400
    Shot "$dir\05-zoom.png"

    Step 'a different background swatch'
    Click 1205 263 200
    Start-Sleep -Milliseconds 600
    Shot "$dir\06-background.png"

    Step 'close saves the project'
    Close-Win $win
    $closed = Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000
    "closed: $closed"
    Summary

    Step 'reopen keeps the edits'
    Open-Studio
    Shot "$dir\07-reopened.png"
    Close-Win $win
    Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000 | Out-Null
    Summary
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
