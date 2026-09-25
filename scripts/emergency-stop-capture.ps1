$ErrorActionPreference = 'SilentlyContinue'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
$LogDir = Join-Path $Root 'data\logs'
$PauseFile = Join-Path $RunDir 'core-service.pause'
$PidFile = Join-Path $RunDir 'core-service.pid'
$EmergencyLog = Join-Path $LogDir 'emergency.log'

New-Item -ItemType Directory -Force -Path $RunDir | Out-Null
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
$now = (Get-Date).ToString('o')
Set-Content -LiteralPath $PauseFile -Value "$now manual emergency capture stop" -Encoding utf8
Add-Content -LiteralPath $EmergencyLog -Value ('{0} service=core-service reason="manual emergency capture stop"' -f $now)

$pidValue = 0
if (Test-Path -LiteralPath $PidFile) {
    $raw = (Get-Content -LiteralPath $PidFile -Raw).Trim()
    [void][int]::TryParse($raw, [ref]$pidValue)
}
if ($pidValue -le 0) {
    $line = netstat -ano | Select-String -Pattern '127\.0\.0\.1:8081\s+.*LISTENING\s+(\d+)$' | Select-Object -First 1
    if ($line -and $line.Matches.Count -gt 0) {
        $pidValue = [int]$line.Matches[0].Groups[1].Value
    }
}

if ($pidValue -gt 0) {
    & taskkill.exe /PID $pidValue /T /F | Out-Null
    Write-Host ("[EMERGENCY] Core/collector process tree killed, pid={0}." -f $pidValue)
} else {
    Write-Host '[EMERGENCY] Core process was not found; pause marker is still active.'
}
Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
Write-Host ("[PAUSED] Capture quarantine active: {0}" -f $PauseFile)
Write-Host '         Run scripts\resume-capture.ps1 after reviewing logs.'
