$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ConfigPath = Join-Path $Root 'configs\cloud-dev.local'
$LogDir = Join-Path $Root 'data\logs'
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

if (Test-Path -LiteralPath $ConfigPath) {
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
}

$node = (Get-Command node.exe -ErrorAction Stop).Source
$vite = Join-Path $Root 'device-simulator\node_modules\vite\bin\vite.js'
$logFile = Join-Path $LogDir 'device-simulator.task.log'
if (-not (Test-Path -LiteralPath $vite)) { throw "Vite not found: $vite" }

Set-Location (Join-Path $Root 'device-simulator')
Add-Content -LiteralPath $logFile -Value ('[launcher] device-simulator started at {0:o}' -f (Get-Date))
& $node $vite --host 127.0.0.1 --port 5176 *>> $logFile
exit $LASTEXITCODE
