# Drives editordemo on the real desktop. Build it, then run each session inside the desktop lock:
#   go build -o $env:TEMP\editordemo\editordemo.exe ./internal/ui/editor/cmd/editordemo
#   go run ./tools/desklock -- powershell -NoProfile -ExecutionPolicy Bypass -File internal\ui\editor\cmd\editordemo\drive\s4.ps1
# Output (data folders, screenshots, frame logs) goes to EDITORDEMO_OUT, %TEMP%\editordemo by default.
# Coordinates assume one 1920x1080 display at 100% with the window at 556,80. s5 and s10 reopen
# the capture s4 made, s7 to s9 the one s6 made, and s12 the one s11 made.
. "C:\Users\wobsoriano\Documents\dev\glint\promo\real\lib.ps1"
$S = if ($env:EDITORDEMO_OUT) { $env:EDITORDEMO_OUT } else { "$env:TEMP\editordemo" }
New-Item -ItemType Directory -Force $S | Out-Null
$Demo = "$S\editordemo.exe"
$WinX = 556
$WinY = 80

function Start-Demo([string]$name, [string[]]$demoArgs) {
    $out = "$S\$name.out.txt"
    Remove-Item $out -ErrorAction SilentlyContinue
    $p = Start-Process $Demo -ArgumentList ($demoArgs + @("-x", "$WinX", "-y", "$WinY")) -RedirectStandardOutput $out -RedirectStandardError "$S\$name.err.txt" -PassThru
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt 15000) {
        if ((Test-Path $out) -and (Select-String -Path $out -Pattern '^opened' -Quiet)) { break }
        Start-Sleep -m 100
    }
    Start-Sleep -m 300
    $a = @{}
    foreach ($l in Get-Content $out) {
        if ($l -match '^anchor (\S+) (-?\d+) (-?\d+)') { $a[$Matches[1]] = @([int]$Matches[2], [int]$Matches[3]) }
    }
    @{ Proc = $p; A = $a; Out = $out }
}

function Stop-Demo($d) {
    if (-not $d.Proc.WaitForExit(8000)) { Stop-Process -Id $d.Proc.Id -Force -ErrorAction SilentlyContinue }
}

function WinShot([string]$name) { Shot "$S\$name.png" }

function WaitWin([string]$title, [int]$ms = 8000) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt $ms) { $h = Win $title; if ($h -ne [IntPtr]::Zero) { Start-Sleep -m 400; return $h }; Start-Sleep -m 100 }
    [IntPtr]::Zero
}

# Types a path into the file dialog titled $title once it shows, and confirms it.
function TypeInto([string]$title, [string]$text, [string]$log) {
    $h = WaitWin $title
    if ($h -eq [IntPtr]::Zero) { "no window $title" | Out-File -Append $log; return }
    [Promo.In]::SetForegroundWindow($h) | Out-Null; Start-Sleep -m 200
    Keys "Ctrl+A"; TypeText $text 25; Keys "Enter"
    Start-Sleep -m 1200
    "$title closed: $((Win $title) -eq [IntPtr]::Zero)" | Out-File -Append $log
}
