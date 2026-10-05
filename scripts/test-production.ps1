[CmdletBinding()]
param(
  [string]$MainUrl = 'https://www.xiaolandaizi.cn',
  [string]$SalesUrl = 'https://sales.xiaolandaizi.cn',
  [switch]$SkipSales,
  [switch]$SkipXiaozhi
)

$ErrorActionPreference = 'Stop'

function Assert-AppMarker {
  param(
    [string]$Url,
    [string]$UserAgent,
    [string]$Expected
  )
  $Response = Invoke-WebRequest -UseBasicParsing -Uri $Url -Headers @{ 'User-Agent' = $UserAgent } -TimeoutSec 15
  if ([int]$Response.StatusCode -ne 200) { throw "$Url returned HTTP $($Response.StatusCode)" }
  $Marker = 'name="xiaolan-app" content="' + $Expected + '"'
  if ($Response.Content -notlike ('*' + $Marker + '*')) { throw "$Url did not serve expected app: $Expected" }
  Write-Host "[smoke] $Expected ok -> $Url"
}

$DesktopUA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/154 Safari/537.36'
$MobileUA = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'

Assert-AppMarker -Url ($MainUrl.TrimEnd('/') + '/') -UserAgent $DesktopUA -Expected 'desktop'
Assert-AppMarker -Url ($MainUrl.TrimEnd('/') + '/') -UserAgent $MobileUA -Expected 'customer-mobile'

$PublicConfig = Invoke-WebRequest -UseBasicParsing -Uri ($MainUrl.TrimEnd('/') + '/api/v1/system/public-config') -TimeoutSec 15
if ([int]$PublicConfig.StatusCode -ne 200) { throw 'Public API smoke test failed.' }
Write-Host '[smoke] management API ok'

$CoreHealth = Invoke-WebRequest -UseBasicParsing -Uri ($MainUrl.TrimEnd('/') + '/core-audio/healthz') -TimeoutSec 15
if ([int]$CoreHealth.StatusCode -ne 200) { throw 'Core proxy smoke test failed.' }
Write-Host '[smoke] core proxy ok'

if (-not $SkipXiaozhi) {
  $XiaozhiHealth = Invoke-RestMethod -Uri ($MainUrl.TrimEnd('/') + '/xiaozhi/healthz') -TimeoutSec 15
  if ($XiaozhiHealth.status -ne 'ok' -or -not $XiaozhiHealth.ffmpeg_available) { throw 'Xiaozhi public health smoke test failed.' }

  $XiaozhiHeaders = @{ 'Device-Id' = '02:00:00:00:ff:fe'; 'Client-Id' = 'production-smoke' }
  $XiaozhiBody = '{"application":{"version":"production-smoke"}}'
  $XiaozhiOTA = Invoke-RestMethod -Method Post -Uri ($MainUrl.TrimEnd('/') + '/xiaozhi/ota/') -Headers $XiaozhiHeaders -ContentType 'application/json' -Body $XiaozhiBody -TimeoutSec 15
  if ($XiaozhiOTA.websocket.url -ne ($MainUrl.TrimEnd('/').Replace('https://', 'wss://') + '/xiaozhi/v1/')) { throw 'Xiaozhi OTA websocket URL smoke test failed.' }
  if ([string]::IsNullOrWhiteSpace([string]$XiaozhiOTA.websocket.token) -or [int]$XiaozhiOTA.websocket.version -ne 1) { throw 'Xiaozhi OTA auth/version smoke test failed.' }
  Write-Host '[smoke] xiaozhi health + OTA ok'
}

if (-not $SkipSales) {
  Assert-AppMarker -Url ($SalesUrl.TrimEnd('/') + '/') -UserAgent $MobileUA -Expected 'sales-mobile'
  $SalesApi = Invoke-WebRequest -UseBasicParsing -Uri ($SalesUrl.TrimEnd('/') + '/api/v1/system/public-config') -TimeoutSec 15
  if ([int]$SalesApi.StatusCode -ne 200) { throw 'Sales API smoke test failed.' }
  Write-Host '[smoke] sales API ok'
}

Write-Host '[smoke] production checks passed'
