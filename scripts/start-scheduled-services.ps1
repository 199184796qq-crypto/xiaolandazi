$ErrorActionPreference = 'Stop'
$tasks = @('LiveCompanion-Management','LiveCompanion-Core','LiveCompanion-Web')
foreach ($task in $tasks) {
    $existing = Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue
    if (-not $existing) { throw ('Scheduled task not found: ' + $task) }
    Start-ScheduledTask -TaskName $task
    Write-Host ('[OK] started {0}' -f $task)
}
