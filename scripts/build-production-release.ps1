[CmdletBinding()]
param(
  [string]$ReleaseId = '',
  [switch]$AllowDirty,
  [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$RepoName = Split-Path $RepoRoot -Leaf
$LocalRoot = if ($env:XIAOLAN_LOCAL_ROOT) {
  $env:XIAOLAN_LOCAL_ROOT
} else {
  Join-Path (Split-Path $RepoRoot -Parent) ($RepoName + '-local')
}
$ReleaseRoot = Join-Path $LocalRoot 'releases'
New-Item -ItemType Directory -Force -Path $ReleaseRoot | Out-Null

$GitSha = ((& git -C $RepoRoot rev-parse --short=12 HEAD) | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or -not $GitSha) { throw 'Cannot resolve Git revision.' }
$DirtyLines = @(& git -C $RepoRoot status --porcelain)
$IsDirty = $DirtyLines.Count -gt 0
if ($IsDirty -and -not $AllowDirty) {
  throw 'Working tree is dirty. Commit/stash first, or rerun with -AllowDirty for an intentional test release.'
}

if (-not $ReleaseId) {
  $Stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
  $DirtySuffix = if ($IsDirty) { '-dirty' } else { '' }
  $ReleaseId = "$Stamp-$GitSha$DirtySuffix"
}
if ($ReleaseId -notmatch '^[A-Za-z0-9._-]+$') { throw 'ReleaseId contains unsupported characters.' }

$ReleaseDir = Join-Path $ReleaseRoot $ReleaseId
$ArchivePath = Join-Path $ReleaseRoot ($ReleaseId + '.tar.gz')
if (Test-Path $ReleaseDir) { throw "Release directory already exists: $ReleaseDir" }
if (Test-Path $ArchivePath) { throw "Release archive already exists: $ArchivePath" }

function Invoke-External {
  param(
    [Parameter(Mandatory = $true)][string]$WorkingDirectory,
    [Parameter(Mandatory = $true)][scriptblock]$Command,
    [Parameter(Mandatory = $true)][string]$Label
  )
  Push-Location $WorkingDirectory
  try {
    & $Command
    if ($LASTEXITCODE -ne 0) { throw "$Label failed with exit code $LASTEXITCODE" }
  } finally {
    Pop-Location
  }
}

Write-Host "[release] id=$ReleaseId sha=$GitSha dirty=$IsDirty"

if (-not $SkipTests) {
  Invoke-External (Join-Path $RepoRoot 'core-service') { go test ./... } 'core-service tests'
  Invoke-External (Join-Path $RepoRoot 'management-service') { go test ./... } 'management-service tests'
  Invoke-External $RepoRoot { pnpm --dir customer-mobile check } 'customer-mobile check'
  Invoke-External $RepoRoot { pnpm --dir sales-mobile check } 'sales-mobile check'
}

Invoke-External $RepoRoot { pnpm build:web } 'desktop web build'
Invoke-External $RepoRoot { pnpm build:customer } 'customer mobile build'
Invoke-External $RepoRoot { pnpm build:sales } 'sales mobile build'

foreach ($required in @(
  'web-console\dist\index.html',
  'customer-mobile\build\index.html',
  'sales-mobile\build\index.html'
)) {
  if (-not (Test-Path (Join-Path $RepoRoot $required))) { throw "Missing web build output: $required" }
}

New-Item -ItemType Directory -Force -Path (Join-Path $ReleaseDir 'bin') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $ReleaseDir 'collector-worker\node_modules') | Out-Null

$OldGoos = $env:GOOS
$OldGoarch = $env:GOARCH
$OldCgo = $env:CGO_ENABLED
try {
  $env:GOOS = 'linux'
  $env:GOARCH = 'amd64'
  $env:CGO_ENABLED = '0'
  Invoke-External (Join-Path $RepoRoot 'core-service') {
    go build -trimpath -ldflags '-s -w' -o (Join-Path $ReleaseDir 'bin\core-service') ./cmd/core
  } 'core-service linux build'
  Invoke-External (Join-Path $RepoRoot 'management-service') {
    go build -trimpath -ldflags '-s -w' -o (Join-Path $ReleaseDir 'bin\management-service') ./cmd/management
  } 'management-service linux build'
} finally {
  $env:GOOS = $OldGoos
  $env:GOARCH = $OldGoarch
  $env:CGO_ENABLED = $OldCgo
}

Copy-Item -LiteralPath (Join-Path $RepoRoot 'web-console\dist') -Destination (Join-Path $ReleaseDir 'web-desktop') -Recurse
Copy-Item -LiteralPath (Join-Path $RepoRoot 'web-console\dist') -Destination (Join-Path $ReleaseDir 'web') -Recurse
Copy-Item -LiteralPath (Join-Path $RepoRoot 'customer-mobile\build') -Destination (Join-Path $ReleaseDir 'web-customer') -Recurse
Copy-Item -LiteralPath (Join-Path $RepoRoot 'sales-mobile\build') -Destination (Join-Path $ReleaseDir 'web-sales') -Recurse

$CollectorDir = Join-Path $ReleaseDir 'collector-worker'
Copy-Item -LiteralPath (Join-Path $RepoRoot 'collector-worker\worker.mjs') -Destination $CollectorDir
Copy-Item -LiteralPath (Join-Path $RepoRoot 'collector-worker\package.json') -Destination $CollectorDir
$PlaywrightPackageJson = ((& node -e "console.log(require.resolve('playwright-core/package.json',{paths:[process.argv[1]]}))" (Join-Path $RepoRoot 'collector-worker')) | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or -not (Test-Path $PlaywrightPackageJson)) { throw 'Cannot resolve playwright-core for collector-worker.' }
$PlaywrightRoot = Split-Path $PlaywrightPackageJson -Parent
Copy-Item -LiteralPath $PlaywrightRoot -Destination (Join-Path $CollectorDir 'node_modules\playwright-core') -Recurse

$Metadata = [ordered]@{
  release_id = $ReleaseId
  git_sha = $GitSha
  dirty = $IsDirty
  built_at_utc = (Get-Date).ToUniversalTime().ToString('o')
  target = 'linux-amd64'
  web = @('desktop', 'customer-mobile', 'sales-mobile')
}
$Metadata | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $ReleaseDir 'VERSION.json') -Encoding UTF8
$ReleaseId | Set-Content -LiteralPath (Join-Path $ReleaseDir 'VERSION') -Encoding ASCII

$Tar = Get-Command tar.exe -ErrorAction SilentlyContinue
if (-not $Tar) { $Tar = Get-Command tar -ErrorAction Stop }
& $Tar.Source -czf $ArchivePath -C $ReleaseDir .
if ($LASTEXITCODE -ne 0) { throw "Failed to create archive: $ArchivePath" }

Write-Host "[release] ready: $ArchivePath"
Write-Output "__XIAOLAN_ARCHIVE__=$ArchivePath"
