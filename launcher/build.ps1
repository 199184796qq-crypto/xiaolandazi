$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$Source = Join-Path $PSScriptRoot 'Program.cs'
$BinDir = Join-Path $PSScriptRoot 'bin'
$Output = Join-Path $BinDir 'XiaolanLiveLauncher.exe'
$RootOutput = Join-Path $Root 'XiaolanLiveLauncher.exe'

New-Item -ItemType Directory -Path $BinDir -Force | Out-Null

$CscCandidates = @(
    "$env:WINDIR\Microsoft.NET\Framework64\v4.0.30319\csc.exe",
    "$env:WINDIR\Microsoft.NET\Framework\v4.0.30319\csc.exe"
)
$Csc = $CscCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $Csc) {
    throw 'Windows .NET Framework C# compiler was not found.'
}

& $Csc /nologo /target:winexe /optimize+ /platform:anycpu /out:"$Output" /reference:System.dll /reference:System.Core.dll /reference:System.Drawing.dll /reference:System.Windows.Forms.dll "$Source"

if ($LASTEXITCODE -ne 0) {
    throw "Launcher compilation failed with exit code $LASTEXITCODE"
}

Copy-Item -LiteralPath $Output -Destination $RootOutput -Force
Write-Host "Built: $RootOutput"
