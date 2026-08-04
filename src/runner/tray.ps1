Add-Type -AssemblyName System.Windows.Forms
$icon = New-Object System.Windows.Forms.NotifyIcon
$icon.Text = 'ProjectBoard Runner'
$icon.Icon = [System.Drawing.SystemIcons]::Application
$menu = New-Object System.Windows.Forms.ContextMenuStrip
foreach ($entry in @(@('Poll now','poll'),@('Pause','pause'),@('Resume','resume'))) {
  $item = $menu.Items.Add($entry[0]); $command = $entry[1]
  $item.Add_Click({ Start-Process -WindowStyle Hidden -FilePath 'projectboard-runner' -ArgumentList $command })
}
$exit = $menu.Items.Add('Exit'); $exit.Add_Click({ $icon.Visible = $false; [System.Windows.Forms.Application]::Exit() })
$icon.ContextMenuStrip = $menu; $icon.Visible = $true
[System.Windows.Forms.Application]::Run()
