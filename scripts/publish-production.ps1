[CmdletBinding()]
param(
  [string]$HostName = $(if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }),
  [string]$UserName = $(if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }),
  [string]$IdentityFile = $(if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh\xiaolan-ecs-deploy' }),
  [string]$ReleaseId = '',
  [switch]$AllowDirty,
  [switch]$SkipTests,
  [switch]$SkipSalesSmoke
)

$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

if (-not (Test-Path $IdentityFile)) { throw "SSH key not found: $IdentityFile" }

if (-not $ReleaseId) {
  $Sha = ((& git -C $RepoRoot rev-parse --short=12 HEAD) | Out-String).Trim()
  if ($LASTEXITCODE -ne 0 -or -not $Sha) { throw 'Cannot resolve Git revision.' }
  $ReleaseId = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + $Sha
  if (@(& git -C $RepoRoot status --porcelain).Count -gt 0) { $ReleaseId += '-dirty' }
}

$BuildArgs = @('-ReleaseId', $ReleaseId)
if ($AllowDirty) { $BuildArgs += '-AllowDirty' }
if ($SkipTests) { $BuildArgs += '-SkipTests' }
$BuildOutput = @(& (Join-Path $PSScriptRoot 'build-production-release.ps1') @BuildArgs)
$ArchiveLine = $BuildOutput | Where-Object { $_ -is [string] -and $_.StartsWith('__XIAOLAN_ARCHIVE__=') } | Select-Object -Last 1
if (-not $ArchiveLine) { throw 'Build completed without returning a release archive path.' }
$ArchivePath = $ArchiveLine.Substring('__XIAOLAN_ARCHIVE__='.Length)
if (-not (Test-Path $ArchivePath)) { throw "Release archive missing: $ArchivePath" }

$SshCommon = @('-i', $IdentityFile, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Remote = "$UserName@$HostName"

& ssh @SshCommon $Remote 'true'
if ($LASTEXITCODE -ne 0) { throw "SSH authentication failed for $Remote" }

$RemoteArchive = "/tmp/xiaolan-$ReleaseId.tar.gz"
$RemoteDeploy = '/tmp/xiaolan-deploy-release.sh'
& scp @SshCommon $ArchivePath ($Remote + ':' + $RemoteArchive)
if ($LASTEXITCODE -ne 0) { throw 'Release upload failed.' }
& scp @SshCommon (Join-Path $PSScriptRoot 'server\deploy-release.sh') ($Remote + ':' + $RemoteDeploy)
if ($LASTEXITCODE -ne 0) { throw 'Deploy script upload failed.' }

& ssh @SshCommon $Remote "sudo -n bash $RemoteDeploy $RemoteArchive $ReleaseId"
if ($LASTEXITCODE -ne 0) {
  & ssh @SshCommon $Remote "sudo -n journalctl -u xiaolan-core.service -u xiaolan-management.service -n 120 --no-pager"
  throw 'Server deployment failed.'
}

$SmokeArgs = @()
if ($SkipSalesSmoke) { $SmokeArgs += '-SkipSales' }
& (Join-Path $PSScriptRoot 'test-production.ps1') @SmokeArgs
if ($LASTEXITCODE -ne 0) { throw 'Production smoke test failed.' }

Write-Host "[deploy] production release delivered: $ReleaseId"
