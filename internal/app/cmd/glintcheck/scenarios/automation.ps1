# After-capture steps: save as JPEG and as PNG into the export folder, a failed save with its
# dialog, and Glint's own windows kept out of captures unless include_own_windows is set.
. "$PSScriptRoot\common.ps1"

function Capture-Screen([string]$dir) {
    Keys 'Ctrl+Shift+3'
    Start-Sleep -Milliseconds 2000
    (Items $dir)[-1].FullName
}

Step 'save as JPEG at quality 70'
$dir = New-Run 'save-jpeg' @{ after_shot = @{ preview = $false; copy = $false; save = $true; edit = $false; pin = $false }; format = 'Jpeg'; jpeg_quality = 70 }
$g = Start-Glint $dir
$item = Capture-Screen $dir
Shot "$dir\01-saved-toast.png"
Get-ChildItem "$dir\exports" | ForEach-Object { "exported $($_.Name) ($($_.Length) bytes)" }
$jpg = (Get-ChildItem "$dir\exports\*.jpg")[0].FullName
Check cmp -a $jpg -b "$item\image.png" -maxmean 2
Stop-Glint $g

Step 'save as PNG'
$dir = New-Run 'save-png' @{ after_shot = @{ preview = $false; copy = $true; save = $true; edit = $false; pin = $false }; format = 'Png' }
$g = Start-Glint $dir
$item = Capture-Screen $dir
Get-ChildItem "$dir\exports" | ForEach-Object { "exported $($_.Name) ($($_.Length) bytes)" }
Check cmp -a (Get-ChildItem "$dir\exports\*.png")[0].FullName -b "$item\image.png"
Check clip -png "$dir\clipboard.png"
Check cmp -a "$dir\clipboard.png" -b "$item\image.png"
Stop-Glint $g

Step 'a save into a folder that cannot exist offers Retry or another folder'
$dir = New-Run 'save-fails' @{ after_shot = @{ preview = $false; copy = $false; save = $true; edit = $false; pin = $false } }
Set-Content "$dir\blocked" 'a file where the export folder should be'
$s = Get-Content "$dir\data\settings.json" | ConvertFrom-Json
$s.save_dir = "$dir\blocked\exports"
$s | ConvertTo-Json -Depth 5 | Set-Content -Encoding ascii "$dir\data\settings.json"
$g = Start-Glint $dir
Keys 'Ctrl+Shift+3'
Start-Sleep -Milliseconds 2500
Shot "$dir\01-save-failed-dialog.png"
Check wins -pid $g.Id
& $Check wins -pid $g.Id | Select-String 'visible=true.*"Glint"' | ForEach-Object { Keys 'Esc' }
Start-Sleep -Milliseconds 700
Shot "$dir\02-not-saved-toast.png"
Stop-Glint $g

# With no after-capture step no toast shows, so the Library window is what must be in or out
# of the capture: its area is compared with the same area once the Library has closed.
foreach ($include in $false, $true) {
    Step "include_own_windows=$($include): the Library showing during Ctrl+Shift+3"
    $dir = New-Run "exclude-$include" @{ include_own_windows = $include; after_shot = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false } }
    $g = Start-Glint $dir
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 5000 | Out-Null
    Start-Sleep -Milliseconds 800
    $r = WinRect 'Glint Library'
    $lib = "$($r.X + 20),$($r.Y + 60),$($r.W - 40),$($r.H - 100)"
    Check wins -pid $g.Id
    $item = Capture-Screen $dir
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 1200
    Check shot -out "$dir\behind-library.png" -rect $lib
    "the capture's Library area against the same area once the Library has closed:"
    & $Check cmp -a "$item\image.png" -arect $lib -b "$dir\behind-library.png" -brect "0,0,$($r.W - 40),$($r.H - 100)" -maxmean 1
    $inCapture = $LASTEXITCODE -ne 0
    "  -> the Library is in the capture: $inCapture (want $include)"
    Copy-Item "$item\image.png" "$dir\capture.png"
    Stop-Glint $g
}
