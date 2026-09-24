param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('management', 'core')]
    [string]$Service
)

$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LogDir = Join-Path $Root 'data\logs'
$AvatarDir = Join-Path $Root 'data\avatars'
$CloudDevConfig = Join-Path $Root 'configs\cloud-dev.local'

New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

function Import-LocalEnvFile([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return $false
    }

    foreach ($line in [System.IO.File]::ReadAllLines($Path)) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith('#')) {
            continue
        }

        $idx = $line.IndexOf('=')
        if ($idx -le 0) {
            continue
        }

        $name = $line.Substring(0, $idx).Trim()
        $value = $line.Substring($idx + 1)
        if ([string]::IsNullOrWhiteSpace($name)) {
            continue
        }

        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }

    return $true
}

if (-not (Import-LocalEnvFile $CloudDevConfig)) {
    throw "Cloud development config not found: $CloudDevConfig"
}

if ([string]::IsNullOrWhiteSpace($env:DB_HOST)) {
    throw 'DB_HOST is empty after loading configs\cloud-dev.local'
}

switch ($Service) {
    'management' {
        New-Item -ItemType Directory -Force -Path $AvatarDir | Out-Null
        $env:MGMT_AVATAR_DIR = $AvatarDir
        $Executable = Join-Path $Root 'management-service\bin\management-service.exe'
        $LogFile = Join-Path $LogDir 'management-service.task.log'
    }
    'core' {
        $Executable = Join-Path $Root 'core-service\bin\core-service.exe'
        $LogFile = Join-Path $LogDir 'core-service.task.log'
    }
}

if (-not (Test-Path -LiteralPath $Executable)) {
    throw "Backend executable not found: $Executable"
}

$maskedDBHost = if ($env:DB_HOST.Length -gt 8) {
    $env:DB_HOST.Substring(0, 4) + '***' + $env:DB_HOST.Substring($env:DB_HOST.Length - 4)
} else {
    '***'
}

Add-Content -LiteralPath $LogFile -Value (
    '[launcher] service={0} db_host={1} db_port={2} db_name={3}' -f
        $Service,
        $maskedDBHost,
        $env:DB_PORT,
        $env:DB_NAME
)

$commandLine = '"' + $Executable + '" >> "' + $LogFile + '" 2>&1'
& $env:ComSpec /d /s /c $commandLine
exit $LASTEXITCODE
