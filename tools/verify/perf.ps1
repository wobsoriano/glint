# Idle cost and leaks of a running Glint. -Mode start launches it in the tray on a copy of a
# Library and records its pid; sample prints CPU seconds, working set, private bytes, handles,
# GDI and USER objects and threads; idle samples over -Seconds and reports CPU %; library,
# editor and studio open and close that window -N times, sampling after each (desktop lock).
param(
    [Parameter(Mandatory)] [ValidateSet('start', 'sample', 'idle', 'library', 'editor', 'studio', 'stop')] [string] $Mode,
    [string] $Seed, [int] $N = 10, [int] $Seconds = 60
)
. "$PSScriptRoot\..\..\internal\app\cmd\glintcheck\scenarios\common.ps1"
if (-not ("GlintPerf.Gui" -as [type])) {
    Add-Type -Namespace GlintPerf -Name Gui -MemberDefinition '[DllImport("user32.dll")] public static extern uint GetGuiResources(IntPtr h, uint flags);'
}
$dir = Join-Path $env:GLINT_OUT 'perf'
$pidFile = Join-Path $dir 'pid.txt'
function Proc { Get-Process -Id ([int](Get-Content $pidFile)) }
function Sample([string]$label) {
    $p = Proc
    $p.Refresh()
    "{0,-22} cpu {1,8:N3} s  ws {2,7:N1} MB  private {3,7:N1} MB  handles {4,5}  gdi {5,4}  user {6,4}  threads {7,3}" -f $label, $p.TotalProcessorTime.TotalSeconds, ($p.WorkingSet64 / 1MB), ($p.PrivateMemorySize64 / 1MB), $p.HandleCount, [GlintPerf.Gui]::GetGuiResources($p.Handle, 0), [GlintPerf.Gui]::GetGuiResources($p.Handle, 1), $p.Threads.Count
}
$Ed = 'Glint ' + [char]0x2014 + ' '
switch ($Mode) {
    'start' {
        Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
        New-Item -ItemType Directory -Force "$dir\data" | Out-Null
        Copy-Item -Recurse $Seed "$dir\data\Library"
        @{ include_own_windows = $true; save_dir = "$dir\exports" } | ConvertTo-Json | Set-Content -Encoding ascii "$dir\data\settings.json"
        $env:GLINT_DATA_DIR = "$dir\data"
        # Glint inherits this process's handles, so run -Mode start outside a pipeline: a
        # piped caller would wait for Glint to exit.
        $p = Start-Process "$Bin\glint.exe" -ArgumentList '--background' -PassThru -RedirectStandardError "$dir\glint.err"
        $p.Id | Set-Content $pidFile
        Start-Sleep -Seconds 2
        Sample 'started'
    }
    'sample' { Sample 'now' }
    'idle' {
        $p = Proc
        $c0 = $p.TotalProcessorTime.TotalSeconds; $t0 = Get-Date
        Sample 'idle start'
        Start-Sleep -Seconds $Seconds
        $p.Refresh()
        $c1 = $p.TotalProcessorTime.TotalSeconds; $dt = ((Get-Date) - $t0).TotalSeconds
        Sample 'idle end'
        "idle over {0:N1} s: {1:N3} CPU s = {2:N3}% of one core ({3:N4}% of {4} logical cores)" -f $dt, ($c1 - $c0), (100 * ($c1 - $c0) / $dt), (100 * ($c1 - $c0) / $dt / [Environment]::ProcessorCount), [Environment]::ProcessorCount
    }
    'library' {
        $env:GLINT_DATA_DIR = "$dir\data"
        for ($i = 1; $i -le $N; $i++) {
            Start-Process "$Bin\glint.exe" -ArgumentList '--library' -NoNewWindow -Wait
            Wait-Until { (Win 'Glint Library') -ne [IntPtr]::Zero } 5000 | Out-Null
            Start-Sleep -Milliseconds 1500
            Close-Win 'Glint Library'
            Wait-Until { (Win 'Glint Library') -eq [IntPtr]::Zero } 3000 | Out-Null
            Start-Sleep -Milliseconds 700
            Sample "library cycle $i"
        }
    }
    'editor' {
        $env:GLINT_DATA_DIR = "$dir\data"
        $it = (Get-ChildItem "$dir\data\Library" -Directory | Where-Object Name -eq '2026-10-17_06-59-28_033f').FullName
        $title = (Meta $it).title
        for ($i = 1; $i -le $N; $i++) {
            Start-Process "$Bin\glint.exe" -ArgumentList "`"$it\image.png`"" -NoNewWindow -Wait
            Wait-Until { (Win "$Ed$title") -ne [IntPtr]::Zero } 5000 | Out-Null
            Start-Sleep -Milliseconds 1500
            Close-Win "$Ed$title"
            Wait-Until { (Win "$Ed$title") -eq [IntPtr]::Zero } 3000 | Out-Null
            Start-Sleep -Milliseconds 700
            Sample "editor cycle $i"
        }
    }
    'studio' {
        $env:GLINT_DATA_DIR = "$dir\data"
        $it = (Get-ChildItem "$dir\data\Library" -Directory | Where-Object Name -eq '2026-10-17_22-34-50_2c0a.glintrec').FullName
        $win = "$((Meta $it).title) - Glint Studio"
        for ($i = 1; $i -le $N; $i++) {
            Start-Process "$Bin\glint.exe" -ArgumentList "--studio `"$it`"" -NoNewWindow -Wait
            Wait-Until { (Win $win) -ne [IntPtr]::Zero } 8000 | Out-Null
            Start-Sleep -Milliseconds 2500
            Close-Win $win
            Wait-Until { (Win $win) -eq [IntPtr]::Zero } 4000 | Out-Null
            Start-Sleep -Milliseconds 700
            Sample "studio cycle $i"
        }
    }
    'stop' { Sample 'final'; Stop-Process -Id ([int](Get-Content $pidFile)) -Force }
}
