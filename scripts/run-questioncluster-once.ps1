param(
  [Parameter(Mandatory = $true)][long]$TenantID,
  [Parameter(Mandatory = $true)][long]$RoomID,
  [switch]$AllSmall
)

$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'
$Exe = Join-Path $Root 'management-service\bin\questioncluster-once.exe'

if (-not (Test-Path -LiteralPath $ConfigPath)) {
  throw "Missing local cloud config: $ConfigPath"
}
foreach ($line in [System.IO.File]::ReadAllLines($ConfigPath)) {
  $trimmed = $line.Trim()
  if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }
  $idx = $line.IndexOf('=')
  if ($idx -le 0) { continue }
  $name = $line.Substring(0, $idx).Trim()
  $value = $line.Substring($idx + 1)
  if ([string]::IsNullOrWhiteSpace($name)) { continue }
  [Environment]::SetEnvironmentVariable($name, $value, 'Process')
}
if (-not (Test-Path -LiteralPath $Exe)) {
  throw "Question cluster utility not found: $Exe"
}

$args = @('-tenant', [string]$TenantID, '-room', [string]$RoomID)
if ($AllSmall) { $args += '-all-small' }
Set-Location $Root
& $Exe @args
exit $LASTEXITCODE
