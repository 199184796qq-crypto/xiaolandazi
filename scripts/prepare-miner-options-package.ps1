[CmdletBinding()]
param([Parameter(Mandatory=$true)][string]$ReleaseId)
$ErrorActionPreference = 'Stop'
if ($ReleaseId -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$' -or $ReleaseId.Contains('..')) { throw 'Invalid release id' }
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$packageRoot = Join-Path $repoRoot "deploy/$ReleaseId"
$cleanWebRoot = Join-Path $repoRoot "deploy/$ReleaseId-clean-web"
$archive = Join-Path $repoRoot "deploy/$ReleaseId.tar.gz"
foreach ($target in @($packageRoot, $cleanWebRoot, $archive)) {
  if (Test-Path -LiteralPath $target) { throw "Output already exists: $target" }
}
New-Item -ItemType Directory -Path (Join-Path $packageRoot 'bin') -Force | Out-Null
$priorTargetOS = $env:GOOS; $priorTargetArch = $env:GOARCH; $priorCgo = $env:CGO_ENABLED
try {
  $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
  Push-Location (Join-Path $repoRoot 'management-service')
  try {
    & go build -trimpath -ldflags '-s -w' -o (Join-Path $packageRoot 'bin/management-service') ./cmd/management
    if ($LASTEXITCODE -ne 0) { throw 'Linux management build failed' }
  } finally { Pop-Location }
} finally { $env:GOOS = $priorTargetOS; $env:GOARCH = $priorTargetArch; $env:CGO_ENABLED = $priorCgo }
Push-Location (Join-Path $repoRoot 'web-console')
try {
  & npx vite build --outDir $cleanWebRoot --emptyOutDir
  if ($LASTEXITCODE -ne 0) { throw 'Web build failed' }
} finally { Pop-Location }
foreach ($surface in @('web','web-desktop')) {
  Copy-Item -LiteralPath $cleanWebRoot -Destination (Join-Path $packageRoot $surface) -Recurse
}
. (Join-Path $PSScriptRoot 'copy-prompt-mothers.ps1')
$motherRoot = Join-Path $packageRoot 'configs/prompts/anchor-style'
$manifest = @(Copy-PromptMotherAssets -SourceRoot (Join-Path $repoRoot 'configs/prompts/anchor-style') -DestinationRoot $motherRoot)
# Generated packaging metadata, not a hand-maintained list of mother files.
[IO.File]::WriteAllText((Join-Path $motherRoot 'MANIFEST.json'), (ConvertTo-Json -InputObject $manifest -Depth 4), [Text.UTF8Encoding]::new($false))
Copy-Item -LiteralPath (Join-Path $repoRoot 'configs/fact-categories.json') -Destination (Join-Path $packageRoot 'configs/fact-categories.json')
& tar.exe -czf $archive -C $packageRoot bin web web-desktop configs
if ($LASTEXITCODE -ne 0) { throw 'Archive failed' }
$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
Write-Output "RELEASE=$ReleaseId"
Write-Output "ARCHIVE=$archive"
Write-Output "SHA256=$hash"
Write-Output "PROMPT_MOTHERS=$($manifest.Count)"
