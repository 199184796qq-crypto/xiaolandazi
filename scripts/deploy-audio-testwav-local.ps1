$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LocalRoot = if ($env:XIAOLAN_LOCAL_ROOT) {
    [IO.Path]::GetFullPath($env:XIAOLAN_LOCAL_ROOT)
} else {
    Join-Path (Split-Path $Root -Parent) ((Split-Path $Root -Leaf) + '-local')
}
$BackupDir = Join-Path $LocalRoot 'backups\audio'
$RunDir = Join-Path $Root 'data\run'
$Task = 'LiveCompanion-Supervisor'
$Launcher = Join-Path $Root 'scripts\run-supervisor-with-local-env.ps1'
$SupervisorExe = [IO.Path]::GetFullPath((Join-Path $Root 'supervisor\bin\livecompanion-supervisor.exe'))
$Target = [IO.Path]::GetFullPath((Join-Path $Root 'audio-service\bin\audio-service.exe'))
$Candidate = [IO.Path]::GetFullPath((Join-Path $Root 'audio-service\bin\audio-service.testwav-next.exe'))

foreach ($path in @($SupervisorExe, $Target, $Candidate)) {
    if (-not (Test-Path -LiteralPath $path)) { throw "Required binary missing: $path" }
}

$registered = Get-ScheduledTask -TaskName $Task -ErrorAction Stop
if (@($registered.Actions | Where-Object { $_.Arguments -like ('*' + $Launcher + '*') }).Count -ne 1) {
    throw 'Scheduled task does not use this project launcher; refusing deployment.'
}

New-Item -ItemType Directory -Force -Path $RunDir,$BackupDir | Out-Null
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
    $deadline = (Get-Date).AddSeconds(60)
    while ((Get-Date) -lt $deadline) {
        $oneSupervisor = @(Find-OwnedProcess $SupervisorExe).Count -eq 1
        if ($oneSupervisor -and (Http-Status 'http://127.0.0.1:8082/healthz') -eq 200) { return $true }
        Start-Sleep -Seconds 2
    }
    return $false
}

try {
    $oldHash = (Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash
    $candidateHash = (Get-FileHash -LiteralPath $Candidate -Algorithm SHA256).Hash
    $backup = Join-Path $BackupDir ('audio-service.backup-testwav-' + [Guid]::NewGuid().ToString('N') + '.exe')
    $changed = $oldHash -ne $candidateHash
    $backupReady = $false

    try {
        Pause-Supervisor
        Stop-OwnedExecutable $Target
        if ($changed) {
            Copy-Item -LiteralPath $Target -Destination $backup
            $backupReady = $true
            Copy-Item -LiteralPath $Candidate -Destination $Target -Force
            if ((Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash -ne $candidateHash) {
                throw 'audio-service candidate copy verification failed.'
            }
        }
    } finally {
        Resume-Supervisor
    }

    if (-not (Wait-Healthy)) {
        if ($backupReady) {
            Pause-Supervisor
            Stop-OwnedExecutable $Target
            Copy-Item -LiteralPath $backup -Destination $Target -Force
            Resume-Supervisor
            $restored = Wait-Healthy
            throw ('Configured test WAV deployment unhealthy; prior audio-service restored. Healthy=' + $restored)
        }
        throw 'audio-service did not become healthy.'
    }

    Write-Output 'DEPLOYED audio-service configured test WAV'
    Write-Output ('Supervisor task=' + (Get-ScheduledTask -TaskName $Task).State)
    if ($backupReady) { Write-Output ('Audio backup=' + $backup) }
} finally {
    $lease.Dispose()
}
