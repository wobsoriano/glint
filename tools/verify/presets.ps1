# Editor presets: Save look writes presets.json, Export writes a .glintpreset file, Import
# reads it back, and a fresh editor lists the saved preset. The editor is placed at 80,40
# 1360x880 at 100%; the Presets card's buttons sit at y 247 with no preset and y 235 with one.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-presets' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $true; pin = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$Ed = 'Glint ' + [char]0x2014 + ' '
$out = "$dir\exported.glintpreset"
function Presets { if (Test-Path "$dir\data\presets.json") { @(Get-Content "$dir\data\presets.json" | ConvertFrom-Json) } else { @() } }
function Dialog([string]$title, [string]$path) {
    if (-not (Wait-Until { (Win $title) -ne [IntPtr]::Zero } 5000)) { throw "no '$title' dialog" }
    Start-Sleep -Milliseconds 800
    Keys 'Alt+N'
    Keys 'Ctrl+A'
    Start-Sleep -Milliseconds 200
    TypeText $path 15
    Keys 'Enter'
    Start-Sleep -Milliseconds 1200
    if ((Win $title) -ne [IntPtr]::Zero) { Shot "$dir\dialog-stuck.png"; Keys 'Esc'; throw "'$title' stayed open" }
}
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

    Step 'Save look'
    Click 1196 247 200
    Start-Sleep -Milliseconds 700
    Check shot -out "$dir\01-save-look.png" -rect '1190,130,330,300' | Out-Null
    "presets.json: $((Presets).Count) preset(s): $((Presets | ForEach-Object { $_.name }) -join ', ')"

    Step 'Export writes a .glintpreset file'
    Click 1364 235 200
    Dialog 'Export presets' $out
    "exported: $(Test-Path $out)"
    if (Test-Path $out) { (Get-Content -Raw $out).Substring(0, [Math]::Min(300, (Get-Content -Raw $out).Length)) }

    Step 'Import reads it back'
    Click 1287 235 200
    Dialog 'Import presets' $out
    Start-Sleep -Milliseconds 600
    Check shot -out "$dir\02-imported.png" -rect '1190,130,330,300' | Out-Null
    "presets.json after import: $((Presets).Count) preset(s): $((Presets | ForEach-Object { $_.name }) -join ', ')"
    Close-Win "$Ed$title"
    Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 3000 | Out-Null
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
