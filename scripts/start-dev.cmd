@echo off
chcp 65001 >nul
cd /d E:\直播伴播

echo [1/4] 启动 MySQL / Redis...
docker compose -f docker\compose.yaml up -d
if errorlevel 1 (
  echo Docker 启动失败，请先打开 Docker Desktop。
  pause
  exit /b 1
)

echo [2/4] 启动 Core Service...
start "LiveCompanion Core" /min "E:\直播伴播\core-service\bin\core-service.exe"
timeout /t 2 /nobreak >nul

echo [3/4] 启动 Management Service...
start "LiveCompanion Management" /min "E:\直播伴播\management-service\bin\management-service.exe"
timeout /t 2 /nobreak >nul

echo [4/4] 启动 Web Console...
start "LiveCompanion Web" /min "C:\Users\19918\AppData\Roaming\fnm\node-versions\v24.21.0\installation\node.exe" "E:\直播伴播\web-console\node_modules\vite\bin\vite.js" "E:\直播伴播\web-console" --host 127.0.0.1 --port 5173
timeout /t 2 /nobreak >nul

echo.
echo Web Console: http://127.0.0.1:5173/
echo Management:  http://127.0.0.1:8080/
echo Core:        http://127.0.0.1:8081/
echo.
start "" "http://127.0.0.1:5173/"