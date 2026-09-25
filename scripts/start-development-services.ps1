$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited

function Get-PortPid([int]$Port) {
    $line = netstat -ano |
        Select-String -Pattern ("127\.0\.0\.1:" + $Port + "\s+.*LISTENING\s+(\d+)$") |
        Select-Object -First 1
    if (-not $line -or $line.Matches.Count -lt 1) { return $null }
    return [int]$line.Matches[0].Groups[1].Value
}

function Wait-Port([int]$Port, [int]$TimeoutSeconds = 30) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $pidValue = Get-PortPid $Port
        if ($pidValue) { return $pidValue }
        Start-Sleep -Milliseconds 300
    } while ((Get-Date) -lt $deadline)
    return $null
}

function Ensure-DevTask([string]$TaskName, [string]$ScriptPath, [string[]]$ExtraArguments) {
    $argumentParts = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', ('"' + $ScriptPath + '"'))
    foreach ($arg in $ExtraArguments) {
        $argumentParts += $arg
    }
    $arguments = $argumentParts -join ' '
    $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument $arguments -WorkingDirectory $Root
    $task = New-ScheduledTask -Action $action -Settings $settings -Principal $principal
    Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
}

$backendRunner = Join-Path $PSScriptRoot 'run-service-with-local-env.ps1'
$webRunner = Join-Path $PSScriptRoot 'run-web-task.ps1'
$deviceRunner = Join-Path $PSScriptRoot 'run-device-simulator-task.ps1'

Ensure-DevTask 'LiveCompanion-Dev-Management' $backendRunner @('-Service', 'management')
Ensure-DevTask 'LiveCompanion-Dev-Core' $backendRunner @('-Service', 'core')
Ensure-DevTask 'LiveCompanion-Dev-Audio' $backendRunner @('-Service', 'audio')
Ensure-DevTask 'LiveCompanion-Dev-Web' $webRunner @()
Ensure-DevTask 'LiveCompanion-Dev-DeviceSimulator' $deviceRunner @()

$services = @(
    @{
        Name = 'management-service'
        Task = 'LiveCompanion-Dev-Management'
        Port = 8080
        PidFile = Join-Path $RunDir 'management-service.pid'
    },
    @{
        Name = 'core-service'
        Task = 'LiveCompanion-Dev-Core'
        Port = 8081
        PidFile = Join-Path $RunDir 'core-service.pid'
    },
    @{
        Name = 'audio-service'
        Task = 'LiveCompanion-Dev-Audio'
        Port = 8082
        PidFile = Join-Path $RunDir 'audio-service.pid'
    },
    @{
        Name = 'web-console'
        Task = 'LiveCompanion-Dev-Web'
        Port = 5173
        PidFile = Join-Path $RunDir 'web-console.pid'
    },
    @{
        Name = 'device-simulator'
        Task = 'LiveCompanion-Dev-DeviceSimulator'
        Port = 5176
        PidFile = Join-Path $RunDir 'device-simulator.pid'
    }
)

foreach ($service in $services) {
    $existingPid = Get-PortPid $service.Port
    if ($existingPid) {
        Set-Content -LiteralPath $service.PidFile -Value $existingPid -Encoding ascii
        Write-Host ("[SKIP] {0} already listening on {1}, pid={2}" -f $service.Name, $service.Port, $existingPid)
        continue
    }

    Start-ScheduledTask -TaskName $service.Task
    $pidValue = Wait-Port $service.Port
    if (-not $pidValue) {
        $taskInfo = Get-ScheduledTaskInfo -TaskName $service.Task -ErrorAction SilentlyContinue
        $lastResult = if ($taskInfo) { $taskInfo.LastTaskResult } else { 'unknown' }
        throw ("{0} failed to listen on {1}; scheduled task last result={2}" -f $service.Name, $service.Port, $lastResult)
    }
    Set-Content -LiteralPath $service.PidFile -Value $pidValue -Encoding ascii
    Write-Host ("[OK] {0} listening on {1}, pid={2}, owner=TaskScheduler" -f $service.Name, $service.Port, $pidValue)
}

Write-Host ''
Write-Host '[DEV] Development services are running under Windows Task Scheduler.'
Write-Host '      No watchdog/restart policy is enabled.'
Write-Host '      Processes are not tied to the WebCodex runner timeout.'
