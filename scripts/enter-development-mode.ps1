$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
$ModeFile = Join-Path $RunDir 'runtime-mode.txt'
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

$taskName = 'LiveCompanion-Supervisor'
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($task) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Disable-ScheduledTask -TaskName $taskName | Out-Null
    Write-Host '[OK] Go Supervisor stopped and disabled for development.'
}

Start-Sleep -Seconds 1
Get-Process -Name 'livecompanion-supervisor' -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Set-Content -LiteralPath $ModeFile -Value 'development' -Encoding ascii
& (Join-Path $PSScriptRoot 'start-development-services.ps1')

Write-Host ''
Write-Host '[DEV] Development mode active.'
Write-Host '      Supervisor will not restart services while code is being rebuilt.'
Write-Host '      Services are controlled manually by the development scripts / programming backend.'
