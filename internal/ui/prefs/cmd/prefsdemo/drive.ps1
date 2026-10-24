# Drives prefsdemo on the real desktop, one step per run, inside the desktop lock:
#   go run ./tools/desklock -- powershell -NoProfile -STA -File internal/ui/prefs/cmd/prefsdemo/drive.ps1 -Step <step> -Copy <data dir> -Out <dir holding prefsdemo.exe>
# Steps: panes, toggles, shortcut, refuse, overlay. Coordinates are client DIPs of the
# 860x640 window at 100% scale.
param([string]$Step, [string]$Copy, [string]$Out)
. "$PSScriptRoot\..\..\..\library\cmd\librarydemo\desk.ps1"

$nav = [ordered]@{ General = 61; Screenshots = 97; Recordings = 133; Overlay = 169; Shortcuts = 205; Privacy = 241; About = 277 }
function Pane([string]$name) { At 66 $nav[$name]; Start-Sleep -m 300 }

$env:PREFSDEMO_REFUSE = if ($Step -eq "refuse") { "Ctrl+Alt+J" } else { "" }
Start-Demo "$Out\prefsdemo.exe" "Glint Settings" $Copy 860 640
switch ($Step) {
    "panes" {
        foreach ($name in $nav.Keys) { Pane $name; Snap $Out "prefs-$name" }
    }
    "toggles" {
        Pane Screenshots
        At 300 229; At 772 365; At 775 588
        Snap $Out "prefs-toggles-screenshots"
        Pane Privacy
        At 577 225
        Snap $Out "prefs-toggles-privacy"
    }
    "shortcut" {
        Pane Shortcuts
        At 763 139
        Keys "Ctrl+Alt+K"
        Snap $Out "prefs-shortcut"
        Keys "Ctrl+Alt+K"
    }
    "refuse" {
        Pane Shortcuts
        At 763 407
        Keys "Ctrl+Alt+J"
        Snap $Out "prefs-refuse"
    }
    "overlay" {
        Pane Overlay
        DragAt 517 426 662 346
        Snap $Out "prefs-overlay"
    }
}
Stop-Demo
