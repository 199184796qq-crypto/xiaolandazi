# Start the standalone "fact library recall + rerank" lab page.
#
# It is read-only and does not touch production services:
#   1. reads DB/DashScope settings from /etc/xiaolan/management.env over SSH (memory only);
#   2. opens an SSH tunnel to the production database;
#   3. starts fact-rerank-lab on http://127.0.0.1:8123.
#
# Usage: powershell -NoProfile -File scripts/start-fact-rerank-lab.ps1 [-Port 8123] [-TunnelPort 13306]
param(
    [int]$Port = 8123,
    [int]$TunnelPort = 13306,
    [string]$SshHost = 'xiaolan-prod'
)

$ErrorActionPreference = 'Stop'
$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Module = Join-Path $Root 'management-service'
$Exe = Join-Path $Module 'bin\fact-rerank-lab.exe'

function Read-ProdEnv {
    $raw = ssh -o BatchMode=yes -o ConnectTimeout=10 $SshHost "sudo -n cat /etc/xiaolan/management.env" 2>$null
    if (-not $raw) { throw "cannot read /etc/xiaolan/management.env from $SshHost" }
    $map = @{}
    foreach ($line in ($raw -split "`n")) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        $index = $trimmed.IndexOf('=')
        if ($index -le 0) { continue }
        $name = $trimmed.Substring(0, $index).Trim()
        $value = $trimmed.Substring($index + 1).Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $map[$name] = $value
    }
    return $map
}

$config = Read-ProdEnv
foreach ($required in @('DB_HOST', 'DB_NAME', 'DB_USER', 'DB_PASSWORD', 'DASHSCOPE_API_KEY')) {
    if (-not $config[$required]) { throw "production config is missing $required" }
}

Write-Host ("tunnel: 127.0.0.1:{0} -> {1}:3306 via {2}" -f $TunnelPort, $config['DB_HOST'], $SshHost)
$tunnel = Start-Process -FilePath 'ssh' -ArgumentList @(
    '-N', '-o', 'BatchMode=yes', '-o', 'ExitOnForwardFailure=yes',
    '-o', 'ServerAliveInterval=30', '-o', 'ServerAliveCountMax=3',
    '-L', "$TunnelPort`:$($config['DB_HOST']):3306", $SshHost
) -PassThru -WindowStyle Hidden
Start-Sleep -Seconds 2
if ($tunnel.HasExited) { throw 'SSH tunnel failed to start (port busy or forwarding refused)' }

try {
    if (-not (Test-Path -LiteralPath $Exe)) {
        Push-Location $Module
        try { go build -o $Exe .\cmd\fact-rerank-lab } finally { Pop-Location }
    }

    $env:DB_HOST = '127.0.0.1'
    $env:DB_PORT = "$TunnelPort"
    $env:DB_NAME = $config['DB_NAME']
    $env:DB_USER = $config['DB_USER']
    $env:DB_PASSWORD = $config['DB_PASSWORD']
    $env:DASHSCOPE_API_KEY = $config['DASHSCOPE_API_KEY']
    if ($config['DASHSCOPE_EMBEDDING_MODEL']) { $env:DASHSCOPE_EMBEDDING_MODEL = $config['DASHSCOPE_EMBEDDING_MODEL'] }
    if ($config['DASHSCOPE_EMBEDDING_BASE_URL']) { $env:DASHSCOPE_EMBEDDING_BASE_URL = $config['DASHSCOPE_EMBEDDING_BASE_URL'] }
    if ($config['MODEL_CONFIG_ENCRYPTION_KEY']) { $env:MODEL_CONFIG_ENCRYPTION_KEY = $config['MODEL_CONFIG_ENCRYPTION_KEY'] }
    $env:DASHSCOPE_RERANK_ENABLED = 'true'
    $env:DASHSCOPE_RERANK_MODEL = 'qwen3.7-text-rerank'
    $env:DASHSCOPE_RERANK_BASE_URL = 'https://dashscope.aliyuncs.com/api/v1'

    Write-Host ("open http://127.0.0.1:{0} in the browser (Ctrl+C to stop)" -f $Port)
    & $Exe -addr "127.0.0.1:$Port"
} finally {
    if ($tunnel -and -not $tunnel.HasExited) { Stop-Process -Id $tunnel.Id -Force -ErrorAction SilentlyContinue }
}
