# Recording Studio through the running app, from every place that opens it: after a recording
# with Settings > After recording > Open Studio, a card's Edit, the Library's Edit, Quick Look's
# Open in Studio, the tray's Edit Last Capture and glint.exe --studio <session>. One window
# opens per session. An export from Studio retakes the card's and the Library's thumbnail, and
# the card's Copy then hands out that same render as a file. Coordinates are for the Library
# placed at 80,60 1180x760 and Studio at 0,0 1440x900, at 100%.
param([int]$IconX = 1642, [int]$IconY = 955, [int]$Seconds = 3)
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'studio' @{
    after_recording = @{ preview = $true; copy = $false; save = $false; studio = $true }
    record          = @{ target = 'Display'; system_audio = $false; countdown = 0 }
}
$g = Start-Glint $dir
$stage = Start-Stage
function Recordings { @(Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue | Sort-Object Name) }
function Picker { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint recording"') }
function Studios { @(& $Check wins -pid $g.Id | Select-String 'visible=true.* - Glint Studio"') }
# Open-Studio waits for the Studio window of the recording titled $title and places it.
function Open-Studio([string]$title, [string]$shot) {
    $ok = Wait-Until { (Win "$title - Glint Studio") -ne [IntPtr]::Zero } 8000
    Start-Sleep -Milliseconds 1500
    Place "$title - Glint Studio" 0 0 1440 900 | Out-Null
    Front "$title - Glint Studio"
    Start-Sleep -Milliseconds 700
    if ($shot) { Shot "$dir\$shot" }
    "Studio for '$title' shows: $ok; Studio windows: $((Studios).Count)"
}
function Close-Studio([string]$title) {
    Close-Win "$title - Glint Studio"
    $gone = Wait-Until { (Win "$title - Glint Studio") -eq [IntPtr]::Zero } 4000
    "Studio closed: $gone"
}
function CardShot([string]$name) {
    $c = Card (Stack $g)
    Glide 1500 880 150
    Start-Sleep -Milliseconds 400
    Check shot -out "$dir\$name" -rect "$($c.X),$($c.Y),$($c.W),$($c.H)" | Out-Null
}
function Open-Library {
    Keys 'Ctrl+Shift+L'
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Front 'Glint Library'
    Start-Sleep -Milliseconds 800
}
function TileShot([string]$name) {
    Glide 1500 880 150
    Start-Sleep -Milliseconds 400
    Check shot -out "$dir\$name" -rect '300,120,220,140' | Out-Null
}

try {
    Hide-Stage
    Step "Ctrl+Shift+7, Enter, $Seconds s, Ctrl+Shift+7 records, and After recording > Open Studio opens it"
    Keys 'Ctrl+Shift+7'
    Wait-Until { Picker } 3000 | Out-Null
    Start-Sleep -Milliseconds 500
    Keys 'Enter'
    Start-Sleep -Seconds $Seconds
    Keys 'Ctrl+Shift+7'
    $done = Wait-Until { (Recordings).Count -ge 1 } 15000
    $rec = (Recordings)[-1].FullName
    $title = (Meta $rec).title
    "recording in the Library: $done ($(Split-Path -Leaf $rec)), title '$title'"
    Open-Studio $title '01-after-recording.png'
    CardShot '01-card-before.png'
    Close-Studio $title

    Step "the card's Edit opens Studio, and a second Edit focuses the same window"
    $card = Card (Stack $g)
    Press $card $card.Edit
    Open-Studio $title '02-from-card.png'
    Press $card $card.Edit
    Start-Sleep -Milliseconds 1000
    "Studio windows after a second Edit: $((Studios).Count); foreground: $((& $Check wins -title 'Glint Studio' | Select-Object -First 1))"
    Close-Studio $title

    Step "the Library's Edit opens the selected recording in Studio"
    Open-Library
    TileShot '03-tile-before.png'
    Click 410 185 200
    Start-Sleep -Milliseconds 400
    Shot "$dir\03-library-selected.png"
    Click 1035 658 200
    Open-Studio $title '03-from-library.png'
    Close-Studio $title

    Step "Quick Look's Open in Studio opens it"
    Front 'Glint Library'
    DClick 410 185 300
    Wait-Until { (Win $title) -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 900
    $q = WinRect $title
    Shot "$dir\04-quicklook.png"
    Click ($q.X + $q.W - 80) ($q.Y + $q.H - 33) 200
    Open-Studio $title '04-from-quicklook.png'
    $pattern = 'visible=true.*"' + [regex]::Escape($title) + '"'
    $lookShows = [bool](& $Check wins -pid $g.Id | Select-String $pattern)
    "Quick Look closed: $(-not $lookShows)"
    Close-Studio $title
    if ((Win $title) -ne [IntPtr]::Zero) { Front $title; Keys 'Esc' }
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 400

    Step "the tray's Edit Last Capture opens the recording, the newest capture"
    Choose 10
    Open-Studio $title '05-from-tray.png'
    Close-Studio $title

    Step 'glint.exe --studio <session> opens it, and again focuses the same window'
    $q = Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -PassThru -NoNewWindow -Wait
    Open-Studio $title '06-from-command-line.png'
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    Start-Sleep -Milliseconds 1200
    "second instance exited $($q.ExitCode); Studio windows after a second --studio: $((Studios).Count)"

    Step 'Export (Ctrl+E, Enter) saves an MP4, and the card and the Library show its first frame'
    Front "$title - Glint Studio"
    Start-Sleep -Milliseconds 300
    $thumbBefore = (Get-FileHash "$rec\thumb.png").Hash
    Keys 'Ctrl+E'
    Start-Sleep -Milliseconds 700
    Shot "$dir\07-export-dialog.png"
    Keys 'Enter'
    $exported = Wait-Until { Test-Path "$dir\exports\*.mp4" } 180000
    Wait-Until { (Get-FileHash "$rec\thumb.png").Hash -ne $thumbBefore } 10000 | Out-Null
    Start-Sleep -Milliseconds 800
    Shot "$dir\07-exported.png"
    $out = Get-ChildItem "$dir\exports\*.mp4" | Select-Object -First 1
    "exported: $exported ($($out.Name), $($out.Length) bytes); thumb.png rewritten: $((Get-FileHash "$rec\thumb.png").Hash -ne $thumbBefore)"
    Check probe -file $out.FullName
    Meta $rec | ConvertTo-Json -Compress
    Close-Studio $title
    CardShot '07-card-after.png'
    Check cmp -a "$dir\01-card-before.png" -b "$dir\07-card-after.png" -differ
    Open-Library
    TileShot '07-tile-after.png'
    Check cmp -a "$dir\03-tile-before.png" -b "$dir\07-tile-after.png" -differ
    Shot "$dir\07-library-after.png"
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 400

    Step "the card's Copy hands out the exported render as a file"
    Set-Clipboard -Value 'glintcheck: before the copy'
    $renders = @(Get-ChildItem "$rec\render\export-*.mp4")
    $card = Card (Stack $g)
    Press $card $card.Copy
    Wait-Until { (& $Check clip) -match '^file: .*\.mp4' } 30000 | Out-Null
    $clip = & $Check clip
    $clip
    $file = ($clip | Select-String '^file: (.*)$').Matches | ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
    $after = @(Get-ChildItem "$rec\render\export-*.mp4")
    "renders before the copy: $($renders.Name -join ', '); after: $($after.Name -join ', ')"
    "the copied file is the exported MP4: $($file -and (Get-FileHash $file).Hash -eq (Get-FileHash $out.FullName).Hash)"
    Shot "$dir\08-copied.png"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
