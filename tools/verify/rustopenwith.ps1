# A packaged Glint.exe deletes the Rust build's "Open with" registrations whenever it starts,
# installed or not. Back them up before a verification pass that runs the packaged exe, restore
# them after, and report what is there:
#   powershell -NoProfile -File tools/verify/rustopenwith.ps1 -Mode backup|restore|show -Dir <scratch>
param([Parameter(Mandatory)] [ValidateSet('backup', 'restore', 'show')] [string] $Mode, [Parameter(Mandatory)] [string] $Dir)
$keys = @('Glint.Image', 'Glint.Video', 'Applications\glint.exe')
$types = @('.png', '.jpg', '.jpeg', '.webp', '.bmp', '.gif', '.mp4', '.mov', '.m4v')
$values = Join-Path $Dir 'rust-openwith-values.txt'

function Show {
    foreach ($k in $keys) { "HKCU\Software\Classes\$k present: $(Test-Path "HKCU:\Software\Classes\$k")" }
    foreach ($t in $types) {
        $p = Get-ItemProperty "HKCU:\Software\Classes\$t\OpenWithProgids" -ErrorAction SilentlyContinue
        $ids = if ($p) { @($p.PSObject.Properties.Name | Where-Object { $_ -like 'Glint.*' }) } else { @() }
        "$t OpenWithProgids Glint values: $($ids -join ', ')"
    }
}

switch ($Mode) {
    'backup' {
        New-Item -ItemType Directory -Force $Dir | Out-Null
        $i = 0
        foreach ($k in $keys) {
            if (Test-Path "HKCU:\Software\Classes\$k") { reg export "HKCU\Software\Classes\$k" (Join-Path $Dir "rust-openwith-$i.reg") /y | Out-Null }
            $i++
        }
        $lines = foreach ($t in $types) {
            $p = Get-ItemProperty "HKCU:\Software\Classes\$t\OpenWithProgids" -ErrorAction SilentlyContinue
            if ($p) { $p.PSObject.Properties.Name | Where-Object { $_ -like 'Glint.*' } | ForEach-Object { "$t|$_" } }
        }
        $lines | Set-Content -Encoding utf8 $values
        Show
    }
    'restore' {
        Get-ChildItem (Join-Path $Dir 'rust-openwith-*.reg') | ForEach-Object { reg import $_.FullName 2>$null }
        Get-Content $values | ForEach-Object {
            $t, $id = $_ -split '\|'
            New-ItemProperty "HKCU:\Software\Classes\$t\OpenWithProgids" -Name $id -Value '' -PropertyType String -Force | Out-Null
        }
        Show
    }
    'show' { Show }
}
