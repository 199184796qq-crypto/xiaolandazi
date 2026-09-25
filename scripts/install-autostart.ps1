$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$supervisorExe = Join-Path (Join-Path $Root 'supervisor/bin') 'livecompanion-supervisor.exe'
$configPath = Join-Path (Join-Path $Root 'configs') 'supervisor.json'
$supervisorLauncher = Join-Path $PSScriptRoot 'run-supervisor-with-local-env.ps1'

& (Join-Path $PSScriptRoot 'build-supervisor.ps1')

$legacyTasks = @('LiveCompanion-Management','LiveCompanion-Core','LiveCompanion-Web','LiveCompanion-Watchdog')
foreach ($taskName in $legacyTasks) {
    $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($existing) {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
        Write-Host ('[OK] removed legacy task {0}' -f $taskName)
    }
}

$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
$arguments = '-NoProfile -ExecutionPolicy Bypass -File "' + $supervisorLauncher + '"'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arguments -WorkingDirectory $Root
$task = New-ScheduledTask -Action $action -Trigger $trigger -Settings $settings -Principal $principal

Register-ScheduledTask -TaskName 'LiveCompanion-Supervisor' -InputObject $task -Force | Out-Null

Write-Host '[DONE] LiveCompanion-Supervisor installed.'
Write-Host 'The Go supervisor is the only logon task and owns Web, Customer Mobile, Sales Mobile, Management and Core.'
Write-Host 'Local cloud environment is loaded before Supervisor starts, when configs/cloud-dev.local exists.'
