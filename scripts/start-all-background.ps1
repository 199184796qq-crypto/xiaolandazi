& (Join-Path $PSScriptRoot 'start-development-services.ps1')
exit $LASTEXITCODE

<#
Legacy detached launcher kept below for history. The wrapper above intentionally
uses Windows Task Scheduler so development services are not children of the
WebCodex runner and therefore are not killed by its execution timeout.
#>

$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LogDir = Join-Path $Root 'data\logs'
$RunDir = Join-Path $Root 'data\run'
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

function Import-LocalEnvFile([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return $false }

    foreach ($line in [System.IO.File]::ReadAllLines($Path)) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }

        $idx = $line.IndexOf('=')
        if ($idx -le 0) { continue }

        $name = $line.Substring(0, $idx).Trim()
        $value = $line.Substring($idx + 1)
        if ([string]::IsNullOrWhiteSpace($name)) { continue }

        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }

    return $true
}

$cloudDevConfig = Join-Path $Root 'configs\cloud-dev.local'
if (Import-LocalEnvFile $cloudDevConfig) {
    Write-Host '[ENV] Loaded configs\cloud-dev.local for development services.'
}

function Get-PortPid([int]$Port) {
    $line = netstat -ano |
        Select-String -Pattern ("127\.0\.0\.1:" + $Port + "\s+.*LISTENING\s+(\d+)$") |
        Select-Object -First 1
    if (-not $line) { return $null }
    if ($line.Matches.Count -lt 1) { return $null }
    return [int]$line.Matches[0].Groups[1].Value
}

function Wait-Port([int]$Port, [int]$TimeoutSeconds = 20) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        if (Get-PortPid $Port) { return $true }
        Start-Sleep -Milliseconds 300
    } while ((Get-Date) -lt $deadline)
    return $false
}

function Start-ServiceProcess(
    [string]$Name,
    [int]$Port,
    [string]$FilePath,
    [string[]]$ArgumentList,
    [string]$WorkingDirectory,
    [string]$PidFile
) {
    $existing = Get-PortPid $Port
    if ($existing) {
        Write-Host ("[SKIP] {0} already listening on {1}, pid={2}" -f $Name, $Port, $existing)
        Set-Content -LiteralPath $PidFile -Value $existing -Encoding ascii
        return
    }

    $stdout = Join-Path $LogDir ($Name + '.stdout.log')
    $stderr = Join-Path $LogDir ($Name + '.stderr.log')

    $params = @{
        FilePath = $FilePath
        WorkingDirectory = $WorkingDirectory
        RedirectStandardOutput = $stdout
        RedirectStandardError = $stderr
        WindowStyle = 'Hidden'
        PassThru = $true
    }
    if ($ArgumentList -and $ArgumentList.Count -gt 0) {
        $params.ArgumentList = $ArgumentList
    }

    $process = Start-Process @params
    Set-Content -LiteralPath $PidFile -Value $process.Id -Encoding ascii

    if (-not (Wait-Port $Port)) {
        throw ("{0} failed to listen on port {1}. See {2} and {3}" -f $Name, $Port, $stdout, $stderr)
    }

    $pidNow = Get-PortPid $Port
    Write-Host ("[OK] {0} listening on {1}, pid={2}" -f $Name, $Port, $pidNow)
    if ($pidNow) {
        Set-Content -LiteralPath $PidFile -Value $pidNow -Encoding ascii
    }
}

$managementExe = Join-Path $Root 'management-service\bin\management-service.exe'
$coreExe = Join-Path $Root 'core-service\bin\core-service.exe'

if (-not (Test-Path $managementExe)) { throw "Management binary not found: $managementExe" }
if (-not (Test-Path $coreExe)) { throw "Core binary not found: $coreExe" }

$env:MGMT_AVATAR_DIR = Join-Path $Root 'data\avatars'
New-Item -ItemType Directory -Force -Path $env:MGMT_AVATAR_DIR | Out-Null

Start-ServiceProcess -Name 'management-service' -Port 8080 -FilePath $managementExe -ArgumentList @() -WorkingDirectory $Root -PidFile (Join-Path $RunDir 'management-service.pid')
Start-ServiceProcess -Name 'core-service' -Port 8081 -FilePath $coreExe -ArgumentList @() -WorkingDirectory $Root -PidFile (Join-Path $RunDir 'core-service.pid')

$webCommand = 'pnpm.cmd -C "' + $Root + '" --filter web-console dev --host 127.0.0.1 --port 5173'
Start-ServiceProcess -Name 'web-console' -Port 5173 -FilePath $env:ComSpec -ArgumentList @('/d', '/s', '/c', ('"' + $webCommand + '"')) -WorkingDirectory $Root -PidFile (Join-Path $RunDir 'web-console.pid')

Write-Host ''
Write-Host '[DONE] Live Companion background services are running.'
Write-Host '       Web:        http://127.0.0.1:5173'
Write-Host '       Management: http://127.0.0.1:8080'
Write-Host '       Core:       http://127.0.0.1:8081'
