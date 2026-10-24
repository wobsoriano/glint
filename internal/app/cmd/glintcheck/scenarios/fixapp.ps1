# Show in Explorer with two captures selected, Settings > About, a capture titled with 300
# characters saved from the editor, and the toast of an image that does not exist. -Seed is a
# Library whose two newest captures are screenshots, such as the realdata copy of
# tools/verify. The Library is placed at 0,0 1920x1032 and Settings at 80,60 860x640, at 100%.
param([Parameter(Mandatory)] [string]$Seed)
. "$PSScriptRoot\common.ps1"
$dir = New-Run 'fixapp'
Copy-Item -Recurse $Seed "$dir\data\Library"
$lib = "$dir\data\Library"
function CtrlClick([int]$x, [int]$y) { [Promo.In]::keybd_event(0x11, 0, 0, [UIntPtr]::Zero); Click $x $y 200; [Promo.In]::keybd_event(0x11, 0, 2, [UIntPtr]::Zero); Start-Sleep -Milliseconds 200 }
function LibraryExplorer {
    $url = ([Uri]$lib).AbsoluteUri
    @((New-Object -ComObject Shell.Application).Windows() | Where-Object { $_.LocationURL -eq $url })
}
$long = ('Quarterly report draft ' * 14).Substring(0, 300)
$item = (Get-ChildItem $lib -Directory | Where-Object { Test-Path "$($_.FullName)\image.png" } | Sort-Object Name)[0].FullName
$raw = [IO.File]::ReadAllText("$item\meta.json") -replace '"title"\s*:\s*(null|"(?:[^"\\]|\\.)*")', "`"title`": `"$long`""
[IO.File]::WriteAllText("$item\meta.json", $raw, (New-Object Text.UTF8Encoding $false))
$g = Start-Glint $dir
try {
    Step 'two captures selected: Show in Explorer selects both'
    Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Library' 0 0 1920 1032 | Out-Null
    Front 'Glint Library'
    Start-Sleep -Milliseconds 800
    Click 318 125 200
    CtrlClick 520 125
    Shot "$dir\01-two-selected.png"
    Click 1736 951 200
    $ok = Wait-Until { @(LibraryExplorer).Count -gt 0 } 8000
    Start-Sleep -Milliseconds 1500
    Shot "$dir\02-explorer.png"
    foreach ($w in LibraryExplorer) {
        $sel = @($w.Document.SelectedItems())
        "Explorer at the Library: $ok; selected $($sel.Count): $(($sel | ForEach-Object { $_.Name }) -join ', ')"
        $w.Quit()
    }
    Start-Sleep -Milliseconds 500
    "Explorer windows on the Library left open: $(@(LibraryExplorer).Count)"

    Step 'Settings > About names the version'
    Front 'Glint Library'
    Click 75 998 200
    Wait-Until { (Win 'Glint Settings') -ne [IntPtr]::Zero } 5000 | Out-Null
    Start-Sleep -Milliseconds 800
    Place 'Glint Settings' 80 60 860 640 | Out-Null
    Start-Sleep -Milliseconds 500
    Click 155 338 200
    Start-Sleep -Milliseconds 600
    & $Check shot -out "$dir\03-about.png" -rect '80,60,860,640' | Out-Null
    Close-Win 'Glint Settings'
    Close-Win 'Glint Library'
    Start-Sleep -Milliseconds 500

    Step 'a capture titled with 300 characters saves from the editor'
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$item\image.png`"" -NoNewWindow -Wait
    $ed = 'Glint ' + [char]0x2014 + ' '
    Wait-Until { (Win $ed) -ne [IntPtr]::Zero } 6000 | Out-Null
    Start-Sleep -Milliseconds 1200
    Front $ed
    Start-Sleep -Milliseconds 300
    Keys 'Ctrl+S'
    Wait-Until { @(Get-ChildItem "$dir\exports" -File -ErrorAction SilentlyContinue).Count -gt 0 } 6000 | Out-Null
    Start-Sleep -Milliseconds 400
    Shot "$dir\04-saved.png"
    foreach ($f in Get-ChildItem "$dir\exports" -File) { "saved $($f.Name.Length) characters: $($f.Name)" }
    "title in meta.json: $(((Get-Content "$item\meta.json" -Raw | ConvertFrom-Json).title).Length) characters"
    Close-Win $ed
    Start-Sleep -Milliseconds 600

    Step 'an image that does not exist: the toast leads with the reason'
    Start-Process "$Bin\glint.exe" -ArgumentList "`"$dir\missing shot.png`"" -NoNewWindow -Wait
    Start-Sleep -Milliseconds 900
    Shot "$dir\05-toast.png"
} finally {
    Glide 1500 880 200
    Stop-Glint $g
    Get-Content "$dir\glint.err"
}
