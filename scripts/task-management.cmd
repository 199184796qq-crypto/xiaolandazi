@echo off
setlocal
set "ROOT=%~dp0.."
cd /d "%ROOT%"
if not exist "%ROOT%\data\logs" mkdir "%ROOT%\data\logs"
if not exist "%ROOT%\data\avatars" mkdir "%ROOT%\data\avatars"
set "MGMT_AVATAR_DIR=%ROOT%\data\avatars"
"%ROOT%\management-service\bin\management-service.exe" >> "%ROOT%\data\logs\management-service.task.log" 2>&1
