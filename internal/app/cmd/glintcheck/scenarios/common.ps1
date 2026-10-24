# Shared by the scenarios. Run one inside the desktop lock:
#   $env:GLINT_BIN = <dir with glint.exe and glintcheck.exe>; $env:GLINT_OUT = <evidence dir>
#   go run ./tools/desklock -- powershell -NoProfile -File internal/app/cmd/glintcheck/scenarios/area.ps1
# GLINT_DRIVE names another copy of the driver helpers than tools/drive/drive.ps1.
$drive = if ($env:GLINT_DRIVE) { $env:GLINT_DRIVE } else { Join-Path $PSScriptRoot '..\..\..\..\..\tools\drive\drive.ps1' }
. $drive
$Bin = $env:GLINT_BIN
$Check = Join-Path $Bin 'glintcheck.exe'

# New-Run makes an evidence folder with a data directory holding settings.json. Glint's own
# windows are included in captures unless the settings say otherwise, so screenshots show them.
function New-Run([string]$name, [hashtable]$settings = @{}) {
    $dir = Join-Path $env:GLINT_OUT $name
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force "$dir\data" | Out-Null
    $s = @{ include_own_windows = $true; save_dir = "$dir\exports" }
    foreach ($k in $settings.Keys) { $s[$k] = $settings[$k] }
    $s | ConvertTo-Json -Depth 5 | Set-Content -Encoding ascii "$dir\data\settings.json"
    $dir
}

function Start-Glint([string]$dir) {
    $env:GLINT_DATA_DIR = "$dir\data"
    $p = Start-Process "$Bin\glint.exe" -ArgumentList '--background' -PassThru -NoNewWindow -RedirectStandardError "$dir\glint.err"
    Start-Sleep -Milliseconds 1500
    $p
}

function Stop-Glint($p) {
    if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id -Force }
}

# The stage is a topmost window of known noise and text, so captures land on pixels that do
# not change between the reference screenshot and Glint's.
function Start-Stage {
    $p = Start-Process $Check -ArgumentList 'stage' -PassThru -NoNewWindow
    Start-Sleep -Milliseconds 1500
    $p
}

function Check {
    & $Check @args
    "  -> exit $LASTEXITCODE"
}

function Items([string]$dir) {
    @(Get-ChildItem "$dir\data\Library" -Directory -ErrorAction SilentlyContinue | Where-Object { -not $_.Name.StartsWith('.') } | Sort-Object Name)
}

function Step([string]$text) { "== $text" }

# Probe reports a PNG's size and the alpha of its top-left corner and centre.
function Probe([string]$png) {
    $b = [Drawing.Bitmap]::FromFile($png)
    "$(Split-Path -Leaf $png): $($b.Width)x$($b.Height), corner alpha $($b.GetPixel(0, 0).A), centre alpha $($b.GetPixel([int]($b.Width / 2), [int]($b.Height / 2)).A)"
    $b.Dispose()
}

function Meta([string]$item) { Get-Content "$item\meta.json" | ConvertFrom-Json }
