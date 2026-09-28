param(
  [Parameter(Mandatory=$true)][long]$TenantID,
  [Parameter(Mandatory=$true)][long]$LiveRoomID,
  [Parameter(Mandatory=$true)][long]$OfflineRoomID,
  [ValidateRange(1, 10)][int]$LeaseSeconds = 3
)
$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'
$Base = 'http://127.0.0.1:8081'

function Get-LocalEnvValue([string]$Name) {
  if (-not (Test-Path -LiteralPath $ConfigPath)) { return $null }
  foreach ($line in [System.IO.File]::ReadAllLines($ConfigPath)) {
    $trimmed = $line.Trim()
    if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }
    $idx = $line.IndexOf('=')
    if ($idx -le 0) { continue }
    if ($line.Substring(0, $idx).Trim() -eq $Name) {
      return $line.Substring($idx + 1)
    }
  }
  return $null
}

$token = Get-LocalEnvValue 'CORE_INTERNAL_TOKEN'
if ([string]::IsNullOrWhiteSpace($token)) { $token = 'local-core-dev-token' }
$Headers = @{ 'X-Core-Token' = $token }

function Runtime-Url([long]$RoomID) {
  return ($Base + '/internal/v1/rooms/' + $RoomID + '/agent-runtime?tenant_id=' + $TenantID)
}

function Get-Runtime([long]$RoomID) {
  return Invoke-RestMethod -Uri (Runtime-Url $RoomID) -Headers $Headers -TimeoutSec 5
}

function Put-Runtime([long]$RoomID, [hashtable]$Body) {
  $json = $Body | ConvertTo-Json -Compress
  return Invoke-RestMethod -Uri (Runtime-Url $RoomID) -Method Put -Headers $Headers -ContentType 'application/json' -Body $json -TimeoutSec 5
}

$offlineBefore = Get-Runtime $OfflineRoomID
try {
  Put-Runtime $OfflineRoomID @{
    command = 'start'
    base_working_seconds = [uint64]$offlineBefore.working_seconds
    lease_seconds = $LeaseSeconds
  } | Out-Null
  throw 'Offline room unexpectedly accepted paid Agent start.'
} catch {
  $status = 0
  if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
  if ($status -ne 409) { throw }
}
$offlineAfter = Get-Runtime $OfflineRoomID
if ($offlineAfter.state -ne 'stopped' -or [uint64]$offlineAfter.lease_remaining_seconds -ne 0) {
  throw ('Offline start changed runtime: state=' + $offlineAfter.state + ' lease=' + $offlineAfter.lease_remaining_seconds)
}
Write-Output ('PASS offline-start-guard room=' + $OfflineRoomID)

$liveBefore = Get-Runtime $LiveRoomID
if ($liveBefore.state -ne 'stopped') {
  throw ('Live test room must start stopped; current=' + $liveBefore.state)
}
$working = Put-Runtime $LiveRoomID @{
  command = 'start'
  base_working_seconds = [uint64]$liveBefore.working_seconds
  lease_seconds = $LeaseSeconds
}
if ($working.state -ne 'working' -or [uint64]$working.lease_remaining_seconds -eq 0) {
  throw ('Live start failed: state=' + $working.state + ' lease=' + $working.lease_remaining_seconds)
}
if ($LeaseSeconds -le 15 -and -not [bool]$working.lease_renewal_due) {
  throw 'Core did not mark short paid lease as renewal due.'
}
Write-Output ('PASS live-start room=' + $LiveRoomID + ' lease=' + $working.lease_remaining_seconds)

Start-Sleep -Seconds ($LeaseSeconds + 2)
$expired = Get-Runtime $LiveRoomID
if ($expired.state -ne 'stopped' -or $expired.stop_reason -ne 'quota_exhausted') {
  throw ('Lease expiry failed: state=' + $expired.state + ' reason=' + $expired.stop_reason)
}
if ([uint64]$expired.lease_remaining_seconds -ne 0) {
  throw ('Expired runtime still has lease=' + $expired.lease_remaining_seconds)
}
Write-Output ('PASS lease-expiry-stop room=' + $LiveRoomID + ' working_seconds=' + $expired.working_seconds)

$manualWorking = Put-Runtime $LiveRoomID @{
  command = 'start'
  base_working_seconds = [uint64]$expired.working_seconds
  lease_seconds = $LeaseSeconds
}
if ($manualWorking.state -ne 'working') {
  throw ('Manual-stop setup failed: state=' + $manualWorking.state)
}
$manualStopped = Put-Runtime $LiveRoomID @{
  command = 'stop'
  base_working_seconds = [uint64]$manualWorking.working_seconds
  stop_reason = 'manual'
}
if ($manualStopped.state -ne 'stopped' -or $manualStopped.stop_reason -ne 'manual') {
  throw ('Manual stop failed: state=' + $manualStopped.state + ' reason=' + $manualStopped.stop_reason)
}
if ([uint64]$manualStopped.lease_remaining_seconds -ne 0) {
  throw ('Manual stop left lease=' + $manualStopped.lease_remaining_seconds)
}
Write-Output ('PASS manual-stop room=' + $LiveRoomID + ' working_seconds=' + $manualStopped.working_seconds)
