[CmdletBinding()]
param(
  [string]$Domain = 'sales.xiaolandaizi.cn',
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }

try {
  $Addresses = @(
    Resolve-DnsName $Domain -Type A -ErrorAction Stop |
      Where-Object { $_.IPAddress } |
      ForEach-Object { $_.IPAddress }
  )
} catch {
  $Addresses = @()
}
if ($Addresses -notcontains $HostName) {
  throw "$Domain must resolve to $HostName before sales HTTPS can be enabled. Current A records: $($Addresses -join ', ')"
}

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$RemoteIssuer = '/tmp/xiaolan-issue-sales-certificate.sh'

& scp @SshCommon (Join-Path $PSScriptRoot 'server\issue-sales-certificate.sh') ($Remote + ':' + $RemoteIssuer)
if ($LASTEXITCODE -ne 0) { throw 'Sales certificate helper upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteIssuer $Domain $HostName"
if ($LASTEXITCODE -ne 0) { throw 'Sales certificate issuance failed.' }

$SalesCert = "/etc/letsencrypt/live/$Domain/fullchain.pem"
$SalesKey = "/etc/letsencrypt/live/$Domain/privkey.pem"

& (Join-Path $PSScriptRoot 'install-production-routing.ps1') `
  -MainCert '/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.pem' `
  -MainKey '/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.key' `
  -SalesCert $SalesCert `
  -SalesKey $SalesKey `
  -TargetConfig '/etc/nginx/sites-enabled/xiaolan' `
  -HostName $HostName `
  -UserName $UserName `
  -IdentityFile $IdentityFile

Write-Host "[sales] production enabled: https://$Domain"
