# Drives librarydemo on the real desktop, one step per run, inside the desktop lock:
#   go run ./tools/desklock -- powershell -NoProfile -STA -File internal/ui/library/cmd/librarydemo/drive.ps1 -Step <step> -Copy <copy of a data dir> -Out <dir holding librarydemo.exe>
# Steps: look, select, quicklook, rename, trash, live (-Arg: a capture directory to add), prefs.
# The grid coordinates assume the ten captures of the Library copied on 2026-10-23.
param([string]$Step, [string]$Copy, [string]$Out, [string]$Arg = "")
. "$PSScriptRoot\desk.ps1"

function OpenLib { Start-Demo "$Out\librarydemo.exe" "Glint Library" $Copy 1180 760 }

OpenLib
switch ($Step) {
    "look" { Snap $Out "lib-open" }
    "prefs" {
        At 786 24; At 960 24; At 80 130
        At 322 24; TypeText "22.36" 40
        Snap $Out "lib-prefs"
        Keys "Esc"
        Stop-Demo
        OpenLib
        Snap $Out "lib-prefs-reopened"
    }
    "select" {
        At 322 124
        Hold Ctrl { At 547 124 }
        Hold Ctrl { At 322 452 }
        Snap $Out "lib-select"
        Keys "Ctrl+C"
        Start-Sleep -m 300
        Add-Type -AssemblyName System.Windows.Forms
        "clipboard: " + (([System.Windows.Forms.Clipboard]::GetFileDropList()) -join " | ")
    }
    "quicklook" {
        DAt 772 124
        Keys "Space"; Keys "Space"
        Snap $Out "lib-quicklook"
    }
    "rename" {
        At 547 124
        Keys "Enter"; Start-Sleep -m 200
        Keys "Ctrl+A"; TypeText "Renamed by test" 30; Keys "Enter"
        Snap $Out "lib-rename"
    }
    "trash" {
        At 772 452
        Keys "Del"
        Snap $Out "lib-trash-ask"
        Keys "Enter"
        Snap $Out "lib-trash-done"
    }
    "live" {
        Snap $Out "lib-live-before"
        $staged = "$Out\2026-10-23_21-00-00_beef"
        Remove-Item -Recurse -Force $staged -ErrorAction SilentlyContinue
        Copy-Item -Recurse $Arg $staged
        $meta = Get-Content -Raw "$staged\meta.json" | ConvertFrom-Json
        $meta.title = "Live added"
        [IO.File]::WriteAllText("$staged\meta.json", ($meta | ConvertTo-Json))
        Move-Item $staged "$Copy\Library\"
        Start-Sleep -m 800
        Snap $Out "lib-live-after"
    }
}
Stop-Demo
