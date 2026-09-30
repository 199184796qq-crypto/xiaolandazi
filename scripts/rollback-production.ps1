[CmdletBinding()]
param(
  [string]$ReleaseId = '',
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$RemoteScript = '/tmp/xiaolan-rollback-release.sh'

& scp @SshCommon (Join-Path $PSScriptRoot 'server\rollback-release.sh') ($Remote + ':' + $RemoteScript)
if ($LASTEXITCODE -ne 0) { throw 'Rollback script upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteScript '$ReleaseId'"
if ($LASTEXITCODE -ne 0) { throw 'Rollback failed.' }

& (Join-Path $PSScriptRoot 'test-production.ps1') -SkipSales
