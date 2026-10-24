# Done into a capture folder that refuses new files and overwrites (reads still allowed) must
# keep the editor open with the edit; a capture titled CON, a reserved device name, saved from
# the editor.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$Ed = 'Glint ' + [char]0x2014 + ' '
$me = "$env:USERDOMAIN\$env:USERNAME"
function Deny([string]$path) { icacls $path /deny "${me}:(OI)(CI)(WD,AD,DC)" | Out-Null }
function Allow([string]$path) { icacls $path /remove:d $me | Out-Null }
function Open-Ed([string]$it, [string]$title) {
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$it\image.png`"" -NoNewWindow -Wait
    Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Keys 'Shift'
    Front "$Ed$title"
    Start-Sleep -Milliseconds 500
}
$stage = Start-Stage
$g = $null
$it = $null
try {
    $dir = New-Run 'x-adv3' @{ after_shot = $off }
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    Capture-Area 420 320 700 500
    $items = Items $dir
    $it = $items[0].FullName
    $title = (Meta $it).title
    Hide-Stage

    Step 'Done into a folder that refuses writes keeps the editor open'
    $before = (Get-FileHash "$it\image.png").Hash
    Deny $it
    Open-Ed $it $title
    "editor opened on the read-only capture: $((Win "$Ed$title") -ne [IntPtr]::Zero)"
    Keys 'A'
    Drag 480 440 660 560 500
    Keys 'Ctrl+Enter'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\01-done-denied.png"
    "editor still open after the failed Done: $((Win "$Ed$title") -ne [IntPtr]::Zero); image.png unchanged: $((Get-FileHash "$it\image.png").Hash -eq $before); files: $((Get-ChildItem $it -Force).Name -join ', ')"
    Allow $it
    Front "$Ed$title"
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+Enter'
    $closed = Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 4000
    "with writes allowed again, Done closes: $closed; edit.json now: $(Test-Path "$it\edit.json")"
    if ($closed) { Check edited -dir $it }

    Step 'a capture titled CON saved from the editor'
    $con = $items[1].FullName
    $m = Get-Content -Raw "$con\meta.json" | ConvertFrom-Json
    $m.title = 'CON'
    [IO.File]::WriteAllText("$con\meta.json", ($m | ConvertTo-Json), (New-Object Text.UTF8Encoding $false))
    Stop-Glint $g
    $g = Start-Glint $dir
    Open-Ed $con 'CON'
    Keys 'Ctrl+S'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\02-save-con.png"
    "exports: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', '); dialog: $([bool](& $Check wins -pid $g.Id | Select-String 'visible=true.*rect=\S+ 572x'))"
    Keys 'Esc'
    Close-Win "${Ed}CON"
} finally {
    if ($it) { Allow $it }
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
