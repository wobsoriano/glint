# The editor's sidebar from top to bottom and its crop bar, as screenshots to find the
# controls by; the editor is placed at 80,40 1360x880 at 100%.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-editor2' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $true; pin = $false } }
$g = Start-Glint $dir
$stage = Start-Stage
$Ed = 'Glint ' + [char]0x2014 + ' '
try {
    Capture-Area 400 300 900 600
    $item = (Items $dir)[-1].FullName
    $title = (Meta $item).title
    Hide-Stage
    if (-not (Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000)) { throw 'the editor did not open' }
    Start-Sleep -Milliseconds 700
    Place "$Ed$title" 80 40 1360 880 | Out-Null
    Front "$Ed$title"
    Start-Sleep -Milliseconds 800
    Glide 1270 500 200
    for ($i = 0; $i -lt 6; $i++) {
        Check shot -out ("$dir\01-sidebar-{0}.png" -f $i) -rect '1200,130,320,780' | Out-Null
        Wheel -480
        Start-Sleep -Milliseconds 400
    }
    Keys 'C'
    Start-Sleep -Milliseconds 500
    Check shot -out "$dir\02-crop-bar.png" -rect '80,40,1360,140' | Out-Null
    Keys 'Esc'
    Close-Win "$Ed$title"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
