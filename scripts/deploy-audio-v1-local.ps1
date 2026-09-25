$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data\run'
$Task = 'LiveCompanion-Supervisor'
$Launcher = Join-Path $Root 'scripts\run-supervisor-with-local-env.ps1'
$SupervisorExe = [IO.Path]::GetFullPath((Join-Path $Root 'supervisor\bin\livecompanion-supervisor.exe'))
$CoreTarget = [IO.Path]::GetFullPath((Join-Path $Root 'core-service\bin\core-service.exe'))
$CoreCandidate = [IO.Path]::GetFullPath((Join-Path $Root 'core-service\bin\core-service.audio-v1-next.exe'))
$AudioTarget = [IO.Path]::GetFullPath((Join-Path $Root 'audio-service\bin\audio-service.exe'))
$AudioCandidate = [IO.Path]::GetFullPath((Join-Path $Root 'audio-service\bin\audio-service.audio-v1-next.exe'))

foreach ($path in @($SupervisorExe, $CoreTarget, $CoreCandidate, $AudioTarget, $AudioCandidate)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Required binary missing: $path" }
}

$registered = Get-ScheduledTask -TaskName $Task -ErrorAction Stop
if (@($registered.Actions | Where-Object { $_.Arguments -like ('*' + $Launcher + '*') }).Count -ne 1) {
    throw 'Scheduled task does not use this project launcher; refusing deployment.'
}

New-Item -ItemType Directory -Force -Path $RunDir | Out-Null
$lease = [IO.File]::Open((Join-Path $RunDir 'service-deployment.lock'), 'OpenOrCreate', 'ReadWrite', 'None')

function Find-OwnedProcess([string]$Path) {
    $name = [IO.Path]::GetFileName($Path)
    Get-CimInstance Win32_Process -Filter ("Name='" + $name + "'") | Where-Object {
        $_.ExecutablePath -and [string]::Equals([IO.Path]::GetFullPath($_.ExecutablePath), $Path, [StringComparison]::OrdinalIgnoreCase)
    }
}

function Stop-OwnedExecutable([string]$Path) {
    $deadline = (Get-Date).AddSeconds(20)
    do {
        $owned = @(Find-OwnedProcess $Path)
        if ($owned.Count -eq 0) { return }
        foreach ($entry in $owned) {
            $process = Get-Process -Id $entry.ProcessId -ErrorAction SilentlyContinue
            if (-not $process) { continue }
            if (-not [string]::Equals($process.Path, $Path, [StringComparison]::OrdinalIgnoreCase)) { throw 'Process identity changed; refusing to stop it.' }
            Stop-Process -InputObject $process -Force -ErrorAction Stop
            if (-not $process.WaitForExit(10000)) { throw "Owned process did not exit: $Path" }
        }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw "Owned executable keeps respawning: $Path"
}

function Pause-Supervisor {
    Disable-ScheduledTask -TaskName $Task | Out-Null
    Stop-ScheduledTask -TaskName $Task -ErrorAction SilentlyContinue
    Stop-OwnedExecutable $SupervisorExe
}

function Resume-Supervisor {
    Enable-ScheduledTask -TaskName $Task | Out-Null
    Start-ScheduledTask -TaskName $Task
    Set-Content -LiteralPath (Join-Path $RunDir 'runtime-mode.txt') -Value 'supervised' -Encoding ascii
}

function Http-Status([string]$Url) {
    try { return [int](Invoke-WebRequest -UseBasicParsing -Uri $Url -TimeoutSec 3).StatusCode }
    catch { if ($_.Exception.Response) { return [int]$_.Exception.Response.StatusCode }; return 0 }
}

function Wait-Healthy {
    $deadline = (Get-Date).AddSeconds(90)
    while ((Get-Date) -lt $deadline) {
        $oneSupervisor = @(Find-OwnedProcess $SupervisorExe).Count -eq 1
        $okWeb = (Http-Status 'http://127.0.0.1:5173/') -eq 200
        $okCustomer = (Http-Status 'http://127.0.0.1:5174/login') -eq 200
        $okSales = (Http-Status 'http://127.0.0.1:5175/login') -eq 200
        $okReceiver = (Http-Status 'http://127.0.0.1:5176/') -eq 200
        $okManagement = (Http-Status 'http://127.0.0.1:8080/healthz') -eq 200
        $okCore = (Http-Status 'http://127.0.0.1:8081/healthz') -eq 200
        $okAudio = (Http-Status 'http://127.0.0.1:8082/healthz') -eq 200
        if ($oneSupervisor -and $okWeb -and $okCustomer -and $okSales -and $okReceiver -and $okManagement -and $okCore -and $okAudio) { return $true }
        Start-Sleep -Seconds 2
    }
    return $false
}

try {
    $coreOldHash = (Get-FileHash -LiteralPath $CoreTarget -Algorithm SHA256).Hash
    $coreCandidateHash = (Get-FileHash -LiteralPath $CoreCandidate -Algorithm SHA256).Hash
    $audioOldHash = (Get-FileHash -LiteralPath $AudioTarget -Algorithm SHA256).Hash
    $audioCandidateHash = (Get-FileHash -LiteralPath $AudioCandidate -Algorithm SHA256).Hash

    $coreChanged = $coreOldHash -ne $coreCandidateHash
    $audioChanged = $audioOldHash -ne $audioCandidateHash

    $coreBackup = Join-Path (Split-Path $CoreTarget) ('core-service.backup-audio-v1-' + [Guid]::NewGuid().ToString('N') + '.exe')
    $audioBackup = Join-Path (Split-Path $AudioTarget) ('audio-service.backup-audio-v1-' + [Guid]::NewGuid().ToString('N') + '.exe')
    $coreBackupReady = $false
    $audioBackupReady = $false

    try {
        Pause-Supervisor
        Stop-OwnedExecutable $CoreTarget
        Stop-OwnedExecutable $AudioTarget

        if ($coreChanged) {
            Copy-Item -LiteralPath $CoreTarget -Destination $coreBackup
            $coreBackupReady = $true
            Copy-Item -LiteralPath $CoreCandidate -Destination $CoreTarget -Force
            if ((Get-FileHash -LiteralPath $CoreTarget -Algorithm SHA256).Hash -ne $coreCandidateHash) {
                throw 'Core candidate copy verification failed.'
            }
        }

        if ($audioChanged) {
            Copy-Item -LiteralPath $AudioTarget -Destination $audioBackup
            $audioBackupReady = $true
            Copy-Item -LiteralPath $AudioCandidate -Destination $AudioTarget -Force
            if ((Get-FileHash -LiteralPath $AudioTarget -Algorithm SHA256).Hash -ne $audioCandidateHash) {
                throw 'Audio candidate copy verification failed.'
            }
        }
    } finally {
        Resume-Supervisor
    }

    if (-not (Wait-Healthy)) {
        if ($coreBackupReady -or $audioBackupReady) {
            Pause-Supervisor
            Stop-OwnedExecutable $CoreTarget
            Stop-OwnedExecutable $AudioTarget
            if ($coreBackupReady) {
                Copy-Item -LiteralPath $coreBackup -Destination $CoreTarget -Force
            }
            if ($audioBackupReady) {
                Copy-Item -LiteralPath $audioBackup -Destination $AudioTarget -Force
            }
            Resume-Supervisor
            $restored = Wait-Healthy
            throw ('Audio V1 deployment unhealthy; prior Core/Audio restored. Healthy=' + $restored)
        }
        throw 'Audio V1 services did not become healthy.'
    }

    Write-Output 'DEPLOYED audio-channel-v1: core=8081 audio=8082 receiver=5176 all healthy'
    Write-Output ('Supervisor task=' + (Get-ScheduledTask -TaskName $Task).State)
    if ($coreBackupReady) { Write-Output ('Core backup=' + [IO.Path]::GetFileName($coreBackup)) }
    if ($audioBackupReady) { Write-Output ('Audio backup=' + [IO.Path]::GetFileName($audioBackup)) }
} finally {
    $lease.Dispose()
}
