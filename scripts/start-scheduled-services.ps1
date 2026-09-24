$ErrorActionPreference = 'Stop'

$taskName = 'LiveCompanion-Supervisor'
$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if (-not $existing) {
    throw ('Scheduled task not found: ' + $taskName)
}

Start-ScheduledTask -TaskName $taskName
Write-Host ('[OK] started {0}' -f $taskName)
Write-Host '[INFO] Go supervisor now owns web, management and core health/restart.'
