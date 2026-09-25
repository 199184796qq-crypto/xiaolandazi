param(
  [Parameter(Mandatory = $true)]
  [ValidateSet("management", "core", "audio")]
  [string]$Service
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'

function Import-LocalEnvFile([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "Missing local cloud config: $Path"
    }

    foreach ($line in [System.IO.File]::ReadAllLines($Path)) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) { continue }
        $idx = $line.IndexOf('=')
        if ($idx -le 0) { continue }
        $name = $line.Substring(0, $idx).Trim()
        $value = $line.Substring($idx + 1)
        if ([string]::IsNullOrWhiteSpace($name)) { continue }
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }
}

Import-LocalEnvFile $ConfigPath

if ($Service -eq 'management') {
    $env:MGMT_AVATAR_DIR = Join-Path $Root 'data\avatars'
    New-Item -ItemType Directory -Force -Path $env:MGMT_AVATAR_DIR | Out-Null
    $Exe = Join-Path $Root 'management-service\bin\management-service.exe'
} elseif ($Service -eq 'audio') {
    $Exe = Join-Path $Root 'audio-service\bin\audio-service.exe'
} else {
    $Exe = Join-Path $Root 'core-service\bin\core-service.exe'
}

if (-not (Test-Path -LiteralPath $Exe)) {
    throw "Service binary not found: $Exe"
}

Set-Location $Root
& $Exe
exit $LASTEXITCODE
