# Runs every self-driving prototype in turn and prints the checks; logs and screenshots land in
# cmd/spike/evidence. It moves the mouse and types for about two minutes.
#   powershell -File cmd/spike/run-all.ps1
$runs = @(
    @('overlay', '-gap', '800ms'),
    @('cards'),
    @('protect'),
    @('tray'),
    @('editor', '-variant', 'vector'),
    @('editor', '-variant', 'cpu'),
    @('editor', '-variant', 'canvas'),
    @('chrome'),
    @('video', '-fps', '30'),
    @('video', '-fps', '60'),
    @('toast'),
    @('outline')
)
$failed = 0
foreach ($run in $runs) {
    Write-Output "=== $($run -join ' ')"
    if ($run[0] -eq 'editor') { $env:MYGO_FRAME_STATS = 'all' } else { Remove-Item env:MYGO_FRAME_STATS -ErrorAction SilentlyContinue }
    $rest = @($run | Select-Object -Skip 1)
    & "$PSScriptRoot\run.ps1" $run[0] @rest | Select-String 'PASS|FAIL|TIMEOUT|panic|Show to|editor |video |toast centre|ink of|done:'
    if ($LASTEXITCODE -ne 0) { $failed++ }
}
Remove-Item env:MYGO_FRAME_STATS -ErrorAction SilentlyContinue
Write-Output "runs failed: $failed"
exit $failed
