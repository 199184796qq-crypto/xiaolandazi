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
$RemoteSalesConfig = '/tmp/xiaolan-sales-http.conf'
$InstalledSalesConfig = '/etc/nginx/conf.d/xiaolan-sales.conf'

& scp @SshCommon (Join-Path $PSScriptRoot 'server\issue-sales-certificate.sh') ($Remote + ':' + $RemoteIssuer)
if ($LASTEXITCODE -ne 0) { throw 'Sales certificate helper upload failed.' }

$CertificatePath = "/etc/letsencrypt/live/$Domain/fullchain.pem"
& ssh @SshCommon $Remote "sudo -n test -f $CertificatePath"
if ($LASTEXITCODE -ne 0) {
  & ssh @SshCommon $Remote "sudo -n bash $RemoteIssuer $Domain $HostName"
  if ($LASTEXITCODE -ne 0) { throw 'Sales certificate issuance failed.' }
}

$SalesConfig = Join-Path (Split-Path $PSScriptRoot -Parent) 'deploy\nginx\xiaolan-sales-http.conf'
& scp @SshCommon $SalesConfig ($Remote + ':' + $RemoteSalesConfig)
if ($LASTEXITCODE -ne 0) { throw 'Sales Nginx config upload failed.' }

& ssh @SshCommon $Remote "sudo -n cp $RemoteSalesConfig $InstalledSalesConfig && sudo -n nginx -t && sudo -n systemctl reload nginx"
if ($LASTEXITCODE -ne 0) { throw 'Sales HTTP site install failed.' }

& ssh @SshCommon $Remote "sudo -n certbot --nginx --cert-name $Domain -d $Domain --redirect --non-interactive"
if ($LASTEXITCODE -ne 0) { throw 'Sales HTTPS install failed.' }

& (Join-Path $PSScriptRoot 'test-production.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Sales production smoke test failed.' }

Write-Host "[sales] production enabled: https://$Domain"
