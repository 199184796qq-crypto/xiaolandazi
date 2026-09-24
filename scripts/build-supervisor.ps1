$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$OutputDir = Join-Path $Root 'supervisor/bin'
$Output = Join-Path $OutputDir 'livecompanion-supervisor.exe'
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

$goCommand = Get-Command go.exe -ErrorAction SilentlyContinue
$go = if ($goCommand) { $goCommand.Source } else { 'C:/Program Files/Go/bin/go.exe' }
if (-not (Test-Path $go)) { throw 'Go executable not found' }
Push-Location $Root
try {
    & $go build -ldflags '-H=windowsgui' -o $Output ./supervisor/cmd/supervisor
    if ($LASTEXITCODE -ne 0) {
        throw "supervisor build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

Write-Host ('[OK] built {0}' -f $Output)
