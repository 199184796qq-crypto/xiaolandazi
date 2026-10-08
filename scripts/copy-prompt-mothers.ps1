function Copy-PromptMotherAssets {
  [CmdletBinding()]
  param([Parameter(Mandatory=$true)][string]$SourceRoot,[Parameter(Mandatory=$true)][string]$DestinationRoot)
  $sourcePath = (Resolve-Path -LiteralPath $SourceRoot -ErrorAction Stop).Path
  $sourceManifestPath = Join-Path $sourcePath 'MANIFEST.json'
  $files = @(Get-ChildItem -LiteralPath $sourcePath -File -Recurse | Where-Object { $_.FullName -ne $sourceManifestPath } | Sort-Object FullName)
  if ($files.Count -eq 0) { throw 'Prompt mother directory is empty.' }
  $manifest = @()
  foreach ($file in $files) {
    $relative = $file.FullName.Substring($sourcePath.Length).TrimStart([char[]]'\/')
    $target = Join-Path $DestinationRoot $relative
    New-Item -ItemType Directory -Force -Path (Split-Path $target -Parent) | Out-Null
    $sourceHash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    Copy-Item -LiteralPath $file.FullName -Destination $target -Force
    if ((Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash.ToLowerInvariant() -ne $sourceHash) { throw "Prompt mother changed while packaging: $relative" }
    $manifest += [ordered]@{path=$relative.Replace([char]92,[char]47);sha256=$sourceHash}
  }
  return $manifest
}
