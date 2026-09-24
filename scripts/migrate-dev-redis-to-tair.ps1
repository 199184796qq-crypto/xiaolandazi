param(
  [ValidateSet("check", "copy", "verify")]
  [string]$Mode = "check",
  [switch]$ReplaceTarget
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

Push-Location (Join-Path $Root 'management-service')
try {
  $Args = @(
    'run',
    './cmd/redis_migrate',
    '-mode', $Mode,
    '-config', '..\configs\cloud-dev.local'
  )
  if ($ReplaceTarget) {
    $Args += '-replace-target'
  }
  & go @Args
  if ($LASTEXITCODE -ne 0) {
    throw "Redis migration command failed with exit code $LASTEXITCODE"
  }
} finally {
  Pop-Location
}
