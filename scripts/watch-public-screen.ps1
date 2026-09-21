param(
    [string]$Room = "",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$projectRoot = Split-Path -Parent $PSScriptRoot
$logFile = Join-Path $projectRoot "data\logs\core-service.log"

if ($SelfTest) {
    Write-Host "SELF_TEST_OK"
    Write-Host ("LOG_FILE=" + $logFile)
    exit 0
}

if ([string]::IsNullOrWhiteSpace($Room)) {
    Write-Host ""
    $Room = Read-Host "输入 Core 房间 ID 或抖音房间号（直接回车看全部）"
}

Write-Host ""
Write-Host "============================================"
Write-Host "  直播伴播 - 公屏监控"
Write-Host "============================================"
if ([string]::IsNullOrWhiteSpace($Room)) {
    Write-Host "房间: 全部"
} else {
    Write-Host ("房间过滤: " + $Room)
}
Write-Host "只显示: 弹幕 / 进房 / 点赞 / 关注 / 礼物"
Write-Host "按 Ctrl+C 只关闭这个查看窗口，不会停止 Core。"
Write-Host ""

if (-not (Test-Path -LiteralPath $logFile)) {
    Write-Host "[ERROR] Core 日志文件不存在。" -ForegroundColor Red
    Write-Host $logFile -ForegroundColor Red
    return
}

Get-Content -LiteralPath $logFile -Encoding UTF8 -Tail 100 -Wait |
    Where-Object {
        if ($_ -notmatch "\[PUBLIC\]") {
            return $false
        }

        if ([string]::IsNullOrWhiteSpace($Room)) {
            return $true
        }

        $escaped = [regex]::Escape($Room)
        return (
            $_ -match ("room=" + $escaped + "(\s|$)") -or
            $_ -match ("web_rid=" + $escaped + "(\s|$)")
        )
    }