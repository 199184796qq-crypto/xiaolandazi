$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$LogDir = Join-Path $Root 'data\logs'
New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

$pnpm = (Get-Command pnpm.cmd -ErrorAction Stop).Source
$logFile = Join-Path $LogDir 'web-console.task.log'

Set-Location $Root
Add-Content -LiteralPath $logFile -Value ('[launcher] web-console started at {0:o}' -f (Get-Date))

$commandLine = '"' + $pnpm + '" -C "' + $Root + '" --filter web-console dev --host 127.0.0.1 --port 5184 >> "' + $logFile + '" 2>&1'
& $env:ComSpec /d /s /c $commandLine
exit $LASTEXITCODE
