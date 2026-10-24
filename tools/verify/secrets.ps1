# A topmost window of made-up sensitive text for redaction checks, titled "Glint secrets".
Add-Type -AssemblyName System.Windows.Forms, System.Drawing
$f = New-Object Windows.Forms.Form
$f.Text = 'Glint secrets'; $f.TopMost = $true; $f.StartPosition = 'Manual'
$f.Location = New-Object Drawing.Point 1250, 150; $f.Size = New-Object Drawing.Size 640, 420
$f.BackColor = [Drawing.Color]::White
$l = New-Object Windows.Forms.Label
$l.Dock = 'Fill'; $l.Font = New-Object Drawing.Font 'Segoe UI', 20
$l.Text = "Email jane.doe@example.com`nCall (415) 555-0134`nCard 4111 1111 1111 1111`nServer 192.168.10.42`nNothing secret here"
$f.Controls.Add($l)
# The first ShowWindow takes the hidden state that powershell -WindowStyle Hidden passes on.
$f.Show(); $f.Hide(); $f.Show()
[Windows.Forms.Application]::Run($f)
