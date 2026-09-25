$ErrorActionPreference = 'Continue'

function Get-PortPid([int]$Port) {
    $line = netstat -ano |
        Select-String -Pattern ("127\.0\.0\.1:" + $Port + "\s+.*LISTENING\s+(\d+)$") |
        Select-Object -First 1
    if (-not $line) { return $null }
    if ($line.Matches.Count -lt 1) { return $null }
    return [int]$line.Matches[0].Groups[1].Value
}

function Show-Service([string]$Name, [int]$Port, [string]$Url) {
    $pidValue = Get-PortPid $Port
    if (-not $pidValue) {
        Write-Host ("[DOWN] {0,-20} port={1}" -f $Name, $Port)
        return
    }

    $process = Get-Process -Id $pidValue -ErrorAction SilentlyContinue
    $processName = if ($process) { $process.ProcessName } else { '?' }

    try {
        $response = Invoke-WebRequest -Uri $Url -Method Head -UseBasicParsing -TimeoutSec 3
        Write-Host ("[UP]   {0,-20} port={1} pid={2} process={3} http={4}" -f $Name, $Port, $pidValue, $processName, [int]$response.StatusCode)
    } catch {
        Write-Host ("[UP]   {0,-20} port={1} pid={2} process={3} http=ERR" -f $Name, $Port, $pidValue, $processName)
    }
}

$RunDir = Join-Path (Resolve-Path (Join-Path $PSScriptRoot '..')).Path 'data\run'
$modeFile = Join-Path $RunDir 'runtime-mode.txt'
$runtimeMode = if (Test-Path $modeFile) {
    (Get-Content -LiteralPath $modeFile -Raw).Trim()
} else {
    'unknown'
}

$supervisor = Get-ScheduledTask -TaskName 'LiveCompanion-Supervisor' -ErrorAction SilentlyContinue
if ($runtimeMode -eq 'development') {
    $taskState = if ($supervisor) { $supervisor.State } else { 'missing' }
    Write-Host ("[DEV]  {0,-20} watchdog=off task={1}" -f 'runtime-mode', $taskState)
} elseif ($supervisor) {
    Write-Host ("[SUP]  {0,-20} state={1}" -f 'supervisor', $supervisor.State)
} else {
    Write-Host ("[DOWN] {0,-20} task=missing" -f 'supervisor')
}

Show-Service 'web-console' 5173 'http://127.0.0.1:5173/'
Show-Service 'customer-mobile' 5174 'http://127.0.0.1:5174/login'
Show-Service 'sales-mobile' 5175 'http://127.0.0.1:5175/login'
Show-Service 'device-simulator' 5176 'http://127.0.0.1:5176/'
Show-Service 'management-service' 8080 'http://127.0.0.1:8080/api/v1/auth/captcha'
Show-Service 'core-service' 8081 'http://127.0.0.1:8081/healthz'
Show-Service 'audio-service' 8082 'http://127.0.0.1:8082/healthz'
