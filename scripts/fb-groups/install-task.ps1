# Registers (or refreshes) the Windows scheduled task that runs the Facebook
# groups runner at 09:30 Monday to Saturday, only while the user is logged on
# (the browser profile is the user's). Run from any directory:
#   powershell -ExecutionPolicy Bypass -File scripts\fb-groups\install-task.ps1
# Remove with:  Unregister-ScheduledTask -TaskName 'Helderberg Social FB groups' -Confirm:$false
$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$node = (Get-Command node).Source
$name = 'Helderberg Social FB groups'
$action = New-ScheduledTaskAction -Execute $node -Argument 'post.mjs' -WorkingDirectory $here
$trigger = New-ScheduledTaskTrigger -Weekly -DaysOfWeek Monday, Tuesday, Wednesday, Thursday, Friday, Saturday -At 09:30
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -ExecutionTimeLimit (New-TimeSpan -Hours 2) -MultipleInstances IgnoreNew
# Interactive token: the run happens in the logged-on session so the browser window can show.
$principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -Settings $settings -Principal $principal -Force | Out-Null
Write-Host "Registered '$name': $node post.mjs in $here, 09:30 Mon-Sat while logged on."
Write-Host "Check: Get-ScheduledTask -TaskName '$name' | Get-ScheduledTaskInfo"
