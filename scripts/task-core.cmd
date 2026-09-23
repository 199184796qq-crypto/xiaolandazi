@echo off
setlocal
set "ROOT=%~dp0.."
cd /d "%ROOT%"
if not exist "%ROOT%\data\logs" mkdir "%ROOT%\data\logs"
"%ROOT%\core-service\bin\core-service.exe" >> "%ROOT%\data\logs\core-service.task.log" 2>&1
