# Shared by the scenarios. Run one inside the desktop lock:
#   $env:GLINT_BIN = <dir with glint.exe and glintcheck.exe>; $env:GLINT_OUT = <evidence dir>
#   go run ./tools/desklock -- powershell -NoProfile -File internal/app/cmd/glintcheck/scenarios/area.ps1
. (Join-Path $PSScriptRoot '..\..\..\..\..\tools\drive\drive.ps1')
$Bin = $env:GLINT_BIN
$Check = Join-Path $Bin 'glintcheck.exe'

# New-Run makes an evidence folder with a data directory holding settings.json. Glint's own
# windows are included in captures unless the settings say otherwise, so screenshots show them.
function New-Run([string]$name, [hashtable]$settings = @{}) {
    $dir = Join-Path $env:GLINT_OUT $name
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force "$dir\data" | Out-Null
    $s = @{ include_own_windows = $true; save_dir = "$dir\exports" }
    foreach ($k in $settings.Keys) { $s[$k] = $settings[$k] }
    $s | ConvertTo-Json -Depth 5 | Set-Content -Encoding ascii "$dir\data\settings.json"
    $dir
}

function Start-Glint([string]$dir) {
    $env:GLINT_DATA_DIR = "$dir\data"
    $p = Start-Process "$Bin\glint.exe" -ArgumentList '--background' -PassThru -NoNewWindow -RedirectStandardError "$dir\glint.err"
    Start-Sleep -Milliseconds 1500
    $p
}

# Stop-Glint waits for the exit: a Glint started while the killed one still holds the
# single-instance lock hands it its command line and exits.
function Stop-Glint($p) {
    if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id -Force; $p.WaitForExit(5000) | Out-Null }
}

# The stage is a topmost window of known noise and text, so captures land on pixels that do
# not change between the reference screenshot and Glint's.
function Start-Stage {
    $p = Start-Process $Check -ArgumentList 'stage' -PassThru -NoNewWindow
    Start-Sleep -Milliseconds 1500
    $p
}

# The tray helpers read $IconX and $IconY, where this machine shows the icon in the taskbar's
# overflow, and $g, the Glint process, from the scenario.
# Chevron finds the taskbar's Show Hidden Icons button through UI Automation. Its place moves
# when Windows adds an icon such as the microphone-in-use indicator, and a click at a fixed
# point then lands on the icon beside it.
function Chevron {
    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
    $bar = [Windows.Automation.AutomationElement]::RootElement.FindFirst([Windows.Automation.TreeScope]::Children,
        (New-Object Windows.Automation.PropertyCondition ([Windows.Automation.AutomationElement]::ClassNameProperty), 'Shell_TrayWnd'))
    $b = if ($bar) { $bar.FindFirst([Windows.Automation.TreeScope]::Descendants,
        (New-Object Windows.Automation.PropertyCondition ([Windows.Automation.AutomationElement]::NameProperty), 'Show Hidden Icons')) }
    if (-not $b) { throw 'the taskbar has no Show Hidden Icons button' }
    $r = $b.Current.BoundingRectangle
    @([int]($r.X + $r.Width / 2), [int]($r.Y + $r.Height / 2))
}

function Show-Overflow {
    & $Check mark -at "$IconX,$IconY" 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) {
        $c = Chevron
        Click $c[0] $c[1] 300
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

# Choose picks the nth item of the tray menu with the keyboard, as a user could.
function Choose([int]$n, [string]$shot = '') {
    Open-Menu
    for ($i = 0; $i -lt $n; $i++) { Keys 'Down' }
    if ($shot) { Shot $shot }
    Keys 'Enter'
}

# The stage stays on top of normal windows; minimize it to see one, such as Quick Look.
function Hide-Stage { [Promo.In]::ShowWindow((Win 'Glint stage'), 6) | Out-Null; Start-Sleep -Milliseconds 300 }
function Show-Stage { [Promo.In]::ShowWindow((Win 'Glint stage'), 9) | Out-Null; Start-Sleep -Milliseconds 300 }

function Check {
    & $Check @args
    "  -> exit $LASTEXITCODE"
}

function Items([string]$dir) {
    @(Get-ChildItem "$dir\data\Library" -Directory -ErrorAction SilentlyContinue | Where-Object { -not $_.Name.StartsWith('.') } | Sort-Object Name)
}

function Step([string]$text) { "== $text" }

# Probe reports a PNG's size and the alpha of its top-left corner and centre.
function Probe([string]$png) {
    $b = [Drawing.Bitmap]::FromFile($png)
    "$(Split-Path -Leaf $png): $($b.Width)x$($b.Height), corner alpha $($b.GetPixel(0, 0).A), centre alpha $($b.GetPixel([int]($b.Width / 2), [int]($b.Height / 2)).A)"
    $b.Dispose()
}

function Meta([string]$item) { Get-Content "$item\meta.json" | ConvertFrom-Json }

# Capture-Area drags an area with Ctrl+Shift+4 and waits for the capture to land.
function Capture-Area([int]$x1, [int]$y1, [int]$x2, [int]$y2, [string]$keys = 'Ctrl+Shift+4') {
    Keys $keys
    Start-Sleep -Milliseconds 500
    Glide $x1 $y1 300
    Start-Sleep -Milliseconds 150
    Down
    Start-Sleep -Milliseconds 100
    Glide $x2 $y2 400
    Start-Sleep -Milliseconds 200
    Up
    Start-Sleep -Milliseconds 1500
}

# Stack is the preview stack's window in physical pixels, or $null when no card shows.
function Stack($p) {
    $r = & $Check stack -pid $p.Id 2>$null
    if ($LASTEXITCODE -ne 0) { return $null }
    $v = "$r" -split ','
    [pscustomobject]@{ X = [int]$v[0]; Y = [int]$v[1]; W = [int]$v[2]; H = [int]$v[3]; K = [int]$v[2] / 244 }
}

# Card is where the stack's newest card is, and its buttons for the default layout
# (internal/ui/stack/view.go): Copy and Save pills, Dismiss, Edit, Pin and More in the corners,
# and the rows of the More menu.
function Card($s) {
    $k = $s.K
    $x = $s.X + [int](10 * $k); $y = $s.Y + [int](50 * $k); $w = [int](224 * $k); $h = [int](148 * $k)
    $cx = $x + [int]($w / 2); $cy = $y + [int]($h / 2)
    $c = [int](20 * $k)
    $rows = @{}
    $i = 0
    foreach ($a in 'SaveAs', 'Compress', 'QuickLook', 'CopyText', 'Delete') { $rows[$a] = $y + [int]((21 + 24 * $i) * $k); $i++ }
    [pscustomobject]@{
        X = $x; Y = $y; W = $w; H = $h; CX = $cx; CY = $cy
        Copy = @($cx, ($cy - [int](19 * $k))); Save = @($cx, ($cy + [int](19 * $k)))
        Dismiss = @(($x + $c), ($y + $c)); Edit = @(($x + $w - $c), ($y + $c))
        Pin = @(($x + $c), ($y + $h - $c)); More = @(($x + $w - $c), ($y + $h - $c))
        Menu = $rows
    }
}

# Press clicks a button of a card: it hovers the card first, as a user does, so the buttons show.
function Press($card, $at) {
    Glide $card.CX ($card.Y + 8) 250
    Start-Sleep -Milliseconds 250
    Click $at[0] $at[1] 200
}

function MenuRow($card, [string]$action) {
    Press $card $card.More
    Start-Sleep -Milliseconds 300
    Click $card.CX $card.Menu[$action] 200
}

if (-not ("GlintCheck.Msg" -as [type])) {
    Add-Type -Namespace GlintCheck -Name Msg -MemberDefinition '[DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, uint m, IntPtr w, IntPtr l);'
}
# Close-Win asks the window to close, as its close button does.
function Close-Win([string]$title) {
    $h = Win $title
    if ($h -ne [IntPtr]::Zero) { [GlintCheck.Msg]::PostMessage($h, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null }
}

function WinRect([string]$title) {
    $h = Win $title
    if ($h -eq [IntPtr]::Zero) { return $null }
    $r = New-Object Promo.In+RECT
    [Promo.In]::GetWindowRect($h, [ref]$r) | Out-Null
    [pscustomobject]@{ X = $r.L; Y = $r.T; W = $r.R - $r.L; H = $r.B - $r.T }
}

function Wait-Until([scriptblock]$cond, [int]$ms = 4000) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt $ms) {
        if (& $cond) { return $true }
        Start-Sleep -Milliseconds 100
    }
    $false
}
