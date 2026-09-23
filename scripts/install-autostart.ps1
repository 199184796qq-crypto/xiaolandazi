$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited

$definitions = @(
    @{ Name = 'LiveCompanion-Management'; Script = Join-Path $PSScriptRoot 'task-management.cmd' },
    @{ Name = 'LiveCompanion-Core'; Script = Join-Path $PSScriptRoot 'task-core.cmd' },
    @{ Name = 'LiveCompanion-Web'; Script = Join-Path $PSScriptRoot 'task-web.cmd' }
)

foreach ($definition in $definitions) {
    $command = '/d /s /c ""' + $definition.Script + '""'
    $action = New-ScheduledTaskAction -Execute $env:ComSpec -Argument $command -WorkingDirectory $Root
    $task = New-ScheduledTask -Action $action -Trigger $trigger -Settings $settings -Principal $principal
    Register-ScheduledTask -TaskName $definition.Name -InputObject $task -Force | Out-Null
    Write-Host ('[OK] registered {0}' -f $definition.Name)
}

Write-Host '[DONE] Auto-start tasks installed.'
Write-Host 'Use start-scheduled-services.ps1 to start them now.'
