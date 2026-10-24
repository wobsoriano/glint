# Card actions and stack behavior that scenarios/cards.ps1 leaves out: three cards in a stack,
# More > Copy Text and More > Compress, Dismiss, Tuck into the edge and the peek tab, Clear All,
# the auto-close delay, a rearranged action layout, and Pin as an after-capture step.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
function StackShot($p, [string]$path) {
    $s = Stack $p
    if ($s) { Check shot -out $path -rect "$($s.X),$($s.Y),$($s.W),$($s.H)" | Out-Null; "stack: $($s.X),$($s.Y) $($s.W)x$($s.H)" } else { 'stack: none' }
}
$stage = Start-Stage
$g = $null
try {
    $dir = New-Run 'x-cards' @{ after_shot = @{ preview = $true; copy = $false; save = $false } }
    $g = Start-Glint $dir
    Step 'three captures stack three cards'
    Capture-Area 400 300 900 600
    Capture-Area 700 200 1200 500
    Capture-Area 760 330 1220 560
    Glide 1500 700 200
    Start-Sleep -Milliseconds 500
    StackShot $g "$dir\01-three-cards.png"
    $card = Card (Stack $g)
    $newest = (Items $dir)[-1].FullName

    Step 'More > Copy Text copies the text of the newest card'
    Set-Clipboard -Value 'glintcheck: before copy text'
    MenuRow $card 'CopyText'
    Start-Sleep -Milliseconds 3000
    Shot "$dir\02-copy-text.png"
    Check clip

    Step 'More > Compress copies a JPEG file'
    Set-Clipboard -Value 'glintcheck: before compress'
    MenuRow $card 'Compress'
    Start-Sleep -Milliseconds 2000
    Shot "$dir\03-compress.png"
    $clip = & $Check clip
    $file = ($clip | Select-String '^file: (.*)$').Matches | ForEach-Object { $_.Groups[1].Value } | Select-Object -First 1
    if ($file) { "compressed: $file, $((Get-Item $file).Length) bytes"; Check cmp -a $file -b "$newest\image.png" -maxmean 4 } else { $clip }

    Step 'Dismiss closes the newest card'
    Press $card $card.Dismiss
    Start-Sleep -Milliseconds 800
    Glide 1500 700 200
    Start-Sleep -Milliseconds 400
    StackShot $g "$dir\04-dismissed.png"
    "captures on disk after Dismiss: $((Items $dir).Count)"

    Step 'the tuck chevron folds the stack into a peek tab, and the tab brings the cards back'
    $s = Stack $g
    if (-not $s) { throw 'no stack to tuck' }
    Click ($s.X + 204) ($s.Y + 24) 250
    Start-Sleep -Milliseconds 800
    Glide 1500 700 200
    Start-Sleep -Milliseconds 400
    # glintcheck stack reports only the column of cards; the tab sits flush with the left edge,
    # lifted 18% of the 1032 px work area (internal/ui/stack/layout.go).
    Check shot -out "$dir\05-peek.png" -rect '0,743,60,124' | Out-Null
    Shot "$dir\05-peek-screen.png"
    "stack while tucked: $(if (Stack $g) { 'cards' } else { 'none (tab only)' })"
    Click 15 805 250
    Start-Sleep -Milliseconds 800
    Glide 1500 700 200
    Start-Sleep -Milliseconds 400
    StackShot $g "$dir\06-unpeeked.png"

    Step 'Clear All closes every card'
    $s = Stack $g
    if (-not $s) { throw 'no stack to clear' }
    Click ($s.X + 150) ($s.Y + 24) 250
    Start-Sleep -Milliseconds 800
    StackShot $g "$dir\07-cleared.png"
    "captures on disk after Clear All: $((Items $dir).Count)"
    Stop-Glint $g

    Step 'auto close after 3 s while the pointer is away'
    $dir = New-Run 'x-cards-autoclose' @{ after_shot = @{ preview = $true; copy = $false; save = $false }; overlay = @{ side = 'Left'; auto_close_seconds = 3; dismiss_after_drag = $true } }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    Glide 1500 700 200
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $shown = [bool](Stack $g)
    $gone = Wait-Until { -not (Stack $g) } 8000
    "card shown: $shown; gone after $($sw.ElapsedMilliseconds) ms: $gone"
    Stop-Glint $g

    Step 'a rearranged layout: Pin, Delete, Dismiss, More in the corners; Edit and Copy in the center'
    $layout = @{ corners = @('pin', 'delete', 'dismiss', 'more'); center = @('edit', 'copy'); hidden = @('save', 'save_as', 'compress', 'quick_look', 'copy_text') }
    $dir = New-Run 'x-cards-layout' @{ after_shot = @{ preview = $true; copy = $false; save = $false }; overlay = @{ side = 'Left'; auto_close_seconds = 0; dismiss_after_drag = $true; layout = $layout } }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $s = Stack $g
    $card = Card $s
    Glide $card.CX ($card.Y + 8) 250
    Start-Sleep -Milliseconds 600
    Check shot -out "$dir\08-layout.png" -rect "$($s.X),$($s.Y),$($s.W),$($s.H)" | Out-Null
    Press $card $card.More
    Start-Sleep -Milliseconds 500
    Check shot -out "$dir\09-layout-more.png" -rect "$($s.X),$($s.Y),$($s.W),$($s.H)" | Out-Null
    Keys 'Esc'
    Stop-Glint $g

    Step 'after capture: Pin floats the capture'
    $dir = New-Run 'x-cards-pin' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $true } }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $ok = Wait-Until { (Win 'Pinned screenshot') -ne [IntPtr]::Zero } 4000
    $p = WinRect 'Pinned screenshot'
    "pin shows: $ok at $($p.X),$($p.Y) $($p.W)x$($p.H); stack: $(if (Stack $g) { 'shown' } else { 'none' })"
    Shot "$dir\10-after-capture-pin.png"
    Front 'Pinned screenshot'
    Keys 'Esc'
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
