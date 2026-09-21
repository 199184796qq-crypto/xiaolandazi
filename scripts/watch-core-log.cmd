@echo off
setlocal
chcp 65001 >nul

cd /d "%~dp0.."
set "CORE_LOG_FILE=%CD%\data\logs\core-service.log"

echo.
echo [Live Companion] Core Service live log
echo [Live Companion] %CORE_LOG_FILE%
echo [Live Companion] Press Ctrl+C to close this window.
echo.

if not exist "%CORE_LOG_FILE%" (
  echo [ERROR] Core log file does not exist yet.
  echo [ERROR] Start Core Service first.
  echo.
  pause
  exit /b 1
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -LiteralPath $env:CORE_LOG_FILE -Encoding UTF8 -Tail 100 -Wait"

echo.
pause