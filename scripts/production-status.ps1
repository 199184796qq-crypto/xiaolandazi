[CmdletBinding()]
param(
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' })
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }
$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"
$Command = "printf 'current='; readlink -f /opt/xiaolan/current 2>/dev/null || true; printf 'core='; systemctl is-active xiaolan-core.service 2>/dev/null || true; printf 'management='; systemctl is-active xiaolan-management.service 2>/dev/null || true; printf 'xiaozhi='; systemctl is-active xiaolan-xiaozhi.service 2>/dev/null || true; printf 'core_health='; curl -fsS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8081/healthz || true; printf '\n'; printf 'management_health='; curl -fsS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8080/healthz || true; printf '\n'; printf 'xiaozhi_health='; curl -fsS -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8083/healthz || true; printf '\nrecent_releases:\n'; ls -1t /opt/xiaolan/releases 2>/dev/null | head -n 8"
& ssh @SshCommon $Remote $Command
if ($LASTEXITCODE -ne 0) { throw 'Production status check failed.' }
