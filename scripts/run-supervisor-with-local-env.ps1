$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'
$SupervisorExe = Join-Path $Root 'supervisor\bin\livecompanion-supervisor.exe'
$SupervisorConfig = Join-Path $Root 'configs\supervisor.json'

function Import-LocalEnvFile([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return
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

if (-not (Test-Path -LiteralPath $SupervisorExe)) {
    throw "Supervisor binary not found: $SupervisorExe"
}
if (-not (Test-Path -LiteralPath $SupervisorConfig)) {
    throw "Supervisor config not found: $SupervisorConfig"
}

Set-Location $Root
$arguments = '-root "' + $Root + '" -config "' + $SupervisorConfig + '"'
$process = Start-Process -FilePath $SupervisorExe -ArgumentList $arguments -WorkingDirectory $Root -PassThru -Wait
exit $process.ExitCode
