[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$ReleaseId)
$ErrorActionPreference = 'Stop'
if ($ReleaseId -notmatch '^[A-Za-z0-9._-]+$') { throw 'Invalid release id' }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$releases = 'E:\直播伴播-local\releases'
$release = Join-Path $releases $ReleaseId
if (-not (Test-Path -LiteralPath (Join-Path $release 'bin\management-service'))) { throw 'Build the release first' }
$manifestPath = Join-Path $releases "$ReleaseId-dirty-manifest.json"
$archivePath = Join-Path $releases "$ReleaseId-scoped.tar.gz"
if ((Test-Path -LiteralPath $archivePath) -or (Test-Path -LiteralPath $manifestPath)) { throw 'Scoped package already exists; choose a new release id' }
$entries = @(& git -C $repo -c core.quotepath=false status --porcelain=v1)
if ($LASTEXITCODE -ne 0) { throw 'Cannot enumerate source status' }
$files = @($entries | ForEach-Object {
  $line = [string]$_
  $path = $line.Substring(3).Trim('"')
  $entry = [ordered]@{ path = $path; status = $line.Substring(0,2) }
  $source = Join-Path $repo $path
  if (Test-Path -LiteralPath $source -PathType Leaf) { $entry.sha256 = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant() }
  $entry
})
$manifest = [ordered]@{
  git_head = ((& git -C $repo rev-parse HEAD) | Out-String).Trim()
  dirty = ($entries.Count -gt 0)
  scope = @('bin/core-service','bin/management-service','web','web-desktop')
  built_release = $ReleaseId
  files = $files
}
$manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifestPath -Encoding utf8NoBOM
& tar -czf $archivePath -C $release bin/core-service bin/management-service web web-desktop
if ($LASTEXITCODE -ne 0) { throw 'Scoped archive failed' }
@{
  archive = $archivePath
  archive_sha = (Get-FileHash -LiteralPath $archivePath).Hash.ToLowerInvariant()
  core_sha = (Get-FileHash -LiteralPath (Join-Path $release 'bin\core-service')).Hash.ToLowerInvariant()
  management_sha = (Get-FileHash -LiteralPath (Join-Path $release 'bin\management-service')).Hash.ToLowerInvariant()
  manifest = $manifestPath
  manifest_sha = (Get-FileHash -LiteralPath $manifestPath).Hash.ToLowerInvariant()
} | ConvertTo-Json
