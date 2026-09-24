$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
$ModeFile = Join-Path $RunDir 'runtime-mode.txt'
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

& (Join-Path $PSScriptRoot 'stop-all-background.ps1')
Start-Sleep -Seconds 1

$taskName = 'LiveCompanion-Supervisor'
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if (-not $task) {
    throw "Scheduled task not found: $taskName. Run install-autostart.ps1 first."
}

Enable-ScheduledTask -TaskName $taskName | Out-Null
Start-ScheduledTask -TaskName $taskName
Set-Content -LiteralPath $ModeFile -Value 'supervised' -Encoding ascii

Write-Host '[OK] Supervised mode active.'
Write-Host '     Go Supervisor owns Web, Management and Core health/restart.'
