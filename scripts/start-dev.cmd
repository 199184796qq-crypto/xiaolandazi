@echo off
setlocal
chcp 65001 >nul
set "ROOT=%~dp0.."
cd /d "%ROOT%"

echo [1/4] 启动 MySQL / Redis...
docker compose -f "%ROOT%\docker\compose.yaml" up -d
if errorlevel 1 (
  echo Docker 启动失败，请先打开 Docker Desktop。
  pause
  exit /b 1
)

echo [2/4] 启动 Core Service...
start "LiveCompanion Core" /min "%ROOT%\scripts\run-core.cmd"
timeout /t 2 /nobreak >nul

echo [3/4] 启动 Management Service...
start "LiveCompanion Management" /min "%ROOT%\scripts\run-management.cmd"
timeout /t 2 /nobreak >nul

echo [4/4] 启动 Web Console...
where pnpm.cmd >nul 2>nul
if errorlevel 1 (
  echo 未找到 pnpm.cmd，请先安装项目要求的 Node.js / pnpm。
  pause
  exit /b 1
)
start "LiveCompanion Web" /min pnpm.cmd -C "%ROOT%" --filter web-console dev --host 127.0.0.1 --port 5173
timeout /t 2 /nobreak >nul

echo.
echo Web Console: http://127.0.0.1:5173/
echo Management:  http://127.0.0.1:8080/
echo Core:        http://127.0.0.1:8081/
echo.
start "" "http://127.0.0.1:5173/"