# Installs Glint with the installer mygo build made, checks what it installed and registered,
# starts the installed app in the tray, uninstalls it, and checks that the registry is as it
# was. Run it from the repository root after `go tool mygo build`:
#
#   powershell -NoProfile -File tools/packagecheck/install.ps1 -Scratch <empty temp dir>
#
# The installer goes into <Scratch>\Glint and the app runs on <Scratch>\data. An earlier
# Rust Glint's "Open with" entries, which the installed app removes, are put back afterwards.
param(
    [Parameter(Mandatory)] [string] $Scratch,
    [string] $Installer
)
$ErrorActionPreference = 'Stop'
$failed = $false
function Check([bool] $ok, [string] $what) {
    if ($ok) { "PASS $what" } else { "FAIL $what"; $script:failed = $true }
}

if (-not $Installer) {
    $Installer = (Get-ChildItem 'build\windows-amd64\Glint Setup *.exe' | Select-Object -First 1).FullName
}
New-Item -ItemType Directory -Force $Scratch | Out-Null
$Scratch = (Resolve-Path $Scratch).Path
$installDir = Join-Path $Scratch 'Glint'
$dataDir = Join-Path $Scratch 'data'
$exe = Join-Path $installDir 'Glint.exe'
$shortcut = Join-Path ([Environment]::GetFolderPath('Programs')) 'Glint.lnk'
$uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\com.glint.app'
$watched = @(
    'HKCU\Software\Classes',
    'HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall',
    'HKCU\Software\Microsoft\Windows\CurrentVersion\Run',
    'HKCU\Control Panel\NotifyIconSettings'
)

function Snapshot([string] $name) {
    $lines = foreach ($key in $watched) {
        $file = Join-Path $Scratch "$name-$($key -replace '[\\ ]', '_').reg"
        reg export $key $file /y | Out-Null
        Get-Content $file
    }
    $lines | Set-Content -Encoding utf8 (Join-Path $Scratch "$name.reg")
    , $lines
}

# Windows keeps caches here that change whenever any app runs: the resource strings of packaged
# apps, which listing the "Open with" handlers fills, their last-use times, and the order of the
# tray's icons, which Explorer rewrites from memory with icons that have left it. The order is
# checked on its own below.
function Quiet([string[]] $lines) {
    $section = ''
    $inOrder = $false
    foreach ($l in $lines) {
        if ($l -match '^\[') { $section = $l }
        $continued = $inOrder
        if ($l -match '^"UIOrderList"=') { $inOrder = $true }
        $skip = $inOrder -or $continued
        if ($inOrder -and -not $l.EndsWith('\')) { $inOrder = $false }
        if (-not $skip -and $section -notmatch '\\MrtCache\\' -and $l -notmatch '^"@\{' -and $l -notmatch '^"(PCT|PTT)"=hex\(b\)') { $l }
    }
}

function Compare-Registry([string[]] $before, [string[]] $after) {
    Compare-Object @(Quiet $before) @(Quiet $after) | ForEach-Object {
        $side = if ($_.SideIndicator -eq '=>') { '+' } else { '-' }
        "  $side $($_.InputObject)"
    }
}

Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;

[ComImport, Guid("F04061AC-1659-4a3f-A954-775AA57FC083"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAssocHandler {
    [PreserveSig] int GetName([MarshalAs(UnmanagedType.LPWStr)] out string name);
    [PreserveSig] int GetUIName([MarshalAs(UnmanagedType.LPWStr)] out string name);
}

[ComImport, Guid("973810ae-9599-4b88-9e4d-6ee98c9552da"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IEnumAssocHandlers {
    [PreserveSig] int Next(uint celt, out IAssocHandler handler, out uint fetched);
}

public static class OpenWith {
    [DllImport("shell32.dll", CharSet = CharSet.Unicode, PreserveSig = false)]
    static extern void SHAssocEnumHandlers(string extra, int filter, out IEnumAssocHandlers handlers);

    // The handlers Explorer lists under "Open with" for an extension, as "UI name|program".
    public static List<string> Handlers(string ext) {
        var list = new List<string>();
        IEnumAssocHandlers e;
        SHAssocEnumHandlers(ext, 0, out e);
        IAssocHandler h;
        uint n;
        while (e.Next(1, out h, out n) == 0 && n == 1) {
            string name, ui;
            h.GetName(out name);
            h.GetUIName(out ui);
            list.Add(ui + "|" + name);
        }
        return list;
    }
}
'@

$rustKeys = @('Glint.Image', 'Glint.Video', 'Applications\glint.exe')
$rustTypes = @('.png', '.jpg', '.jpeg', '.webp', '.bmp', '.gif', '.mp4', '.mov', '.m4v')
$rustBackup = Join-Path $Scratch 'rust-open-with.reg'
$rustValues = @()
"Windows Registry Editor Version 5.00" | Set-Content -Encoding unicode $rustBackup
foreach ($k in $rustKeys) {
    if (Test-Path "HKCU:\Software\Classes\$k") {
        $part = Join-Path $Scratch 'part.reg'
        reg export "HKCU\Software\Classes\$k" $part /y | Out-Null
        Get-Content $part | Select-Object -Skip 1 | Add-Content -Encoding unicode $rustBackup
    }
}
foreach ($t in $rustTypes) {
    foreach ($id in 'Glint.Image', 'Glint.Video') {
        $p = Get-ItemProperty "HKCU:\Software\Classes\$t\OpenWithProgids" -Name $id -ErrorAction SilentlyContinue
        if ($p) { $rustValues += , @($t, $id) }
    }
}
"Rust Glint registrations present before: $($rustValues.Count) OpenWithProgids values"

$before = Snapshot 'before'
"Open with .png before: $((([OpenWith]::Handlers('.png')) -match 'glint') -join '; ')"

Start-Process -Wait $Installer -ArgumentList '/S', "/D=$installDir"
Check (Test-Path $exe) "installed $exe"
Check (Test-Path (Join-Path $installDir 'Uninstall.exe')) 'installed Uninstall.exe'
"  files: $((Get-ChildItem $installDir -Recurse -File | ForEach-Object { $_.FullName.Substring($installDir.Length + 1) }) -join ', ')"
Check (Test-Path $shortcut) "Start menu shortcut $shortcut"
if (Test-Path $shortcut) {
    $target = (New-Object -ComObject WScript.Shell).CreateShortcut($shortcut).TargetPath
    Check ($target -eq $exe) "the shortcut starts $target"
}
Check (Test-Path $uninstallKey) 'listed in Settings > Apps'
foreach ($ext in 'png', 'jpg', 'mp4') {
    $cmd = (Get-ItemProperty "HKCU:\Software\Classes\com.glint.app.$ext\shell\open\command").'(default)'
    Check ($cmd -eq "`"$exe`" `"%1`"") "com.glint.app.$ext opens with $cmd"
    $listed = Get-ItemProperty "HKCU:\Software\Classes\.$ext\OpenWithProgids" -Name "com.glint.app.$ext" -ErrorAction SilentlyContinue
    Check ($null -ne $listed) ".$ext\OpenWithProgids lists com.glint.app.$ext"
}

$env:GLINT_DATA_DIR = $dataDir
$app = Start-Process $exe -ArgumentList '--background' -PassThru
Start-Sleep -Seconds 6
$app.Refresh()
Check (-not $app.HasExited) "the installed Glint runs (pid $($app.Id))"
$children = @(Get-CimInstance Win32_Process -Filter "ParentProcessId = $($app.Id)" | ForEach-Object Name)
Check (-not ($children -contains 'conhost.exe')) "no console host started for it (children: $($children -join ', '))"
$tray = @(Get-ChildItem 'HKCU:\Control Panel\NotifyIconSettings' | Where-Object { (Get-ItemProperty $_.PSPath).ExecutablePath -eq $exe })
Check ($tray.Count -ge 1) "Windows lists its tray icon ($($tray.PSChildName -join ', '))"
foreach ($ext in '.png', '.jpg', '.mp4') {
    $glint = @([OpenWith]::Handlers($ext) | Where-Object { $_ -match 'glint' })
    Check ($glint.Count -eq 1 -and $glint[0] -eq "Glint|$exe") "Open with $ext lists one Glint: $($glint -join '; ')"
}
"Registry keys and values that differ while installed and running:"
Compare-Registry $before (Snapshot 'installed') | Where-Object { $_ -match '^  [+-] (\[|")' -and $_ -notmatch 'MuiCache\]$' }
Stop-Process -Id $app.Id
$app.WaitForExit()

Start-Process -Wait (Join-Path $installDir 'Uninstall.exe') -ArgumentList '/S'
# The uninstaller reruns from a temporary copy and returns before it is done.
for ($i = 0; $i -lt 100 -and (Test-Path $installDir); $i++) { Start-Sleep -Milliseconds 200 }
Check (-not (Test-Path $installDir)) 'the install folder is gone'
Check (-not (Test-Path $shortcut)) 'the Start menu shortcut is gone'
Check (-not (Test-Path $uninstallKey)) 'the uninstall entry is gone'
foreach ($ext in 'png', 'jpg', 'mp4') {
    Check (-not (Test-Path "HKCU:\Software\Classes\com.glint.app.$ext")) "com.glint.app.$ext is gone"
}

# Windows remembers the tray icon, its place in the tray and the program's name; none of them
# is the installer's, but this run made them.
$notify = 'HKCU:\Control Panel\NotifyIconSettings'
foreach ($t in $tray) {
    Remove-Item -Recurse $t.PSPath
    $order = (Get-ItemProperty $notify).UIOrderList
    $kept = for ($i = 0; $i -lt $order.Length; $i += 8) {
        if ([BitConverter]::ToUInt64($order, $i) -ne [uint64]::Parse($t.PSChildName)) { $order[$i..($i + 7)] }
    }
    Set-ItemProperty $notify -Name UIOrderList -Value ([byte[]]$kept) -Type Binary
    $order = (Get-ItemProperty $notify).UIOrderList
    $left = for ($i = 0; $i -lt $order.Length; $i += 8) { [BitConverter]::ToUInt64($order, $i) }
    Check ($left -notcontains [uint64]::Parse($t.PSChildName)) "the tray order no longer lists icon $($t.PSChildName)"
}
$muiCache = 'HKCU:\Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache'
(Get-Item $muiCache).Property | Where-Object { $_.StartsWith($exe, [StringComparison]::OrdinalIgnoreCase) } |
    ForEach-Object { Remove-ItemProperty $muiCache -Name $_ }

$uninstalled = @(Compare-Registry $before (Snapshot 'uninstalled'))
"Registry lines that differ after uninstalling, before the Rust build's entries are put back:"
$uninstalled
if ($rustValues.Count -gt 0) {
    $removedByApp = @($uninstalled | Where-Object { $_ -match '^  - .*Glint\.(Image|Video)' })
    Check ($removedByApp.Count -ge 2 + $rustValues.Count) 'the installed app removed the Rust build''s Open with entries'
    # reg reports success on stderr, which this script's error preference would stop at.
    $ErrorActionPreference = 'Continue'
    reg import $rustBackup 2>$null
    $ErrorActionPreference = 'Stop'
    foreach ($v in $rustValues) {
        New-ItemProperty "HKCU:\Software\Classes\$($v[0])\OpenWithProgids" -Name $v[1] -Value '' -PropertyType String -Force | Out-Null
    }
}

$after = Snapshot 'after'
$diff = @(Compare-Registry $before $after)
Check ($diff.Count -eq 0) "HKCU Classes, Uninstall, Run and NotifyIconSettings are as before ($($diff.Count) lines differ)"
$diff
if ($failed) { exit 1 }
