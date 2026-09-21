param(
    [string]$Room = "",
    [ValidateSet("public", "all", "errors")]
    [string]$Mode = "",
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

$projectRoot = Split-Path -Parent $PSScriptRoot
$logFile = Join-Path $projectRoot "data\logs\core-service.log"

function Test-RoomMatch {
    param(
        [string]$Line,
        [string]$RoomFilter
    )

    if ([string]::IsNullOrWhiteSpace($RoomFilter)) {
        return $true
    }

    $escaped = [regex]::Escape($RoomFilter)
    return (
        $Line -match ("room=" + $escaped + "(\s|$)") -or
        $Line -match ("web_rid=" + $escaped + "(\s|$)")
    )
}

if ($SelfTest) {
    Write-Host "SELF_TEST_OK"
    Write-Host ("LOG_FILE=" + $logFile)
    exit 0
}

Write-Host ""
Write-Host "============================================"
Write-Host "  直播伴播 - 房间日志监控"
Write-Host "============================================"
Write-Host ""

if ([string]::IsNullOrWhiteSpace($Room)) {
    Write-Host "可以输入 Core 房间 ID，例如 7"
    Write-Host "也可以输入抖音房间号，例如 917740116124"
    Write-Host "直接回车表示查看全部房间"
    Write-Host ""
    $Room = Read-Host "房间过滤"
}

if ([string]::IsNullOrWhiteSpace($Mode)) {
    Write-Host ""
    Write-Host "查看模式："
    Write-Host "  1 = 只看公屏"
    Write-Host "  2 = 看该房间全部日志"
    Write-Host "  3 = 只看错误/异常"
    Write-Host ""
    $choice = Read-Host "请选择 [1/2/3，默认 1]"

    switch ($choice) {
        "2" { $Mode = "all" }
        "3" { $Mode = "errors" }
        default { $Mode = "public" }
    }
}

Write-Host ""
Write-Host ("日志文件: " + $logFile)
if ([string]::IsNullOrWhiteSpace($Room)) {
    Write-Host "房间: 全部"
} else {
    Write-Host ("房间过滤: " + $Room)
}
Write-Host ("模式: " + $Mode)
Write-Host "按 Ctrl+C 只关闭这个查看窗口，不会停止 Core。"
Write-Host ""

if (-not (Test-Path -LiteralPath $logFile)) {
    Write-Host "[ERROR] Core 日志文件不存在。" -ForegroundColor Red
    Write-Host $logFile -ForegroundColor Red
    Write-Host ""
    Write-Host "请先启动 Core Service。"
    return
}

Get-Content -LiteralPath $logFile -Encoding UTF8 -Tail 150 -Wait |
    Where-Object {
        $line = $_

        if (-not (Test-RoomMatch -Line $line -RoomFilter $Room)) {
            return $false
        }

        switch ($Mode) {
            "public" {
                return $line -match "\[PUBLIC\]"
            }
            "errors" {
                return $line -match "(?i)error|failed|offline|timeout|panic|fatal|warn|异常|失败"
            }
            default {
                return $true
            }
        }
    }