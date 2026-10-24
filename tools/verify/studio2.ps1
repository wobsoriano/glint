# Studio, after studio.ps1 on the same copy: a background swatch, 16:9, a clip's speed menu,
# an export timed twice (the second is the cached render), and an export cancelled with Esc.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = Join-Path $env:GLINT_OUT 'x-studio'
$env:GLINT_DATA_DIR = "$dir\data"
$rec = (Get-ChildItem "$dir\data\Library" -Directory)[0].FullName
$win = "$((Meta $rec).title) - Glint Studio"
$g = Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -PassThru -NoNewWindow -RedirectStandardError "$dir\glint2.err"
function Exports { @(Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue) }
function Export-Once([string]$label) {
    $n = (Exports).Count
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Keys 'Ctrl+E'
    Start-Sleep -Milliseconds 700
    Shot "$dir\20-export-dialog-$label.png"
    Keys 'Enter'
    $ok = Wait-Until { (Exports).Count -gt $n } 180000
    "$label export: $ok after $($sw.ElapsedMilliseconds) ms; renders: $(@(Get-ChildItem "$rec\render" -Filter 'export-*').Name -join ', ')"
}
try {
    $ok = Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000
    if (-not $ok) { throw "Studio did not open: $win" }
    Start-Sleep -Milliseconds 2000
    Place $win 0 0 1440 900 | Out-Null
    Keys 'Shift'
    Front $win
    Start-Sleep -Milliseconds 800

    Step 'Background pane: the orange swatch'
    Click 1144 72 150
    Click 1205 263 200
    Start-Sleep -Milliseconds 700
    Shot "$dir\10-orange.png"

    Step 'Layout pane: 16:9'
    Click 1176 72 150
    Click 1232 195 200
    Start-Sleep -Milliseconds 700
    Shot "$dir\11-16x9.png"

    Step 'select the first clip: its speed menu'
    Click 300 815 200
    Start-Sleep -Milliseconds 300
    Click 324 742 200
    Start-Sleep -Milliseconds 500
    Shot "$dir\12-speed-menu.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300

    Step 'export twice: the second is the cached render'
    Export-Once 'first'
    Start-Sleep -Milliseconds 1500
    Export-Once 'second'

    Step 'a change, then an export cancelled with Esc'
    Click 1144 72 150
    Click 1243 263 200
    Start-Sleep -Milliseconds 500
    $n = (Exports).Count
    Keys 'Ctrl+E'; Start-Sleep -Milliseconds 700; Keys 'Enter'
    Start-Sleep -Milliseconds 800
    Shot "$dir\21-exporting.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\22-cancelled.png"
    "exports $n -> $((Exports).Count); render folder: $(@(Get-ChildItem "$rec\render").Name -join ', ')"

    Close-Win $win
    Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000 | Out-Null
    $p = Get-Content "$rec\project.json" | ConvertFrom-Json
    "saved project: aspect $($p.aspect); background $($p.design.background | ConvertTo-Json -Compress -Depth 4); clips $(($p.clips | ForEach-Object { "$($_.start_ms)-$($_.end_ms)x$($_.speed)" }) -join ' '); export $($p.export | ConvertTo-Json -Compress)"
    Exports | ForEach-Object { Check probe -file $_.FullName }
} finally {
    Glide 1500 880 200
    Stop-Glint $g
}
