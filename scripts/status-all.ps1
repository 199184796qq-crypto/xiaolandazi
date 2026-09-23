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

Show-Service 'web-console' 5173 'http://127.0.0.1:5173/'
Show-Service 'management-service' 8080 'http://127.0.0.1:8080/api/v1/auth/captcha'
Show-Service 'core-service' 8081 'http://127.0.0.1:8081/healthz'
