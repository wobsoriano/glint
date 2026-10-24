# With "Save without asking where" off, a card's Save asks where to save; the editor's
# Ctrl+Shift+S asks even when it is on. Each dialog gets a path typed into its file name box.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$Ed = 'Glint ' + [char]0x2014 + ' '
function SaveDialog([string]$path) {
    $h = $null
    $ok = Wait-Until { ($script:h = & $Check wins -pid $g.Id | Select-String 'visible=true.*"(Save|Save As|Save screenshot)[^"]*"') -ne $null } 5000
    Start-Sleep -Milliseconds 800
    Shot "$dir\dialog-$(Split-Path -Leaf $path).png"
    "dialog: $ok $($script:h)"
    if (-not $ok) { return }
    Keys 'Alt+N'; Keys 'Ctrl+A'
    TypeText $path 15
    Keys 'Enter'
    Start-Sleep -Milliseconds 1500
}
$stage = Start-Stage
$g = $null
try {
    $dir = New-Run 'x-savedialog' @{ after_shot = @{ preview = $true; copy = $false; save = $false; edit = $false; pin = $false }; save_without_dialog = $false }
    New-Item -ItemType Directory -Force "$dir\chosen" | Out-Null
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $card = Card (Stack $g)
    Press $card $card.Save
    SaveDialog "$dir\chosen\from-card.png"
    "chosen folder: $((Get-ChildItem "$dir\chosen").Name -join ', '); export folder: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', ')"
    Stop-Glint $g

    $dir = New-Run 'x-savedialog-editor' @{ after_shot = @{ preview = $false; copy = $false; save = $false; edit = $true; pin = $false } }
    New-Item -ItemType Directory -Force "$dir\chosen" | Out-Null
    $g = Start-Glint $dir
    Capture-Area 400 300 900 600
    $title = (Meta (Items $dir)[-1].FullName).title
    if (-not (Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 6000)) { throw 'the editor did not open' }
    Start-Sleep -Milliseconds 800
    Front "$Ed$title"
    Keys 'Ctrl+Shift+S'
    SaveDialog "$dir\chosen\from-editor.png"
    "chosen folder: $((Get-ChildItem "$dir\chosen").Name -join ', '); export folder: $(@(Get-ChildItem "$dir\exports" -ErrorAction SilentlyContinue).Name -join ', ')"
    Close-Win "$Ed$title"
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    Stop-Process -Id $stage.Id -Force -ErrorAction SilentlyContinue
}
