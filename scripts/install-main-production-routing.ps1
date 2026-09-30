[CmdletBinding()]
param(
  [string]$MainCert = '/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.pem',
  [string]$MainKey = '/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.key',
  [string]$TargetConfig = '/etc/nginx/sites-enabled/xiaolan',
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }
foreach ($RemotePath in @($MainCert, $MainKey, $TargetConfig)) {
  if ($RemotePath -notmatch '^/[A-Za-z0-9._/-]+$') { throw "Unsupported remote path: $RemotePath" }
}

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$RemoteTemplate = '/tmp/xiaolan-main.conf.template'
$RemoteInstaller = '/tmp/xiaolan-install-main-web-routing.sh'

& scp @SshCommon (Join-Path (Split-Path $PSScriptRoot -Parent) 'deploy\nginx\xiaolan-main.conf.template') ($Remote + ':' + $RemoteTemplate)
if ($LASTEXITCODE -ne 0) { throw 'Main Nginx template upload failed.' }
& scp @SshCommon (Join-Path $PSScriptRoot 'server\install-main-web-routing.sh') ($Remote + ':' + $RemoteInstaller)
if ($LASTEXITCODE -ne 0) { throw 'Main Nginx installer upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteInstaller $RemoteTemplate $TargetConfig $MainCert $MainKey"
if ($LASTEXITCODE -ne 0) { throw 'Main production routing install failed.' }

& (Join-Path $PSScriptRoot 'test-production.ps1') -SkipSales
