# On a copy of a Rust-written Library: the Library's Copy on a Rust recording that already has
# the Rust build's render in render/, timed, with the render folder before and after. Then
# After recording > Save writes the recording's render into the export folder.
param([Parameter(Mandatory)] [string] $Seed)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$dir = New-Run 'x-rustrender' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
Copy-Item -Recurse $Seed "$dir\data\Library"
$rec = "$dir\data\Library\2026-10-17_22-35-47_1424.glintrec"
$g = Start-Glint $dir
try {
    "render before: $((Get-ChildItem "$rec\render").Name -join ', ')"
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    if (-not (Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 6000)) { throw 'Library did not open' }
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 0 0 1920 1032 | Out-Null
    Keys 'Shift'; Front 'Glint Library'
    Start-Sleep -Milliseconds 1000
    Click 722 125 200
    Start-Sleep -Milliseconds 400
    Set-Clipboard -Value 'glintcheck: before'
    $sw = [Diagnostics.Stopwatch]::StartNew()
    Click 1700 910 200
    $ok = Wait-Until { (& $Check clip) -match '^file: .*\.mp4' } 120000
    "Copy put a video on the clipboard: $ok after $($sw.ElapsedMilliseconds) ms"
    Shot "$dir\01-copied.png"
    Check clip
    "render after: $((Get-ChildItem "$rec\render").Name -join ', ')"
    Close-Win 'Glint Library'
    Stop-Glint $g

    Step 'After recording > Save writes the render into the export folder'
    $dir = New-Run 'x-rec-save' @{ after_recording = @{ preview = $false; copy = $false; save = $true; studio = $false }; record = @{ target = 'Display'; area = $null; camera = $null; mic = $null; system_audio = $false; countdown = 0; teleprompter = $false; script = ''; prompter_lines = 3 } }
    $g = Start-Glint $dir
    Keys 'Ctrl+Shift+7'; Start-Sleep -Milliseconds 900; Keys 'Enter'
    Start-Sleep -Seconds 3
    Keys 'Ctrl+Shift+7'
    $saved = Wait-Until { @(Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue).Count -ge 1 } 60000
    Start-Sleep -Milliseconds 800
    "saved: $saved"
    Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue | ForEach-Object { Check probe -file $_.FullName }
} finally {
    Glide 1500 880 200
    Stop-Glint $g
}
