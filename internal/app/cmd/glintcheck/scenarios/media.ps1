# Videos through the running app: glint.exe <an mp4> opens Trim & Compress, and a recording
# opened in Quick Look from the Library plays its picture and its sound. The recording comes
# from internal/media/cmd/record with system audio, whose tone bursts land in system.wav;
# loopback capture of the output device hears them again during playback. GLINT_BIN must
# also hold record.exe (go build -o record.exe ./internal/media/cmd/record).
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'media' @{ after_shot = @{ preview = $false; copy = $false; save = $false } }
$lib = "$dir\data\Library"
$g = $null
try {
    Step 'a 6 s display recording with system audio, made by the recorder into the Library'
    & "$Bin\record.exe" -source display -seconds 6 -system -out $lib | Select-Object -Last 4
    $rec = @(Get-ChildItem $lib -Directory -Filter '*.glintrec')
    "recordings: $($rec.Count); files: $((Get-ChildItem $rec[0].FullName).Name -join ', ')"

    $g = Start-Glint $dir
    Step 'glint.exe <mp4> opens Trim & Compress in the running Glint'
    Check video -out "$dir\clip.mp4" -seconds 3
    $q = Start-Process "$Bin\glint.exe" -ArgumentList "`"$dir\clip.mp4`"" -PassThru -NoNewWindow -Wait
    $ok = Wait-Until { (Win 'Trim & Compress') -ne [IntPtr]::Zero } 5000
    Start-Sleep -Milliseconds 1500
    "second instance exited $($q.ExitCode); Trim & Compress shows: $ok; foreground: $((& $Check wins -title 'Trim & Compress' | Select-Object -First 1))"
    # The script's process is not a foreground app the user opened the file from, so the
    # window opens behind; a key press entitles the script to bring it up for the screenshot.
    Keys 'Shift'
    Front 'Trim & Compress'
    Start-Sleep -Milliseconds 500
    Shot "$dir\01-trim.png"
    Close-Win 'Trim & Compress'
    Start-Sleep -Milliseconds 500

    Step 'the output device is quiet before playback'
    Check loopback -seconds 2 -silent

    Step 'the Library opens the recording in Quick Look, which plays it with its sound'
    Keys 'Ctrl+Shift+L'
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 80 60 1180 760 | Out-Null
    Start-Sleep -Milliseconds 800
    $title = (Meta $rec[0].FullName).title
    DClick 410 185 300
    $ok = Wait-Until { (Win $title) -ne [IntPtr]::Zero }
    "Quick Look '$title' shows: $ok"
    Check loopback -seconds 5
    $r = WinRect $title
    Check shot -out "$dir\02-playing-a.png" -rect "$($r.X),$($r.Y),$($r.W),$($r.H)"
    Start-Sleep -Milliseconds 700
    Check shot -out "$dir\02-playing-b.png" -rect "$($r.X),$($r.Y),$($r.W),$($r.H)"
    Shot "$dir\02-quicklook.png"
    Step 'frames advance: two shots of Quick Look 0.7 s apart differ'
    Check cmp -a "$dir\02-playing-a.png" -b "$dir\02-playing-b.png" -differ
    Front $title
    Keys 'Esc'
} finally {
    Glide 1500 880 200
    if ($g) { Stop-Glint $g; Get-Content "$dir\glint.err" }
}
