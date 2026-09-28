param(
  [string]$Candidate = '',
  [ValidateSet('management', 'core', 'supervisor')][string]$Service = 'management',
  [ValidateRange(30, 600)][int]$StartupTimeoutSeconds = 240,
  [string]$ExpectedCurrentSHA256 = ''
)
$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RunDir = Join-Path $Root 'data/run'
$Task = 'LiveCompanion-Supervisor'
$SupervisorExe = Join-Path $Root 'supervisor/bin/livecompanion-supervisor.exe'
$ManagementExe = Join-Path $Root 'management-service/bin/management-service.exe'
$CoreExe = Join-Path $Root 'core-service/bin/core-service.exe'
$Launcher = Join-Path $Root 'scripts/run-supervisor-with-local-env.ps1'
if ($Service -eq 'supervisor') {
  $Bin = (Resolve-Path (Join-Path $Root 'supervisor/bin')).Path
  $Target = $SupervisorExe
  if (-not $Candidate) { $Candidate = 'supervisor/bin/livecompanion-supervisor.singleton-next.exe' }
} elseif ($Service -eq 'core') {
  $Bin = (Resolve-Path (Join-Path $Root 'core-service/bin')).Path
  $Target = $CoreExe
  if (-not $Candidate) { $Candidate = 'core-service/bin/core-service.next.exe' }
} else {
  $Bin = (Resolve-Path (Join-Path $Root 'management-service/bin')).Path
  $Target = $ManagementExe
  if (-not $Candidate) { $Candidate = 'management-service/bin/management-service.sales-next.exe' }
}
$Target = [IO.Path]::GetFullPath($Target)
$SupervisorExe = [IO.Path]::GetFullPath($SupervisorExe)
$ManagementExe = [IO.Path]::GetFullPath($ManagementExe)
$CoreExe = [IO.Path]::GetFullPath($CoreExe)
$Source = (Resolve-Path (Join-Path $Root $Candidate)).Path
if (-not $Source.StartsWith($Bin + '\', [StringComparison]::OrdinalIgnoreCase) -or $Source -eq $Target) {
  throw 'Candidate must be a distinct binary inside the selected service bin directory.'
}
$RegisteredTask = Get-ScheduledTask -TaskName $Task -ErrorAction Stop
if (@($RegisteredTask.Actions | Where-Object { $_.Arguments -like ('*' + $Launcher + '*') }).Count -ne 1) {
  throw 'Scheduled task does not use this project launcher; refusing to change it.'
}
New-Item -ItemType Directory -Force $RunDir | Out-Null
# Shared, kernel-released deployment lease prevents two script invocations racing.
$Lease = [IO.File]::Open((Join-Path $RunDir 'service-deployment.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
function Find-OwnedProcess([string]$Path) {
  $name = [IO.Path]::GetFileName($Path)
  Get-CimInstance Win32_Process -Filter ("Name='" + $name + "'") | Where-Object {
    $_.ExecutablePath -and [string]::Equals([IO.Path]::GetFullPath($_.ExecutablePath), $Path, [StringComparison]::OrdinalIgnoreCase)
  }
}
function Stop-OwnedExecutable([string]$Path) {
  # Match the executable, not its port: migrations run before 8080 is bound.
  $deadline = (Get-Date).AddSeconds(20)
  do {
    $owned = @(Find-OwnedProcess $Path)
    if ($owned.Count -eq 0) { return }
    foreach ($entry in $owned) {
      $process = Get-Process -Id $entry.ProcessId -ErrorAction SilentlyContinue
      if (-not $process) { continue }
      if (-not [string]::Equals($process.Path, $Path, [StringComparison]::OrdinalIgnoreCase)) { throw 'Process identity changed; refusing to stop it.' }
      if ([Math]::Abs(($process.StartTime - $entry.CreationDate).TotalSeconds) -gt 1) { throw 'PID was reused; refusing to stop it.' }
      Stop-Process -InputObject $process -Force -ErrorAction Stop
      if (-not $process.WaitForExit(10000)) { throw 'Owned process did not exit.' }
    }
    Start-Sleep -Milliseconds 250
  } while ((Get-Date) -lt $deadline)
  throw 'Owned executable keeps respawning; another controller is active.'
}
function Pause-Supervisor {
  Disable-ScheduledTask -TaskName $Task | Out-Null
  Stop-ScheduledTask -TaskName $Task
  # Stopping a PowerShell scheduled task can leave its native child alive.
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
function Wait-Healthy([string]$HealthUrl, [bool]$RequireSalesRoutes) {
  $end = (Get-Date).AddSeconds($StartupTimeoutSeconds)
  while ((Get-Date) -lt $end) {
    $oneOwner = @(Find-OwnedProcess $SupervisorExe).Count -eq 1
    $healthy = (Http-Status $HealthUrl) -eq 200
    if ($oneOwner -and $healthy -and (-not $RequireSalesRoutes -or (Http-Status 'http://127.0.0.1:8080/api/v1/sales/leads') -eq 401)) { return $true }
    Start-Sleep -Seconds 3
  }
  return $false
}
try {
  $OriginalHash = (Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash
  $CandidateHash = (Get-FileHash -LiteralPath $Source -Algorithm SHA256).Hash
  if ($ExpectedCurrentSHA256 -and $OriginalHash -ne $ExpectedCurrentSHA256) { throw 'Current binary differs from caller snapshot; deployment stopped.' }
  $Backup = Join-Path $Bin ([IO.Path]::GetFileNameWithoutExtension($Target) + '.backup-' + [Guid]::NewGuid().ToString('N') + '.exe')
  $changed = $OriginalHash -ne $CandidateHash
  $backupReady = $false
  $writeAttempted = $false
  $HealthUrl = if ($Service -eq 'core') { 'http://127.0.0.1:8081/healthz' } else { 'http://127.0.0.1:8080/healthz' }
  $RequireSalesRoutes = $Service -ne 'core'
  try {
    Pause-Supervisor
    if ($changed) {
      if ($Service -eq 'management') { Stop-OwnedExecutable $ManagementExe }
      if ($Service -eq 'core') { Stop-OwnedExecutable $CoreExe }
      if ((Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash -ne $OriginalHash) { throw 'Binary changed concurrently; deployment stopped.' }
      Copy-Item -LiteralPath $Target -Destination $Backup
      $backupReady = $true
      $writeAttempted = $true
      Copy-Item -LiteralPath $Source -Destination $Target -Force
      if ((Get-FileHash -LiteralPath $Target -Algorithm SHA256).Hash -ne $CandidateHash) { throw 'Candidate copy verification failed.' }
    }
  } catch {
    if ($backupReady -and $writeAttempted) {
      Stop-OwnedExecutable $Target
      Copy-Item -LiteralPath $Backup -Destination $Target -Force
    }
    throw
  } finally { Resume-Supervisor }
  if (-not (Wait-Healthy $HealthUrl $RequireSalesRoutes)) {
    if ($backupReady -and $writeAttempted) {
      try {
        Pause-Supervisor
        Stop-OwnedExecutable $Target
        Copy-Item -LiteralPath $Backup -Destination $Target -Force
      } finally { Resume-Supervisor }
      $restored = Wait-Healthy $HealthUrl $false
      throw ('Candidate unhealthy; prior binary restored. Healthy=' + $restored)
    }
    throw 'Service health or supervisor ownership did not recover.'
  }
  Write-Output ('DEPLOYED ' + $Service + ': health=200, supervisor owners=1')
  Write-Output ('Supervisor task=' + (Get-ScheduledTask -TaskName $Task).State)
  if ($backupReady) { Write-Output ('Backup=' + [IO.Path]::GetFileName($Backup)) }
} finally { $Lease.Dispose() }

