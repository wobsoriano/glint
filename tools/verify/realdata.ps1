# Real-data compatibility: the Go build on a copy of a Library, settings.json and presets.json
# that the Rust build wrote. It opens the Library, every screenshot in the editor and every
# recording in Studio (playing it), closes each without a change, quits from the tray, and
# lists every file the session created, deleted or rewrote. Run inside the desktop lock:
#   $env:GLINT_BIN = <dir with glint.exe and glintcheck.exe>
#   go run ./tools/desklock -- powershell -NoProfile -File tools/verify/realdata.ps1 -Data <copy> -Out <evidence>
param(
    [Parameter(Mandatory)] [string] $Data,
    [Parameter(Mandatory)] [string] $Out,
    [int] $IconX = 1642, [int] $IconY = 955,
    [switch] $LibraryOnly
)
$env:GLINT_OUT = $Out
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
New-Item -ItemType Directory -Force $Out | Out-Null
$Ed = 'Glint ' + [char]0x2014 + ' '

function Snapshot([string]$root) {
    Get-ChildItem $root -Recurse -File -Force | ForEach-Object {
        [pscustomobject]@{
            Path = $_.FullName.Substring($root.Length + 1); Length = $_.Length
            Hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash; Written = $_.LastWriteTimeUtc.ToString('o')
        }
    }
}

$before = @(Snapshot $Data)
$before | Export-Csv -NoTypeInformation "$Out\files-before.csv"
$env:GLINT_DATA_DIR = $Data
$g = Start-Process "$Bin\glint.exe" -PassThru -NoNewWindow -RedirectStandardError "$Out\glint.err"
$items = @(Get-ChildItem "$Data\Library" -Directory | Where-Object { -not $_.Name.StartsWith('.') } | Sort-Object Name)
try {
    Step 'a plain launch opens the Library'
    $ok = Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 8000
    Start-Sleep -Milliseconds 1500
    Place 'Glint Library' 0 0 1920 1032 | Out-Null
    Front 'Glint Library'
    Start-Sleep -Milliseconds 1500
    "Library shows: $ok; items on disk: $($items.Count)"
    Shot "$Out\01-library.png"
    Glide 300 300 200
    Wheel -1200
    Start-Sleep -Milliseconds 800
    Shot "$Out\01b-library-scrolled.png"
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 600

    if (-not $LibraryOnly) {
        $n = 2
        foreach ($it in $items) {
            $m = Meta $it.FullName
            if ($m.kind -eq 'screenshot') {
                Step "editor: $($it.Name) '$($m.title)'"
                Start-Process "$Bin\glint.exe" -ArgumentList "`"$($it.FullName)\image.png`"" -NoNewWindow -Wait
                $shown = Wait-Until { (Win "$Ed$($m.title)") -ne [IntPtr]::Zero } 8000
                Start-Sleep -Milliseconds 1500
                Place "$Ed$($m.title)" 80 40 1360 880 | Out-Null
                Keys 'Shift'
                Front "$Ed$($m.title)"
                Start-Sleep -Milliseconds 1200
                "editor shows: $shown; edit.json: $(Test-Path "$($it.FullName)\edit.json")"
                Shot ("$Out\{0:d2}-editor-{1}.png" -f $n, $it.Name)
                Close-Win "$Ed$($m.title)"
                $closed = Wait-Until { (Win "$Ed$($m.title)") -eq [IntPtr]::Zero } 3000
                "closed without a prompt: $closed"
                if (-not $closed) { Shot ("$Out\{0:d2}-editor-close-prompt-{1}.png" -f $n, $it.Name); Keys 'Esc' }
            } else {
                Step "studio: $($it.Name) '$($m.title)'"
                $title = "$($m.title) - Glint Studio"
                Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$($it.FullName)`"" -NoNewWindow -Wait
                $shown = Wait-Until { (Win $title) -ne [IntPtr]::Zero } 10000
                Start-Sleep -Milliseconds 2500
                Place $title 0 0 1440 900 | Out-Null
                Keys 'Shift'
                Front $title
                Start-Sleep -Milliseconds 1200
                "Studio shows: $shown"
                Shot ("$Out\{0:d2}-studio-{1}.png" -f $n, $it.Name)
                Glide 700 450 200
                Keys 'Space'
                Start-Sleep -Milliseconds 1200
                Check shot -out ("$Out\{0:d2}-studio-play-a.png" -f $n) -rect '40,60,1000,560'
                Start-Sleep -Milliseconds 700
                Check shot -out ("$Out\{0:d2}-studio-play-b.png" -f $n) -rect '40,60,1000,560'
                Step 'frames advance while playing'
                Check cmp -a ("$Out\{0:d2}-studio-play-a.png" -f $n) -b ("$Out\{0:d2}-studio-play-b.png" -f $n) -differ
                Keys 'Space'
                Start-Sleep -Milliseconds 300
                Shot ("$Out\{0:d2}-studio-paused-{1}.png" -f $n, $it.Name)
                Close-Win $title
                $closed = Wait-Until { (Win $title) -eq [IntPtr]::Zero } 5000
                "closed: $closed"
                if (-not $closed) { Shot ("$Out\{0:d2}-studio-close-{1}.png" -f $n, $it.Name) }
            }
            $n++
        }
    }

    Step 'quit from the tray'
    Choose 12
    $exited = Wait-Until { $g.HasExited } 8000
    "glint exited: $exited"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
}

$after = @(Snapshot $Data)
$after | Export-Csv -NoTypeInformation "$Out\files-after.csv"
Step 'files the session created, deleted or rewrote'
$was = @{}; foreach ($f in $before) { $was[$f.Path] = $f }
$now = @{}; foreach ($f in $after) { $now[$f.Path] = $f }
foreach ($f in $after) {
    $b = $was[$f.Path]
    if (-not $b) { "  created  $($f.Path) ($($f.Length) bytes)" }
    elseif ($b.Hash -ne $f.Hash) { "  changed  $($f.Path) ($($b.Length) -> $($f.Length) bytes)" }
    elseif ($b.Written -ne $f.Written) { "  rewritten with the same bytes  $($f.Path)" }
}
foreach ($f in $before) { if (-not $now[$f.Path]) { "  deleted  $($f.Path)" } }
"glint.err:"
Get-Content "$Out\glint.err"
