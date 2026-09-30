[CmdletBinding()]
param(
  [ValidateSet('core', 'management')][string]$Service = 'core',
  [ValidateRange(10, 2000)][int]$Lines = 200,
  [switch]$Follow,
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }
$Unit = if ($Service -eq 'core') { 'xiaolan-core.service' } else { 'xiaolan-management.service' }
$FollowArg = if ($Follow) { '-f' } else { '' }
$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
& ssh @SshCommon $Remote "sudo -n journalctl -u $Unit -n $Lines --no-pager $FollowArg"
if ($LASTEXITCODE -ne 0) { throw 'Failed to read production logs.' }
