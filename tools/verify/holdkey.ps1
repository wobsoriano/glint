# Holds Ctrl+Alt+J as a global hotkey for -Seconds, as another app would, and writes whether
# Windows granted it to -Status.
param([Parameter(Mandatory)] [string] $Status, [int] $Seconds = 90)
Add-Type -Namespace Hold -Name Key -MemberDefinition '[DllImport("user32.dll")] public static extern bool RegisterHotKey(IntPtr h, int id, uint mods, uint vk);'
"held: $([Hold.Key]::RegisterHotKey([IntPtr]::Zero, 7, 3, 0x4A))" | Set-Content $Status
Start-Sleep -Seconds $Seconds
