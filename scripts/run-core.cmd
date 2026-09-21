@echo off
setlocal EnableExtensions
chcp 65001 >nul

cd /d "%~dp0.."

set "CORE_LOG_FILE=%CD%\data\logs\core-service.log"
set "CORE_PID="

for /f "tokens=5" %%P in ('netstat -ano ^| findstr "127.0.0.1:8081" ^| findstr "LISTENING"') do (
  set "CORE_PID=%%P"
)

if defined CORE_PID (
  tasklist /FI "PID eq %CORE_PID%" /FO CSV /NH 2>nul | findstr /I "core-service.exe" >nul
  if not errorlevel 1 (
    echo.
    echo [Live Companion] Core Service is already running.
    echo [Live Companion] PID: %CORE_PID%
    echo [Live Companion] Showing live log:
    echo [Live Companion] %CORE_LOG_FILE%
    echo [Live Companion] Press Ctrl+C to close this log window.
    echo [Live Companion] Core will continue running in the background.
    echo.

    if not exist "%CORE_LOG_FILE%" (
      echo [ERROR] Log file does not exist yet:
      echo %CORE_LOG_FILE%
      echo.
      pause
      exit /b 1
    )

    powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-Content -LiteralPath $env:CORE_LOG_FILE -Encoding UTF8 -Tail 80 -Wait"
    echo.
    pause
    exit /b 0
  )

  echo.
  echo [ERROR] Port 8081 is already occupied by PID %CORE_PID%.
  echo [ERROR] It is not core-service.exe.
  echo [ERROR] Please release port 8081 before starting Core Service.
  echo.
  tasklist /FI "PID eq %CORE_PID%"
  echo.
  pause
  exit /b 1
)

echo.
echo [Live Companion] Starting Core Service...
echo [Live Companion] Log file: %CORE_LOG_FILE%
echo [Live Companion] Press Ctrl+C to stop Core Service.
echo.

"core-service\bin\core-service.exe"
set "EXIT_CODE=%ERRORLEVEL%"

echo.
echo [Live Companion] Core Service exited.
echo [Live Companion] Exit code: %EXIT_CODE%
echo.
pause
exit /b %EXIT_CODE%