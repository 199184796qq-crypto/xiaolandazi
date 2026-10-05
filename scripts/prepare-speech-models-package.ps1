[CmdletBinding()]
param([string]$ReleaseId = '20261004-speech-models-v1')
$ErrorActionPreference = 'Stop'
if ($ReleaseId -notmatch '^[A-Za-z0-9._-]+$' -or $ReleaseId.Contains('..')) { throw 'Invalid release id' }
$taskRepo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$taskReleases = 'E:\直播伴播-local\releases'
$taskRelease = Join-Path $taskReleases $ReleaseId
if (Test-Path -LiteralPath $taskRelease) { throw 'Release exists' }
# Frozen, successfully checked feature build based on the deployed v19 page.
# Do not package newer unrelated live-page edits from a subsequent rebuild.
$taskDist = Join-Path $taskRepo 'web-console\dist'
$baselineJS = Join-Path $taskDist 'assets\index-aer8b4vW.js'
$baselineCSS = Join-Path $taskDist 'assets\index-BU2V3BjP.css'
$featureJS = Join-Path $taskDist 'assets\index-BK75lwsi.js'
if ((Get-FileHash -LiteralPath $baselineJS).Hash.ToLowerInvariant() -ne '684295ccfcd4533f046eb9da89e54cec7cb9320f4936a99d4843e38b2b2e9368') { throw 'Baseline JS changed' }
if ((Get-FileHash -LiteralPath $baselineCSS).Hash.ToLowerInvariant() -ne '8ec0c833f0035a504ed459dc510f6df77b19251efacc1bb1bdb8fa7b41df387b') { throw 'Baseline CSS changed' }
if ((Get-FileHash -LiteralPath $featureJS).Hash.ToLowerInvariant() -ne '6d8e4de573f08e54a3179a9d2efcf44fa6dd4f8b3869085f5467d107536a3747') { throw 'Feature JS changed' }
New-Item -ItemType Directory -Path (Join-Path $taskRelease 'bin') -Force | Out-Null
$taskGoos=$env:GOOS; $taskGoarch=$env:GOARCH; $taskCgo=$env:CGO_ENABLED
try {
  $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
  Push-Location (Join-Path $taskRepo 'management-service')
  try {
    & go build -trimpath -ldflags '-s -w' -o (Join-Path $taskRelease 'bin\management-service') ./cmd/management
    if ($LASTEXITCODE -ne 0) { throw 'Management build failed' }
    & go test -c -o (Join-Path $taskReleases "$ReleaseId-db.test") ./internal/db
    if ($LASTEXITCODE -ne 0) { throw 'Database test build failed' }
  } finally { Pop-Location }
} finally { $env:GOOS=$taskGoos; $env:GOARCH=$taskGoarch; $env:CGO_ENABLED=$taskCgo }
foreach ($surface in @('web','web-desktop')) {
  New-Item -ItemType Directory -Path (Join-Path $taskRelease $surface) | Out-Null
  Copy-Item -LiteralPath (Join-Path $taskDist 'assets') -Destination (Join-Path $taskRelease "$surface\assets") -Recurse
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'speech-models-desktop-index.html') -Destination (Join-Path $taskRelease "$surface\index.html")
  # Keep immutable/static media referenced by the existing desktop.
  foreach ($item in Get-ChildItem -LiteralPath $taskDist -Force) {
    if ($item.Name -notin @('assets','index.html')) { Copy-Item -LiteralPath $item.FullName -Destination (Join-Path $taskRelease $surface) -Recurse }
  }
}
$taskLines = @(& git -C $taskRepo -c core.quotepath=false status --porcelain=v1)
$taskFiles = @($taskLines | ForEach-Object {
  $taskLine=[string]$_; $taskPath=$taskLine.Substring(3).Trim('"')
  $taskEntry=[ordered]@{path=$taskPath;status=$taskLine.Substring(0,2)}
  $taskSource=Join-Path $taskRepo $taskPath
  if (Test-Path -LiteralPath $taskSource -PathType Leaf) { $taskEntry.sha256=(Get-FileHash -LiteralPath $taskSource).Hash.ToLowerInvariant() }
  $taskEntry
})
$taskManifest=Join-Path $taskReleases "$ReleaseId-dirty-manifest.json"
[ordered]@{git_head=((& git -C $taskRepo rev-parse HEAD)|Out-String).Trim();dirty=$true;scope=@('bin/management-service','web','web-desktop');frontend_snapshot='2026-10-04T19:12:22+08:00';files=$taskFiles} | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $taskManifest -Encoding utf8NoBOM
$taskArchive=Join-Path $taskReleases "$ReleaseId.tar.gz"
& tar -czf $taskArchive -C $taskRelease bin/management-service web web-desktop
if ($LASTEXITCODE -ne 0) { throw 'Archive failed' }
[ordered]@{archive=$taskArchive;archive_sha=(Get-FileHash -LiteralPath $taskArchive).Hash.ToLowerInvariant();binary_sha=(Get-FileHash -LiteralPath (Join-Path $taskRelease 'bin\management-service')).Hash.ToLowerInvariant();manifest=$taskManifest;manifest_sha=(Get-FileHash -LiteralPath $taskManifest).Hash.ToLowerInvariant();db_test=(Join-Path $taskReleases "$ReleaseId-db.test")} | ConvertTo-Json
