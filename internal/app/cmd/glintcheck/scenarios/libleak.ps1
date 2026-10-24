# Handles and USER objects over Library open and close cycles, and what the cycles left
# behind: kernel handles by type and name, and windows by class, diffed between a snapshot
# after -Warm cycles and one after -N more. -Open picks how the Library opens: 'cli' runs a
# second glint.exe --library, as tools/verify/perf.ps1 does; 'hotkey' presses Ctrl+Shift+L,
# so no second process talks to Glint. -Seed is a Library folder to copy.
param([string]$Seed, [int]$Warm = 5, [int]$N = 25, [ValidateSet('cli', 'hotkey')] [string]$Open = 'cli')
. "$PSScriptRoot\common.ps1"
if (-not ("GlintLeak.Gui" -as [type])) {
    Add-Type -Namespace GlintLeak -Name Gui -MemberDefinition '[DllImport("user32.dll")] public static extern uint GetGuiResources(IntPtr h, uint flags);'
}
$dir = New-Run "libleak-$Open"
if ($Seed) { Copy-Item -Recurse $Seed "$dir\data\Library" }
$g = Start-Glint $dir
function Sample([string]$label) {
    $g.Refresh()
    "{0,-12} handles {1,5}  gdi {2,4}  user {3,4}  threads {4,3}  private {5,6:N1} MB" -f $label, $g.HandleCount,
        [GlintLeak.Gui]::GetGuiResources($g.Handle, 0), [GlintLeak.Gui]::GetGuiResources($g.Handle, 1), $g.Threads.Count, ($g.PrivateMemorySize64 / 1MB)
}
function Cycle {
    if ($Open -eq 'cli') {
        Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
    } else {
        Keys 'Ctrl+Shift+L'
    }
    Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 5000 | Out-Null
    Start-Sleep -Milliseconds 1500
    Close-Win 'Glint Library'
    Wait-Until { (Win 'Glint Library') -eq [IntPtr]::Zero } 3000 | Out-Null
    Start-Sleep -Milliseconds 700
}
try {
    Sample 'started'
    for ($i = 1; $i -le $Warm; $i++) { Cycle }
    Sample "warm $Warm"
    & $Check handles -pid $g.Id > "$dir\before.txt"
    for ($i = 1; $i -le $N; $i++) {
        Cycle
        if ($i % 5 -eq 0) { Sample "cycle +$i" }
    }
    Start-Sleep -Milliseconds 1500
    Sample 'settled'
    & $Check handles -pid $g.Id > "$dir\after.txt"
    Step "left behind by $N cycles (+ after, - before)"
    $count = @{}
    Get-Content "$dir\before.txt" | ForEach-Object { $count[$_] = $count[$_] - 1 }
    Get-Content "$dir\after.txt" | ForEach-Object { $count[$_] = $count[$_] + 1 }
    $count.GetEnumerator() | Where-Object Value -ne 0 | Sort-Object { -[math]::Abs($_.Value) }, Name |
        ForEach-Object { "{0,4:+0;-0} {1}" -f $_.Value, $_.Name }
    # Thread start addresses map to a module through this list.
    $g.Modules | ForEach-Object { "{0:x} {1:x} {2}" -f [int64]$_.BaseAddress, $_.ModuleMemorySize, $_.ModuleName } > "$dir\modules.txt"
} finally {
    Stop-Glint $g
}
