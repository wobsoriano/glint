# Settings from the tray menu, and that its changes reach the running app: the area shortcut
# moved to Ctrl+Alt+K (the new one captures, the old one does nothing), the preview stack
# moved to the right, after-capture Copy turned on and off, and Launch at login written to
# and removed from the registry. Coordinates are for the window placed at 80,60 at 100%.
param([int]$IconX = 1642, [int]$IconY = 955)
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'prefs' @{ after_shot = @{ preview = $true; copy = $false; save = $false } }
$g = Start-Glint $dir
function Saved { Get-Content "$dir\data\settings.json" | ConvertFrom-Json }
function Overlay-Open { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint capture"') }
$runKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
try {
    Step 'tray > Settings… opens Settings'
    Choose 11
    $ok = Wait-Until { (Win 'Glint Settings') -ne [IntPtr]::Zero }
    Start-Sleep -Milliseconds 800
    Place 'Glint Settings' 80 60 860 640 | Out-Null
    Start-Sleep -Milliseconds 500
    "Settings shows: $ok"
    Shot "$dir\01-settings.png"

    Step 'Shortcuts: Change the area shortcut to Ctrl+Alt+K'
    Click 160 266 200
    Click 851 200 200
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+Alt+K'
    Start-Sleep -Milliseconds 600
    Shot "$dir\02-shortcut.png"
    "settings.json hotkeys.area: $((Saved).hotkeys.area)"

    Step 'Ctrl+Alt+K captures an area'
    $n = (Items $dir).Count
    Capture-Area 1000 300 1400 600 'Ctrl+Alt+K'
    "items: $n -> $((Items $dir).Count)"

    Step 'Ctrl+Shift+4 no longer opens the overlay'
    Keys 'Ctrl+Shift+4'
    Start-Sleep -Milliseconds 800
    $open = Overlay-Open
    Shot "$dir\03-old-shortcut.png"
    "overlay opened by Ctrl+Shift+4: $open"
    if ($open) { Keys 'Esc' }

    Step 'Overlay: Position Right moves the open stack, and the next card shows on the right'
    $before = Stack $g
    "stack before: $($before.X),$($before.Y)"
    Click 154 230 200
    # The stack's hidden owner window sits mid-screen; a settings change once drew it there as
    # a bare caption. Its spot must look the same before and after the change.
    $owner = & $Check wins -pid $g.Id | Select-String 'rect=\((-?\d+),(-?\d+) (\d+)x(\d+)\) "Glint stack"' | ForEach-Object { $_.Matches[0].Groups[1..4].Value -join ',' }
    Glide 1500 880 200
    Start-Sleep -Milliseconds 300
    Check shot -out "$dir\04-owner-before.png" -rect $owner | Out-Null
    Click 858 172 200
    Start-Sleep -Milliseconds 500
    Glide 1500 880 200
    Start-Sleep -Milliseconds 800
    Check shot -out "$dir\04-owner-after.png" -rect $owner | Out-Null
    "the stack's owner at $owner stays undrawn:"
    Check cmp -a "$dir\04-owner-before.png" -b "$dir\04-owner-after.png"
    $moved = Stack $g
    "settings.json overlay.side: $((Saved).overlay.side); stack now: $($moved.X),$($moved.Y) $($moved.W)x$($moved.H)"
    Capture-Area 1000 300 1400 600 'Ctrl+Alt+K'
    $after = Stack $g
    "stack after the next capture: $($after.X),$($after.Y) $($after.W)x$($after.H) (right half: $($after.X -gt $ScreenW / 2))"
    Shot "$dir\04-stack-right.png"

    Step 'Screenshots: after-capture Copy on puts the next capture on the clipboard'
    Click 168 158 200
    Click 871 225 200
    Start-Sleep -Milliseconds 300
    "settings.json after_shot.copy: $((Saved).after_shot.copy)"
    Shot "$dir\05-copy-on.png"
    Set-Clipboard -Value 'glintcheck: before the capture'
    Capture-Area 1000 300 1400 600 'Ctrl+Alt+K'
    $item = (Items $dir)[-1].FullName
    Check clip -png "$dir\05-clipboard.png"
    Check cmp -a "$dir\05-clipboard.png" -b "$item\image.png"

    Step 'after-capture Copy off leaves the clipboard alone'
    Click 871 225 200
    Start-Sleep -Milliseconds 300
    "settings.json after_shot.copy: $((Saved).after_shot.copy)"
    Set-Clipboard -Value 'glintcheck: before the capture'
    Capture-Area 1000 300 1400 600 'Ctrl+Alt+K'
    Check clip

    Step 'General: Launch at login writes and removes the Run entry'
    if ((Get-ItemProperty $runKey).PSObject.Properties.Name -contains 'Glint') {
        'skipped: this account already starts a Glint at login, which the check must not touch'
    } else {
        Click 155 122 200
        Click 871 165 200
        Start-Sleep -Milliseconds 400
        "on: Run\Glint = $((Get-ItemProperty $runKey).Glint); settings.json launch_at_login: $((Saved).launch_at_login)"
        Shot "$dir\06-login-on.png"
        Click 871 165 200
        Start-Sleep -Milliseconds 400
        "off: Run\Glint present: $((Get-ItemProperty $runKey).PSObject.Properties.Name -contains 'Glint'); settings.json launch_at_login: $((Saved).launch_at_login)"
    }
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
