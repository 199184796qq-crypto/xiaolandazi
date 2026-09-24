@echo off
setlocal
set "ROOT=%~dp0.."
cd /d "%ROOT%"
powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\scripts\run-backend-task.ps1" -Service core
