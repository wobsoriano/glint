# Automatic redaction on capture, Trim & Compress cutting the middle half of a video, and a
# recording with the microphone picked in the recording picker.
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
$off = @{ preview = $false; copy = $false; save = $false; edit = $false; pin = $false }
$g = $null
$secrets = $null
try {
    Step 'automatic redaction hides an email, a phone number, a card and an IP address'
    $dir = New-Run 'x-redact' @{ after_shot = $off; auto_redact = $true; redact_style = 'Blur' }
    $secrets = Start-Process powershell -ArgumentList '-NoProfile', '-File', "`"$PSScriptRoot\secrets.ps1`"" -PassThru -WindowStyle Hidden
    Wait-Until { (Win 'Glint secrets') -ne [IntPtr]::Zero } 8000 | Out-Null
    Start-Sleep -Milliseconds 800
    $g = Start-Glint $dir
    Capture-Area 1255 185 1885 565
    Start-Sleep -Milliseconds 2500
    Shot "$dir\01-redacted-toast.png"
    $it = (Items $dir)[-1].FullName
    Check edited -dir $it
    (Get-Content "$it\edit.json" | ConvertFrom-Json).annotations | ForEach-Object { "  $($_.shape.type) $($_.shape.kind) at $($_.shape.rect.x),$($_.shape.rect.y) $($_.shape.rect.w)x$($_.shape.rect.h)" }
    Copy-Item "$it\image.png" "$dir\02-redacted-image.png"
    Stop-Process -Id $secrets.Id -Force
    Stop-Glint $g

    Step 'Trim & Compress: keep 1 s to 3 s of a 4 s video'
    $dir = New-Run 'x-trim' @{ after_shot = $off }
    Check video -out "$dir\clip.mp4" -seconds 4 | Out-Null
    $g = Start-Glint $dir
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$dir\clip.mp4`"" -NoNewWindow -Wait
    $ok = Wait-Until { (Win 'Trim & Compress') -ne [IntPtr]::Zero } 6000
    if (-not $ok) { throw 'Trim & Compress did not open' }
    Start-Sleep -Milliseconds 1200
    Place 'Trim & Compress' 478 137 964 750 | Out-Null
    Keys 'Shift'
    Front 'Trim & Compress'
    Start-Sleep -Milliseconds 600
    Click 730 598 200; Start-Sleep -Milliseconds 300; Click 1093 542 200
    Click 1190 598 200; Start-Sleep -Milliseconds 300; Click 1184 542 200
    Start-Sleep -Milliseconds 400
    Shot "$dir\01-selection.png"
    Click 1376 851 200
    $done = Wait-Until { @(Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue).Count -ge 1 } 60000
    Start-Sleep -Milliseconds 800
    Shot "$dir\02-exported.png"
    "export written: $done"
    Get-ChildItem "$dir\exports\*.mp4" -ErrorAction SilentlyContinue | ForEach-Object { Check probe -file $_.FullName }
    Close-Win 'Trim & Compress'
    Stop-Glint $g

    Step 'a recording with the microphone picked in the picker'
    $dir = New-Run 'x-mic' @{ after_recording = @{ preview = $true; copy = $false; save = $false; studio = $false }; record = @{ target = 'Display'; area = $null; camera = $null; mic = $null; system_audio = $false; countdown = 0; teleprompter = $false; script = ''; prompter_lines = 3 } }
    $g = Start-Glint $dir
    Keys 'Ctrl+Shift+7'
    Start-Sleep -Milliseconds 900
    Click 963 976 200
    Start-Sleep -Milliseconds 500
    Click 666 918 200
    Start-Sleep -Milliseconds 800
    Shot "$dir\01-mic-picked.png"
    "settings.json record.mic: $((Get-Content "$dir\data\settings.json" | ConvertFrom-Json).record.mic)"
    Click 1281 976 200
    Start-Sleep -Milliseconds 1500
    Shot "$dir\02-after-record.png"
    Start-Sleep -Milliseconds 1500
    Keys 'Ctrl+Shift+7'
    Start-Sleep -Milliseconds 1500
    Shot "$dir\03-after-stop.png"
    Get-Content "$dir\glint.err"
    Wait-Until { @(Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec' -ErrorAction SilentlyContinue).Count -ge 1 } 15000 | Out-Null
    Start-Sleep -Milliseconds 800
    $rec = (Get-ChildItem "$dir\data\Library" -Directory -Filter '*.glintrec')[0].FullName
    "files: $((Get-ChildItem $rec | ForEach-Object { "$($_.Name) ($($_.Length))" }) -join ', ')"
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g }
    if ($secrets) { Stop-Process -Id $secrets.Id -Force -ErrorAction SilentlyContinue }
}
