# The Library window through the running app: Ctrl+Shift+L lists the captures, a capture
# taken while it is open shows up in it, a double click opens Quick Look, and a rename
# reaches meta.json. Coordinates are for the window placed at 80,60 at 100%, where the first
# grid tile's thumbnail is centred near 410,185.
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'library' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$tileX = 410; $tileY = 185
try {
    Step 'one capture, then Ctrl+Shift+L opens the Library listing it'
    Capture-Area 400 300 900 600
    Keys 'Ctrl+Shift+L'
    $ok = Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero }
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Hide-Stage
    Start-Sleep -Milliseconds 500
    "Library shows: $ok; items on disk: $((Items $dir).Count)"
    Shot "$dir\01-library.png"

    Step 'a capture taken while the Library is open shows up in it'
    Capture-Area 1300 200 1700 500
    Start-Sleep -Milliseconds 800
    "items on disk: $((Items $dir).Count)"
    Shot "$dir\02-library-updated.png"

    Step 'a double click on the newest tile opens Quick Look'
    $newest = (Items $dir)[-1].FullName
    $title = (Meta $newest).title
    DClick $tileX $tileY 300
    $ok = Wait-Until { (Win $title) -ne [IntPtr]::Zero }
    Start-Sleep -Milliseconds 600
    "Quick Look '$title' shows: $ok"
    Shot "$dir\03-quicklook.png"
    Front $title
    Keys 'Esc'
    Start-Sleep -Milliseconds 500
    "Quick Look closed by Esc: $((Win $title) -eq [IntPtr]::Zero)"

    Step 'Enter renames the selected capture, and meta.json keeps the name'
    Click $tileX $tileY 200
    Start-Sleep -Milliseconds 300
    Keys 'Enter'
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+A'
    TypeText 'Renamed by glintcheck' 20
    Keys 'Enter'
    Start-Sleep -Milliseconds 800
    "meta.json title: $((Meta $newest).title)"
    Shot "$dir\04-renamed.png"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
    Get-Content "$dir\glint.err"
}
