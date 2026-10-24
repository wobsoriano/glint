# Builds the demos of the Quick Access stack, pins and Quick Look, and runs each with -auto inside
# the desktop lock, its Library in a new GLINT_DATA_DIR under %TEMP%. Each prints PASS or FAIL
# per check; logs and screenshots go to -Out. The exit code is the number of demos that failed.
#   powershell -File internal/ui/stack/cmd/run-demos.ps1
#   powershell -File internal/ui/stack/cmd/run-demos.ps1 -Demos pin
param(
    [string]$Out = (Join-Path $env:TEMP 'glint-surface-evidence'),
    [string[]]$Demos = @('stack', 'pin', 'quicklook')
)
$root = Resolve-Path "$PSScriptRoot\..\..\..\.."
$bin = Join-Path $env:TEMP 'glint-surface-demos'
New-Item -ItemType Directory -Force $bin, $Out | Out-Null
go -C $root build -o "$bin\desklock.exe" ./tools/desklock
if (-not $?) { exit 1 }
$pkgs = @{
    stack     = './internal/ui/stack/cmd/stackdemo'
    pin       = './internal/ui/pin/cmd/pindemo'
    quicklook = './internal/ui/quicklook/cmd/quicklookdemo'
}
$failed = 0
foreach ($d in $Demos) {
    go -C $root build -o "$bin\$d.exe" $pkgs[$d]
    if (-not $?) { $failed++; continue }
    $env:GLINT_DATA_DIR = Join-Path $bin ("data-$d-" + (Get-Random))
    $log = Join-Path $Out "$d.log"
    & "$bin\desklock.exe" -- "$bin\$d.exe" -auto -out (Join-Path $Out $d) 2>&1 | Out-File -Encoding utf8 $log
    if ($LASTEXITCODE -ne 0) { $failed++ }
    Get-Content $log
    Remove-Item -Recurse -Force $env:GLINT_DATA_DIR -ErrorAction SilentlyContinue
}
exit $failed
