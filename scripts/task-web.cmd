@echo off
setlocal
set "ROOT=%~dp0.."
cd /d "%ROOT%"
if not exist "%ROOT%\data\logs" mkdir "%ROOT%\data\logs"
pnpm.cmd -C "%ROOT%" --filter web-console dev --host 127.0.0.1 --port 5173 >> "%ROOT%\data\logs\web-console.task.log" 2>&1
