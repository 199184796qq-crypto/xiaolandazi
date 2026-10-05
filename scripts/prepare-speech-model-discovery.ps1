[CmdletBinding()]
param([string]$ReleaseId='20261004-speech-model-discovery-v1')
$ErrorActionPreference='Stop'
if ($ReleaseId -notmatch '^[A-Za-z0-9._-]+$' -or $ReleaseId.Contains('..')) { throw 'Invalid release id' }
$taskRepo=(Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$taskRoot='E:\直播伴播-local\releases'
$taskRelease=Join-Path $taskRoot $ReleaseId
if (Test-Path -LiteralPath $taskRelease) { throw 'Release already exists' }
# Ensure the rebuilt main is byte-identical to the current page after stripping
# content-hash references: only the isolated model-settings chunk changes.
Push-Location $taskRepo
try {
  & node -e 'const fs=require("fs");const a=fs.readFileSync("web-console/dist/assets/index-CbTPqK72.js","utf8");const b=fs.readFileSync("web-console/dist/assets/index-B4uJztIB.js","utf8");const n=s=>s.replace(/([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)/g,"$1-HASH.$2");if(n(a)!==n(b))throw Error("unrelated main bundle changed");console.log("Main page unchanged except hash references");'
  if ($LASTEXITCODE -ne 0) {throw 'Main comparison failed'}
} finally {Pop-Location}
New-Item -ItemType Directory -Path (Join-Path $taskRelease 'bin') -Force | Out-Null
foreach ($surface in @('web','web-desktop')) { Copy-Item -LiteralPath (Join-Path $taskRepo 'web-console\dist') -Destination (Join-Path $taskRelease $surface) -Recurse }
$taskGoos=$env:GOOS; $taskGoarch=$env:GOARCH; $taskCgo=$env:CGO_ENABLED
try {
 $env:GOOS='linux'; $env:GOARCH='amd64'; $env:CGO_ENABLED='0'
 Push-Location (Join-Path $taskRepo 'management-service')
 try { & go build -trimpath -ldflags '-s -w' -o (Join-Path $taskRelease 'bin\management-service') ./cmd/management; if($LASTEXITCODE-ne 0){throw 'Build failed'} } finally{Pop-Location}
} finally {$env:GOOS=$taskGoos;$env:GOARCH=$taskGoarch;$env:CGO_ENABLED=$taskCgo}
$taskLines=@(& git -C $taskRepo -c core.quotepath=false status --porcelain=v1)
$taskFiles=@($taskLines | ForEach-Object{
 $taskLine=[string]$_;$taskPath=$taskLine.Substring(3).Trim('"');$taskEntry=[ordered]@{path=$taskPath;status=$taskLine.Substring(0,2)}
 $taskSource=Join-Path $taskRepo $taskPath
 if(Test-Path -LiteralPath $taskSource -PathType Leaf){$taskEntry.sha256=(Get-FileHash -LiteralPath $taskSource).Hash.ToLowerInvariant()}
 $taskEntry
})
$taskManifest=Join-Path $taskRoot "$ReleaseId-dirty-manifest.json"
[ordered]@{git_head=((& git -C $taskRepo rev-parse HEAD)|Out-String).Trim();dirty=$true;scope=@('bin/management-service','web','web-desktop');files=$taskFiles}|ConvertTo-Json -Depth 6|Set-Content -LiteralPath $taskManifest -Encoding utf8NoBOM
$taskArchive=Join-Path $taskRoot "$ReleaseId.tar.gz"
& tar -czf $taskArchive -C $taskRelease bin/management-service web web-desktop
if($LASTEXITCODE-ne 0){throw 'Archive failed'}
[ordered]@{archive=$taskArchive;archive_sha=(Get-FileHash -LiteralPath $taskArchive).Hash.ToLowerInvariant();binary_sha=(Get-FileHash -LiteralPath (Join-Path $taskRelease 'bin\management-service')).Hash.ToLowerInvariant();manifest=$taskManifest;manifest_sha=(Get-FileHash -LiteralPath $taskManifest).Hash.ToLowerInvariant()}|ConvertTo-Json
