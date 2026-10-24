# Adversarial file system cases: a unicode title and a 300-character title saved from the
# editor, an export folder that denies writes, an editor Done into a capture folder that denies
# writes, a Library folder that denies writes, Glint killed mid-recording and started again,
# Studio closed during an export and Glint quit during an export.
param([int]$IconX = 1642, [int]$IconY = 955)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$Ed = 'Glint ' + [char]0x2014 + ' '
$me = "$env:USERDOMAIN\$env:USERNAME"
function Deny([string]$path) { icacls $path /deny "${me}:(OI)(CI)(W,D,DC)" | Out-Null }
function Allow([string]$path) { icacls $path /remove:d $me | Out-Null }
function SetTitle([string]$item, [string]$title) {
    $m = Get-Content -Raw -Encoding UTF8 "$item\meta.json" | ConvertFrom-Json
    $m.title = $title
    [IO.File]::WriteAllText("$item\meta.json", ($m | ConvertTo-Json), (New-Object Text.UTF8Encoding $false))
}
function Recordings([string]$d) { @(Get-ChildItem "$d\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue | Sort-Object Name) }
function Picker { [bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint recording"') }
$stage = Start-Stage
$g = $null
$denied = @()
try {
    Step 'a unicode title and a 300-character title, saved from the editor'
    $dir = New-Run 'x-adv2-titles' @{ after_shot = $off }
    $g = Start-Glint $dir
    Capture-Area 400 300 700 500
    Capture-Area 420 320 720 520
    Stop-Glint $g
    $items = Items $dir
    $uni = 'Gl' + [char]0x65E5 + [char]0x672C + [char]0x8A9E + ' ' + [char]0xD83C + [char]0xDF89 + ' caf' + [char]0xE9 + ' a/b\c:*?"<>|'
    $long = 'Long ' + ('x' * 295)
    SetTitle $items[0].FullName $uni
    SetTitle $items[1].FullName $long
    $g = Start-Glint $dir
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    Start-Sleep -Milliseconds 1200
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Hide-Stage
    Start-Sleep -Milliseconds 600
    Shot "$dir\01-library-titles.png"
    Close-Win 'Glint Library'
    foreach ($it in $items) {
        $t = (Get-Content -Raw -Encoding UTF8 "$($it.FullName)\meta.json" | ConvertFrom-Json).title
        Start-Process "$Bin\glint.exe" -ArgumentList "`"$($it.FullName)\image.png`"" -NoNewWindow -Wait
        $ok = Wait-Until { (Win "$Ed$($t.Substring(0, 2))") -ne [IntPtr]::Zero } 6000
        Start-Sleep -Milliseconds 900
        Keys 'Shift'
        Front "$Ed$($t.Substring(0, 2))"
        Start-Sleep -Milliseconds 400
        Keys 'Ctrl+S'
        Start-Sleep -Milliseconds 1500
        Shot "$dir\02-save-$($it.Name).png"
        "editor for a $($t.Length)-char title shows: $ok; exports now: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue | ForEach-Object { "$($_.Name) ($($_.Name.Length) chars)" }) -join ' | ')"
        Close-Win "$Ed$($t.Substring(0, 2))"
        Start-Sleep -Milliseconds 500
    }
    Show-Stage
    Stop-Glint $g

    Step 'an export folder that denies writes: Save after capture'
    $dir = New-Run 'x-adv2-ro-export' @{ after_shot = @{ preview = $false; copy = $false; save = $true; edit = $false; pin = $false } }
    New-Item -ItemType Directory -Force "$dir\exports" | Out-Null
    Deny "$dir\exports"; $denied += "$dir\exports"
    $g = Start-Glint $dir
    Capture-Area 400 300 700 500
    Start-Sleep -Milliseconds 1000
    Shot "$dir\01-save-denied.png"
    Check wins -pid $g.Id
    Keys 'Esc'
    Start-Sleep -Milliseconds 600
    "capture still in the Library: $((Items $dir).Count)"
    Stop-Glint $g

    Step 'Done into a capture folder that denies writes keeps the editor open'
    $dir = New-Run 'x-adv2-ro-item' @{ after_shot = $off }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $it = (Items $dir)[-1].FullName
    $title = (Meta $it).title
    $before = (Get-FileHash "$it\image.png").Hash
    Deny $it; $denied += $it
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$it\image.png`"" -NoNewWindow -Wait
    Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Keys 'Shift'
    Front "$Ed$title"
    Hide-Stage
    Start-Sleep -Milliseconds 500
    Keys 'A'
    Drag 480 440 660 560 500
    Keys 'Ctrl+Enter'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\01-done-denied.png"
    "editor still open after a failed Done: $((Win "$Ed$title") -ne [IntPtr]::Zero); image.png unchanged: $((Get-FileHash "$it\image.png").Hash -eq $before); files: $((Get-ChildItem $it -Force).Name -join ', ')"
    Allow $it
    Front "$Ed$title"
    Keys 'Ctrl+Enter'
    $closed = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 4000
    "after allowing writes, Done closes: $closed; edit.json: $(Test-Path "$it\edit.json")"
    Show-Stage

    Step 'a Library folder that denies writes'
    Deny "$dir\data\Library"; $denied += "$dir\data\Library"
    $n = (Items $dir).Count
    Capture-Area 400 300 700 500
    Start-Sleep -Milliseconds 800
    Shot "$dir\02-library-denied.png"
    "items $n -> $((Items $dir).Count); alive: $(-not $g.HasExited)"
    Allow "$dir\data\Library"
    Stop-Glint $g

    Step 'Glint killed mid-recording, then started again'
    $dir = New-Run 'x-adv2-kill' @{ after_recording = @{ preview = $true; copy = $false; save = $false; studio = $false }; record = @{ target = 'Display'; area = $null; camera = $null; mic = $null; system_audio = $false; countdown = 0; teleprompter = $false; script = ''; prompter_lines = 3 } }
    $g = Start-Glint $dir
    Keys 'Ctrl+Shift+7'; Wait-Until { Picker } 3000 | Out-Null; Start-Sleep -Milliseconds 400
    Keys 'Enter'
    Start-Sleep -Seconds 2
    Stop-Process -Id $g.Id -Force
    Start-Sleep -Milliseconds 800
    "after the kill: $(@(Get-ChildItem "$dir\data\Library" -Force -Directory).Name -join ', ')"
    $g = Start-Glint $dir
    Start-Sleep -Milliseconds 1500
    "after starting again: $(@(Get-ChildItem "$dir\data\Library" -Force -Directory).Name -join ', ')"
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    Start-Sleep -Milliseconds 1200
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Hide-Stage
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-library-after-kill.png"
    Close-Win 'Glint Library'
    Show-Stage

    Step 'a 4 s recording, then Studio closed while it exports'
    Keys 'Ctrl+Shift+7'; Wait-Until { Picker } 3000 | Out-Null; Start-Sleep -Milliseconds 400
    Keys 'Enter'
    Start-Sleep -Seconds 4
    Keys 'Ctrl+Shift+7'
    Wait-Until { (Recordings $dir).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 1000
    $rec = (Recordings $dir)[-1].FullName
    $title = (Meta $rec).title
    $win = "$title - Glint Studio"
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000 | Out-Null
    Start-Sleep -Milliseconds 1500
    Keys 'Shift'; Front $win
    Keys 'Ctrl+E'; Start-Sleep -Milliseconds 700; Keys 'Enter'
    Start-Sleep -Milliseconds 700
    Shot "$dir\02-exporting.png"
    Close-Win $win
    $closed = Wait-Until { (Win $win) -eq [IntPtr]::Zero } 5000
    Start-Sleep -Milliseconds 1500
    Shot "$dir\03-closed-mid-export.png"
    "Studio closed mid-export: $closed; render: $(@(Get-ChildItem "$rec\render" -ErrorAction SilentlyContinue).Name -join ', '); exports: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', ')"

    Step 'Glint quit from the tray while Studio exports'
    Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$rec`"" -NoNewWindow -Wait
    Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000 | Out-Null
    Start-Sleep -Milliseconds 1500
    Keys 'Shift'; Front $win
    Keys 'Ctrl+E'; Start-Sleep -Milliseconds 700; Keys 'Enter'
    Start-Sleep -Milliseconds 600
    Choose 12
    $exited = Wait-Until { $g.HasExited } 15000
    Start-Sleep -Milliseconds 500
    "glint exited: $exited; render: $(@(Get-ChildItem "$rec\render" -ErrorAction SilentlyContinue).Name -join ', '); exports: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', ')"
    if (-not $exited) { Shot "$dir\04-quit-mid-export.png" }
} finally {
    foreach ($d in $denied) { Allow $d }
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
