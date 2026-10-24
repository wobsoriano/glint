# README's command line against a running Glint: --capture screen, previous before and after
# an area capture, window, text, --trim <video>, and --library; plus a first launch with
# --capture screen, which must start Glint and capture.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
function Glint([string[]]$a) { $q = Start-Process "$Bin\glint.exe" -ArgumentList $a -PassThru -NoNewWindow -Wait; Start-Sleep -Milliseconds 1500; $q.ExitCode }
function Overlay { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint capture"') }
$stage = Start-Stage
$g = $null
try {
    $dir = New-Run 'x-cli' @{ after_shot = $off }
    $env:GLINT_DATA_DIR = "$dir\data"
    Step 'a first launch with --capture screen starts Glint and captures'
    $first = Start-Process "$Bin\glint.exe" -ArgumentList '--capture', 'screen' -PassThru -NoNewWindow -RedirectStandardError "$dir\glint.err"
    $g = $first
    Wait-Until { (Items $dir).Count -ge 1 } 6000 | Out-Null
    "items: $((Items $dir).Count); Glint still running in the tray: $(-not $first.HasExited)"
    Step '--capture previous with no previous area'
    $n = (Items $dir).Count
    "exit $(Glint '--capture', 'previous'); items $n -> $((Items $dir).Count); overlay open: $(Overlay)"
    if (Overlay) { Shot "$dir\01-previous-without-area.png"; Keys 'Esc'; Start-Sleep -Milliseconds 400 }
    Capture-Area 400 300 900 600
    $n = (Items $dir).Count
    "after an area capture, --capture previous: exit $(Glint '--capture', 'previous'); items $n -> $((Items $dir).Count)"
    Step '--capture window takes the foreground window'
    Click 700 136 200
    $n = (Items $dir).Count
    "exit $(Glint '--capture', 'window'); items $n -> $((Items $dir).Count); source: $((Meta (Items $dir)[-1].FullName).source)"
    Step '--capture text opens the text overlay'
    "exit $(Glint '--capture', 'text'); overlay open: $(Overlay)"
    Shot "$dir\02-text-overlay.png"
    Keys 'Esc'; Start-Sleep -Milliseconds 400
    Step '--trim <video> and --library'
    Check video -out "$dir\clip.mov" -seconds 2 | Out-Null
    $trimArgs = @('--trim', ('"' + "$dir\clip.mov" + '"'))
    $code = Glint $trimArgs
    "exit $code; Trim & Compress shows: $((Win 'Trim & Compress') -ne [IntPtr]::Zero)"
    Close-Win 'Trim & Compress'
    "exit $(Glint '--library'); Library shows: $((Win 'Glint Library') -ne [IntPtr]::Zero)"
    Close-Win 'Glint Library'
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
