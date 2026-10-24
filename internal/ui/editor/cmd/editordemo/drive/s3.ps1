. "$PSScriptRoot\common.ps1"
$saved = Pos
$env:MYGO_FRAME_STATS = "all"
$marks = "$S\s3.marks.txt"
Remove-Item $marks -ErrorAction SilentlyContinue
$d = Start-Demo "s3" @("-data", "$S\data3", "-capture", "-size", "2560x1440", "-seed", "30", "-frames", "$S\s3.frames.txt")
Start-Sleep -m 1500
WinShot "s3-open"
$pick = $null
foreach ($k in ($d.A.Keys | Sort-Object)) {
    if ($k -match '^ann\d+-edge$') {
        $p = $d.A[$k]
        if ($p[0] -gt 900 -and $p[0] -lt 1250 -and $p[1] -gt 450 -and $p[1] -lt 750) { $pick = $p; $name = $k; break }
    }
}
"picked $name $($pick[0]),$($pick[1])" | Out-File "$S\s3.picked.txt"
Keys "V"
for ($run = 0; $run -lt 2; $run++) {
    Glide $pick[0] $pick[1] 400; Start-Sleep -m 150; Down; Start-Sleep -m 100
    $t0 = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
    for ($k = 0; $k -lt 3; $k++) {
        Glide ($pick[0] + 200) ($pick[1] - 100) 500
        Glide ($pick[0] + 100) ($pick[1] + 120) 500
        Glide ($pick[0]) ($pick[1]) 500
    }
    $t1 = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
    Start-Sleep -m 100; Up
    "drag$run $t0 $t1" | Out-File -Append $marks
    Start-Sleep -m 800
    WinShot "s3-drag$run"
}
Keys "Ctrl+Enter"
Stop-Demo $d
Glide $saved[0] $saved[1] 200
