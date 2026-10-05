[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][ValidatePattern('^[A-Za-z0-9:._-]{2,160}$')][string]$DeviceId,
  [Parameter(Mandatory = $true)][ValidateRange(1, 2147483647)][long]$RoomId,
  [ValidateSet('bind', 'unbind')][string]$Action = 'bind',
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$RemoteHelper = '/tmp/xiaolan-bind-xiaozhi-device.sh'
$LocalHelper = Join-Path $PSScriptRoot 'server\bind-xiaozhi-device.sh'

& scp @SshCommon $LocalHelper ($Remote + ':' + $RemoteHelper)
if ($LASTEXITCODE -ne 0) { throw 'Xiaozhi binding helper upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteHelper $DeviceId $RoomId $Action"
if ($LASTEXITCODE -ne 0) { throw "Xiaozhi device $Action failed." }

Write-Host "[xiaozhi] $Action device=$DeviceId room=$RoomId"
