@echo off
chcp 65001 >nul

for %%P in (5173 8080 8081) do (
  for /f "tokens=5" %%A in ('netstat -ano ^| findstr ":%%P " ^| findstr "LISTENING"') do (
    taskkill /PID %%A /T /F >nul 2>nul
  )
)

echo 直播伴播开发服务及其采集子进程已停止。
pause