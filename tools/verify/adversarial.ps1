# Adversarial captures and command lines: a 1x1 drag, a click without a drag, an area over the
# taskbar, ten Ctrl+Shift+3 in a burst, Ctrl+Shift+4 twice, Ctrl+Shift+3 while the overlay is
# open, a capture whose folder is deleted while its card shows, and bad or odd command lines
# sent to a running Glint.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
function Alive { -not $g.HasExited }
function Overlays { @(& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint capture"').Count }
$stage = Start-Stage
$g = $null
try {
    $dir = New-Run 'x-adv' @{ after_shot = $off }
    $g = Start-Glint $dir

    Step 'a 1x1 drag'
    $n = (Items $dir).Count
    Keys 'Ctrl+Shift+4'; Start-Sleep -Milliseconds 500
    Glide 500 500 200; Start-Sleep -Milliseconds 100; Down; Start-Sleep -Milliseconds 80
    [Promo.In]::SetCursorPos(501, 501) | Out-Null; Start-Sleep -Milliseconds 120; Up
    Start-Sleep -Milliseconds 1500
    $items = Items $dir
    "items $n -> $($items.Count); overlay still open: $(Overlays); alive: $(Alive)"
    if ($items.Count -gt $n) { $m = Meta $items[-1].FullName; "  captured $($m.width)x$($m.height)" }
    if (Overlays) { Shot "$dir\01-after-1x1.png"; Keys 'Esc'; Start-Sleep -Milliseconds 300 }

    Step 'a click without a drag over the stage'
    $n = (Items $dir).Count
    Keys 'Ctrl+Shift+4'; Start-Sleep -Milliseconds 500
    Click 900 420 250
    Start-Sleep -Milliseconds 1800
    $items = Items $dir
    "items $n -> $($items.Count); overlay still open: $(Overlays)"
    if ($items.Count -gt $n) { $m = Meta $items[-1].FullName; "  captured $($m.width)x$($m.height), source '$($m.source)'" }
    if (Overlays) { Keys 'Esc'; Start-Sleep -Milliseconds 300 }

    Step 'an area over the taskbar, to the bottom-right corner'
    Glide 1300 600 200
    Check shot -out "$dir\02-before-taskbar.png" | Out-Null
    Keys 'Ctrl+Shift+4'; Start-Sleep -Milliseconds 500
    Glide 1500 900 200; Down; Start-Sleep -Milliseconds 80; Glide 1919 1079 400; Start-Sleep -Milliseconds 150; Up
    Start-Sleep -Milliseconds 1500
    $it = (Items $dir)[-1].FullName
    $m = Meta $it
    $a = (Get-Content "$dir\data\settings.json" | ConvertFrom-Json).last_area
    "captured $($m.width)x$($m.height); last_area $($a -join ',')"
    Check cmp -a "$it\image.png" -b "$dir\02-before-taskbar.png" -brect "$($a[0]),$($a[1]),$($a[2]),$($a[3])" -maxmean 3

    Step 'ten Ctrl+Shift+3 in a burst'
    $n = (Items $dir).Count
    for ($i = 0; $i -lt 10; $i++) { Keys 'Ctrl+Shift+3' }
    Start-Sleep -Seconds 6
    "items $n -> $((Items $dir).Count); alive: $(Alive)"

    Step 'Ctrl+Shift+4 twice opens one overlay'
    Keys 'Ctrl+Shift+4'; Keys 'Ctrl+Shift+4'
    Start-Sleep -Milliseconds 700
    "overlay windows: $(Overlays)"
    Step 'Ctrl+Shift+3 while the overlay is open'
    $n = (Items $dir).Count
    Keys 'Ctrl+Shift+3'
    Start-Sleep -Milliseconds 1500
    "items $n -> $((Items $dir).Count); overlay still open: $(Overlays)"
    if ((Items $dir).Count -gt $n) { Copy-Item "$((Items $dir)[-1].FullName)\image.png" "$dir\03-fullscreen-during-overlay.png" }
    Keys 'Esc'; Start-Sleep -Milliseconds 400
    "overlay after Esc: $(Overlays)"

    Step 'command lines sent to the running Glint'
    $cases = @(@('--bogus'), @('--capture'), @('--capture', 'bogus'), @('--trim'), @("`"$dir\missing.png`""), @('--studio', "`"$dir\no-such-session`""), @('--trim', "`"$dir\data\settings.json`""), @("`"$dir\data\settings.json`""))
    $i = 0
    foreach ($c in $cases) {
        $q = Start-Process "$Bin\glint.exe" -ArgumentList $c -PassThru -NoNewWindow -Wait -RedirectStandardError "$dir\cli-$i.err"
        Start-Sleep -Milliseconds 900
        Shot "$dir\04-cli-$i.png"
        "glint.exe $($c -join ' ') -> exit $($q.ExitCode); stderr: $((Get-Content "$dir\cli-$i.err" -ErrorAction SilentlyContinue) -join ' | '); running Glint alive: $(Alive)"
        $i++
    }
    "items now: $((Items $dir).Count)"
    Stop-Glint $g

    Step 'a capture deleted from disk while its card shows'
    $dir = New-Run 'x-adv-deleted' @{ after_shot = @{ preview = $true; copy = $false; save = $false } }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $it = (Items $dir)[-1].FullName
    $card = Card (Stack $g)
    Remove-Item -Recurse -Force $it
    "folder removed: $(-not (Test-Path $it))"
    Set-Clipboard -Value 'glintcheck: before'
    Press $card $card.Copy; Start-Sleep -Milliseconds 900; Shot "$dir\01-copy.png"
    Check clip
    Press $card $card.Save; Start-Sleep -Milliseconds 1200; Shot "$dir\02-save.png"
    "exports: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', ')"
    Press $card $card.Edit; Start-Sleep -Milliseconds 1500; Shot "$dir\03-edit.png"
    Check wins -pid $g.Id
    Close-Win ('Glint ' + [char]0x2014)
    "alive: $(Alive); Library folders: $(@(Get-ChildItem "$dir\data\Library" -Force -ErrorAction SilentlyContinue).Name -join ', ')"
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g; Get-Content "$dir\glint.err" }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
