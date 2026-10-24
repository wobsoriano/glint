# Builds the spike and runs one self-driving prototype, printing its log.
#   powershell -File cmd/spike/run.ps1 overlay -variant fullscreen
param([Parameter(Mandatory)][string]$Mode, [Parameter(ValueFromRemainingArguments)][string[]]$Rest)
$root = Resolve-Path "$PSScriptRoot\..\.."
$exe = Join-Path $env:TEMP 'glint-spike.exe'
go -C $root build -o $exe ./cmd/spike
if (-not $?) { exit 1 }
$log = Join-Path $env:TEMP ("glint-spike-$Mode-" + ($Rest -join "").Replace("-", "") + ".log")
$args = @('-mode', $Mode, '-auto', '-out', "$root\cmd\spike\evidence") + @($Rest | Where-Object { $_ })
$p = Start-Process -FilePath $exe -ArgumentList $args -NoNewWindow -PassThru -WorkingDirectory $root -RedirectStandardOutput $log -RedirectStandardError "$log.err"
if (-not $p.WaitForExit(120000)) { $p.Kill(); Write-Output 'TIMEOUT' }
Get-Content $log
Get-Content "$log.err" | Select-Object -Last 15
exit $p.ExitCode
