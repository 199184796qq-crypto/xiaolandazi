[CmdletBinding()]
param(
  [Parameter(Mandatory = $true)][string]$MainCert,
  [Parameter(Mandatory = $true)][string]$MainKey,
  [Parameter(Mandatory = $true)][string]$SalesCert,
  [Parameter(Mandatory = $true)][string]$SalesKey,
  [string]$TargetConfig = '/etc/nginx/conf.d/xiaolan-sites.conf',
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }

foreach ($RemotePath in @($MainCert, $MainKey, $SalesCert, $SalesKey, $TargetConfig)) {
  if ($RemotePath -notmatch '^/[A-Za-z0-9._/-]+$') { throw "Unsupported remote path: $RemotePath" }
}

try {
  $SalesDns = Resolve-DnsName sales.xiaolandaizi.cn -Type A -ErrorAction Stop | Where-Object { $_.IPAddress }
} catch {
  $SalesDns = @()
}
if (-not $SalesDns) { throw 'sales.xiaolandaizi.cn has no A record yet. Add DNS before installing production routing.' }

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$RemoteTemplate = '/tmp/xiaolan-sites.conf.template'
$RemoteInstaller = '/tmp/xiaolan-install-web-routing.sh'

& scp @SshCommon (Join-Path (Split-Path $PSScriptRoot -Parent) 'deploy\nginx\xiaolan-sites.conf.template') ($Remote + ':' + $RemoteTemplate)
if ($LASTEXITCODE -ne 0) { throw 'Nginx template upload failed.' }
& scp @SshCommon (Join-Path $PSScriptRoot 'server\install-web-routing.sh') ($Remote + ':' + $RemoteInstaller)
if ($LASTEXITCODE -ne 0) { throw 'Nginx installer upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteInstaller $RemoteTemplate $TargetConfig $MainCert $MainKey $SalesCert $SalesKey"
if ($LASTEXITCODE -ne 0) { throw 'Production routing install failed.' }

& (Join-Path $PSScriptRoot 'test-production.ps1')
