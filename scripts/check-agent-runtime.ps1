param(
  [Parameter(Mandatory=$true)][long]$TenantID,
  [long[]]$RoomID = @()
)
$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'

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
if ([string]::IsNullOrWhiteSpace($token)) {
  $token = 'local-core-dev-token'
}
$headers = @{ 'X-Core-Token' = $token }
$base = 'http://127.0.0.1:8081'

if ($RoomID.Count -eq 0) {
  $roomsResponse = Invoke-RestMethod -Uri ($base + '/internal/v1/rooms?tenant_id=' + $TenantID) -Headers $headers -TimeoutSec 5
  $rooms = if ($null -ne $roomsResponse.items) { @($roomsResponse.items) } else { @($roomsResponse) }
  $RoomID = @($rooms | Where-Object { $null -ne $_.id } | ForEach-Object { [long]$_.id })
}

$rows = foreach ($id in $RoomID) {
  try {
    $room = Invoke-RestMethod -Uri ($base + '/internal/v1/rooms/' + $id + '?tenant_id=' + $TenantID) -Headers $headers -TimeoutSec 5
    $runtime = Invoke-RestMethod -Uri ($base + '/internal/v1/rooms/' + $id + '/agent-runtime?tenant_id=' + $TenantID) -Headers $headers -TimeoutSec 5
    [PSCustomObject]@{
      room_id = $id
      name = $room.name
      room_status = $room.status
      state = $runtime.state
      stop_reason = $runtime.stop_reason
      working_seconds = $runtime.working_seconds
      lease_remaining_seconds = $runtime.lease_remaining_seconds
      boot_id = $runtime.boot_id
    }
  } catch {
    [PSCustomObject]@{
      room_id = $id
      name = ''
      room_status = ''
      state = 'ERROR'
      stop_reason = $_.Exception.Message
      working_seconds = 0
      lease_remaining_seconds = 0
      boot_id = ''
    }
  }
}
$rows | Format-Table -AutoSize
