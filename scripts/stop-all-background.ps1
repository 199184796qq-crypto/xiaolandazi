$ErrorActionPreference = 'SilentlyContinue'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'

function Stop-FromPidFile([string]$Name, [string]$PidFile) {
    if (-not (Test-Path $PidFile)) {
        Write-Host ("[SKIP] {0}: no pid file" -f $Name)
        return
    }

    $value = (Get-Content -LiteralPath $PidFile -Raw).Trim()
    $pidValue = 0
    if (-not [int]::TryParse($value, [ref]$pidValue)) {
        Write-Host ("[SKIP] {0}: invalid pid file" -f $Name)
        Remove-Item -LiteralPath $PidFile -Force
        return
    }

    $process = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
    if ($process) {
        & taskkill.exe /PID $pidValue /T /F | Out-Null
        Write-Host ("[OK] stopped process tree {0}, pid={1}" -f $Name, $pidValue)
    } else {
        Write-Host ("[SKIP] {0}: pid {1} is not running" -f $Name, $pidValue)
    }

    Remove-Item -LiteralPath $PidFile -Force -ErrorAction SilentlyContinue
}

Stop-FromPidFile 'web-console' (Join-Path $RunDir 'web-console.pid')
Stop-FromPidFile 'device-simulator' (Join-Path $RunDir 'device-simulator.pid')
Stop-FromPidFile 'core-service' (Join-Path $RunDir 'core-service.pid')
Stop-FromPidFile 'audio-service' (Join-Path $RunDir 'audio-service.pid')
Stop-FromPidFile 'management-service' (Join-Path $RunDir 'management-service.pid')
