# Handles of a Go process with no MyGo that serves AF_UNIX connections as the single-instance
# lock does, over -N dials from other processes. It needs no desktop.
param([int]$N = 60)
. "$PSScriptRoot\common.ps1"
$sock = Join-Path $env:TEMP "glintcheck-$PID.sock"
$out = Join-Path $env:TEMP "glintcheck-$PID.txt"
$p = Start-Process $Check -ArgumentList 'sockserve', '-sock', "`"$sock`"" -PassThru -WindowStyle Hidden -RedirectStandardOutput $out
Wait-Until { Test-Path $sock } 5000 | Out-Null
function Count([string]$label) {
    $l = & $Check handles -pid $p.Id
    $p.Refresh()
    "{0,-16} handles {1,4}  thread handles {2,3}  events {3,3}  threads {4,3}" -f $label, $p.HandleCount,
        @($l | Where-Object { $_ -like 'handle Thread*' }).Count, @($l | Where-Object { $_ -like 'handle Event*' }).Count, $p.Threads.Count
}
try {
    Count 'serving'
    for ($i = 1; $i -le $N; $i++) {
        Start-Process $Check -ArgumentList 'sockdial', '-sock', "`"$sock`"" -Wait -WindowStyle Hidden
        if ($i -in 1, 2, 5, 10, 20, 40, 60, $N) { Count "after $i dials" }
    }
    & $Check handles -pid $p.Id | Where-Object { $_ -like 'handle Thread*' } | Group-Object | ForEach-Object { "{0} x {1}" -f $_.Count, $_.Name }
} finally {
    Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    Remove-Item $sock, $out -ErrorAction SilentlyContinue
}
