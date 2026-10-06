[CmdletBinding()]
param([string]$ReleaseId = '')
$ErrorActionPreference = 'Stop'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LocalRoot = if ($env:XIAOLAN_LOCAL_ROOT) { $env:XIAOLAN_LOCAL_ROOT } else { Join-Path (Split-Path $RepoRoot -Parent) ((Split-Path $RepoRoot -Leaf) + '-local') }
if (-not $ReleaseId) { $ReleaseId = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-mainline-runtime' }
if ($ReleaseId -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$' -or $ReleaseId.Contains('..')) { throw 'Invalid release id' }
$ReleaseDir = Join-Path $LocalRoot "releases/$ReleaseId"
if (Test-Path -LiteralPath $ReleaseDir) { throw 'Release already exists' }
$Key = if ($env:XIAOLAN_SSH_KEY) { $env:XIAOLAN_SSH_KEY } else { Join-Path $env:USERPROFILE '.ssh/xiaolan-ecs-deploy' }
$Server = if ($env:XIAOLAN_PROD_HOST) { $env:XIAOLAN_PROD_HOST } else { '47.114.55.117' }
$User = if ($env:XIAOLAN_PROD_USER) { $env:XIAOLAN_PROD_USER } else { 'ecs-user' }
$Remote = "$User@$Server"
$SshArgs = @('-i', $Key, '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-o', 'StrictHostKeyChecking=yes')
$Expected = ((& ssh @SshArgs $Remote 'readlink -f /opt/xiaolan/current') | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $Expected -notmatch '^/opt/xiaolan/releases/[A-Za-z0-9._-]+$' -or $Expected.Contains('..')) { throw 'Cannot pin production base' }
New-Item -ItemType Directory -Path (Join-Path $ReleaseDir 'bin') -Force | Out-Null
$OldGoos = $env:GOOS; $OldGoarch = $env:GOARCH; $OldCgo = $env:CGO_ENABLED
Push-Location (Join-Path $RepoRoot 'management-service')
try {
  $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
  & go build -trimpath -ldflags '-s -w' -o (Join-Path $ReleaseDir 'bin/management-service') ./cmd/management
  if ($LASTEXITCODE -ne 0) { throw 'Linux management build failed' }
} finally { $env:GOOS = $OldGoos; $env:GOARCH = $OldGoarch; $env:CGO_ENABLED = $OldCgo; Pop-Location }
$Dist = Join-Path $RepoRoot 'web-console/dist'
$AssetPattern = '(?:assets/|\./)([A-Za-z0-9_-]+\.(?:js|css))'
$Assets = [Collections.Generic.HashSet[string]]::new()
$Queue = [Collections.Generic.Queue[string]]::new()
foreach ($Match in [regex]::Matches((Get-Content -LiteralPath (Join-Path $Dist 'index.html') -Raw), $AssetPattern)) {
  if ($Assets.Add($Match.Groups[1].Value)) { $Queue.Enqueue($Match.Groups[1].Value) }
}
while ($Queue.Count -gt 0) {
  $Name = $Queue.Dequeue()
  $Asset = Join-Path $Dist "assets/$Name"
  if (-not (Test-Path -LiteralPath $Asset -PathType Leaf)) { throw "Missing asset: $Name" }
  foreach ($Match in [regex]::Matches((Get-Content -LiteralPath $Asset -Raw), $AssetPattern)) {
    if ($Assets.Add($Match.Groups[1].Value)) { $Queue.Enqueue($Match.Groups[1].Value) }
  }
}
if ($Assets.Count -lt 3) { throw 'Incomplete asset graph' }
foreach ($Surface in @('web', 'web-desktop')) {
  $SurfacePath = Join-Path $ReleaseDir $Surface
  New-Item -ItemType Directory -Path (Join-Path $SurfacePath 'assets') -Force | Out-Null
  Copy-Item -LiteralPath (Join-Path $Dist 'index.html') -Destination $SurfacePath
  foreach ($Name in $Assets) { Copy-Item -LiteralPath (Join-Path $Dist "assets/$Name") -Destination (Join-Path $SurfacePath 'assets') }
}
$Tracked = @(& git -C $RepoRoot -c core.quotepath=false diff --name-only HEAD)
$Untracked = @(& git -C $RepoRoot -c core.quotepath=false ls-files --others --exclude-standard)
$Inventory = @(@($Tracked + $Untracked | Sort-Object -Unique) | ForEach-Object {
  $Path = Join-Path $RepoRoot $_
  if (Test-Path -LiteralPath $Path -PathType Leaf) { [ordered]@{ path = $_; sha256 = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() } }
})
$Manifest = [ordered]@{ release_id = $ReleaseId; git_head = ((& git -C $RepoRoot rev-parse HEAD) | Out-String).Trim(); dirty = ($Inventory.Count -gt 0); files = $Inventory; base_release = $Expected; scope = 'management-desktop'; tests = @('management go test ./... -count=1','desktop pnpm build') }
$Json = $Manifest | ConvertTo-Json -Depth 8
$ManifestPatch = "*** Begin Patch`n*** Add File: $($ReleaseDir.Replace('\','/'))/source-manifest.json`n" + (($Json -split "`r?`n" | ForEach-Object { '+' + $_ }) -join "`n") + "`n*** End Patch"
# Output is consumed by the caller, which saves the source manifest via apply_patch.
Write-Output "__MANIFEST_PATCH__=$ManifestPatch"
Write-Output "__RELEASE_DIR__=$ReleaseDir"
Write-Output "__EXPECTED__=$Expected"
Write-Output "__RELEASE_ID__=$ReleaseId"
