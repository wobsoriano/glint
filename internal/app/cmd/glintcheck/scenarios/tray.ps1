# The tray menu: Capture Full Screen, Self-Timer with its countdown, Open Library and
# Settings (not built yet: a toast says so), a double click capturing an area, and Quit.
# The icon sits in the taskbar's overflow; IconX, IconY are where this machine puts it. Keys
# go to a menu only after glintcheck confirms the menu is Glint's.
param([int]$IconX = 1642, [int]$IconY = 955, [int]$Timer = 5)
. "$PSScriptRoot\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$dir = New-Run "tray-$Timer" @{ after_shot = $off; timer_seconds = $Timer }
$g = Start-Glint $dir

function Show-Overflow {
    & $Check mark -at "$IconX,$IconY" | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Click 1722 1056 300
        Start-Sleep -Milliseconds 700
        & $Check mark -at "$IconX,$IconY" | Out-Null
    }
    if ($LASTEXITCODE -ne 0) { throw 'the tray icon is not showing' }
}

function Open-Menu {
    Show-Overflow
    Glide $IconX $IconY 250
    Start-Sleep -Milliseconds 100
    RDown; Start-Sleep -Milliseconds 60; RUp
    Start-Sleep -Milliseconds 600
    & $Check menu -pid $g.Id | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'the tray menu did not open' }
}

# Choose picks the nth item of the menu with the keyboard, as a user could.
function Choose([int]$n, [string]$shot = '') {
    Open-Menu
    for ($i = 0; $i -lt $n; $i++) { Keys 'Down' }
    if ($shot) { Shot $shot }
    Keys 'Enter'
}

try {
    Step 'the tray menu'
    Open-Menu
    Shot "$dir\01-tray-menu.png"
    Keys 'Esc'
    Start-Sleep -Milliseconds 300

    Step 'Capture Full Screen from the menu'
    Choose 3 "$dir\02-menu-fullscreen.png"
    Start-Sleep -Milliseconds 1500
    $items = Items $dir
    $m = Meta $items[-1].FullName
    "items: $($items.Count); $($m.width)x$($m.height)"
    Copy-Item "$($items[-1].FullName)\image.png" "$dir\03-menu-fullscreen-capture.png"

    Step 'Capture Area from the menu freezes the screen without the menu, then Enter takes it'
    Choose 1
    Start-Sleep -Milliseconds 700
    Shot "$dir\02b-menu-area-overlay.png"
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
    $items = Items $dir
    "items: $($items.Count)"
    Copy-Item "$($items[-1].FullName)\image.png" "$dir\03b-menu-area-capture.png"

    Step "Self-Timer ($Timer s) counts down in a toast, then captures"
    Choose 6
    $chosen = Get-Date
    Start-Sleep -Milliseconds 1200
    Shot "$dir\04-countdown.png"
    Start-Sleep -Seconds ($Timer + 1)
    $items = Items $dir
    $m = Meta $items[-1].FullName
    "items: $($items.Count); captured $([int](([datetime]$m.created) - $chosen).TotalMilliseconds) ms after Enter on the menu item"
    Copy-Item "$($items[-1].FullName)\image.png" "$dir\05-timer-capture.png"

    Step 'Open Library and Settings ask surfaces that are not built yet'
    Choose 9
    Start-Sleep -Milliseconds 500
    Shot "$dir\06-open-library.png"
    Choose 11
    Start-Sleep -Milliseconds 500
    Shot "$dir\07-settings.png"
    Start-Sleep -Milliseconds 2500

    Step 'a double click on the icon captures an area'
    Show-Overflow
    DClick $IconX $IconY 250
    Start-Sleep -Milliseconds 700
    Glide 900 420 300
    Start-Sleep -Milliseconds 300
    Shot "$dir\08-double-click-overlay.png"
    Check wins -pid $g.Id
    & $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint capture"' | ForEach-Object { Keys 'Esc' }
    Start-Sleep -Milliseconds 400

    Step 'Quit Glint'
    Choose 12
    Start-Sleep -Milliseconds 1500
    "glint exited: $($g.HasExited)"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
