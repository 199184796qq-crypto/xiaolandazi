param(
  [ValidateSet("check", "import", "verify", "compare")]
  [string]$Mode = "check"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$configPath = Join-Path $projectRoot "configs\cloud-dev.local"
$backupPath = Join-Path $projectRoot "data\run\rds-migration\livecompanion-before-rds-20260923.sql"

if (-not (Test-Path -LiteralPath $configPath)) {
  throw "Missing local cloud config: $configPath"
}

$cfg = @{}
foreach ($line in [System.IO.File]::ReadAllLines($configPath)) {
  $trimmed = $line.Trim()
  if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith("#")) { continue }
  $idx = $line.IndexOf("=")
  if ($idx -le 0) { continue }
  $cfg[$line.Substring(0, $idx).Trim()] = $line.Substring($idx + 1)
}

foreach ($key in @("DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD")) {
  if (-not $cfg.ContainsKey($key) -or [string]::IsNullOrEmpty($cfg[$key])) {
    throw "Missing required cloud DB setting: $key"
  }
}

$env:MYSQL_PWD = $cfg["DB_PASSWORD"]
$env:DB_HOST = $cfg["DB_HOST"]
$env:DB_PORT = $cfg["DB_PORT"]
$env:DB_NAME = $cfg["DB_NAME"]
$env:DB_USER = $cfg["DB_USER"]

try {
  if ($Mode -eq "check") {
    & docker run --rm -e MYSQL_PWD -e DB_HOST -e DB_PORT -e DB_NAME -e DB_USER mysql:8.4 mysql --connect-timeout=60 -h $env:DB_HOST -P $env:DB_PORT -u $env:DB_USER -D $env:DB_NAME -Nse "SELECT CONCAT('cloud_ok=1;db=',DATABASE(),';user=',CURRENT_USER(),';tables=',(SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()));"
    if ($LASTEXITCODE -ne 0) { throw "Cloud MySQL check failed with exit code $LASTEXITCODE" }
    exit 0
  }

  if ($Mode -eq "import") {
    if (-not (Test-Path -LiteralPath $backupPath)) { throw "Missing migration backup: $backupPath" }
    $backupDir = Split-Path -Parent $backupPath
    & docker run --rm -e MYSQL_PWD -e DB_HOST -e DB_PORT -e DB_NAME -e DB_USER -v "$($backupDir):/migration:ro" mysql:8.4 sh -lc 'exec mysql --connect-timeout=60 -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" "$DB_NAME" < /migration/livecompanion-before-rds-20260923.sql'
    if ($LASTEXITCODE -ne 0) { throw "RDS import failed with exit code $LASTEXITCODE" }
    Write-Output "rds_import_ok=1"
    exit 0
  }

  if ($Mode -eq "compare") {
    $tables = @(& docker exec live-companion-mysql-1 mysql -uroot -plocal-root-dev -Nse "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA='livecompanion' ORDER BY TABLE_NAME;")
    if ($LASTEXITCODE -ne 0) { throw "Failed to read local table list" }
    if ($tables.Count -eq 0) { throw "Local database has no tables" }

    $bt = [char]96
    $parts = foreach ($table in $tables) {
      $safeName = $table.Replace([string]$bt, ([string]$bt + [string]$bt))
      $safeLiteral = $table.Replace("'", "''")
      "SELECT '$safeLiteral' AS table_name, COUNT(*) AS row_count FROM $bt$safeName$bt"
    }
    $countSql = ($parts -join " UNION ALL ") + ";"

    $localCounts = @(& docker exec live-companion-mysql-1 mysql -uroot -plocal-root-dev -D livecompanion -Nse $countSql)
    if ($LASTEXITCODE -ne 0) { throw "Failed to count local rows" }

    $cloudCounts = @(& docker run --rm -e MYSQL_PWD -e DB_HOST -e DB_PORT -e DB_NAME -e DB_USER mysql:8.4 mysql --connect-timeout=60 -h $env:DB_HOST -P $env:DB_PORT -u $env:DB_USER -D $env:DB_NAME -Nse $countSql)
    if ($LASTEXITCODE -ne 0) { throw "Failed to count RDS rows" }

    $diff = @(Compare-Object -ReferenceObject $localCounts -DifferenceObject $cloudCounts)
    if ($diff.Count -gt 0) {
      throw ("Exact row-count comparison failed for {0} table result(s)" -f $diff.Count)
    }

    $totalRows = 0L
    foreach ($line in $localCounts) {
      $parts = $line -split [char]9
      if ($parts.Count -ge 2) { $totalRows += [int64]$parts[1] }
    }
    Write-Output ("exact_compare_ok=1;tables={0};rows={1}" -f $tables.Count, $totalRows)
    exit 0
  }

  & docker run --rm -e MYSQL_PWD -e DB_HOST -e DB_PORT -e DB_NAME -e DB_USER mysql:8.4 mysql --connect-timeout=60 -h $env:DB_HOST -P $env:DB_PORT -u $env:DB_USER -D $env:DB_NAME -Nse "SELECT CONCAT('tables=',COUNT(*),', rows=',COALESCE(SUM(TABLE_ROWS),0),', data_mb=',ROUND(COALESCE(SUM(DATA_LENGTH+INDEX_LENGTH),0)/1024/1024,2)) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE();"
  if ($LASTEXITCODE -ne 0) { throw "RDS verification failed with exit code $LASTEXITCODE" }
} finally {
  foreach ($name in @("MYSQL_PWD", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER")) {
    Remove-Item "Env:\$name" -ErrorAction SilentlyContinue
  }
}
