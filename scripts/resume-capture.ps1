$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
$LogDir = Join-Path $Root 'data\logs'
$PauseFile = Join-Path $RunDir 'core-service.pause'
$EmergencyLog = Join-Path $LogDir 'emergency.log'

Remove-Item -LiteralPath $PauseFile -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
$now = (Get-Date).ToString('o')
Add-Content -LiteralPath $EmergencyLog -Value ('{0} service=core-service reason="manual capture resume"' -f $now)

$task = Get-ScheduledTask -TaskName 'LiveCompanion-Supervisor' -ErrorAction SilentlyContinue
if (-not $task) {
    throw 'LiveCompanion-Supervisor is not installed. Run scripts\install-autostart.ps1 first.'
}
if ($task.State -ne 'Running') {
    Enable-ScheduledTask -TaskName 'LiveCompanion-Supervisor' | Out-Null
    Start-ScheduledTask -TaskName 'LiveCompanion-Supervisor'
}
Write-Host '[OK] Capture quarantine removed. Supervisor will restore Core on the next health cycle.'
