@echo off
setlocal
set "ROOT=%~dp0.."
set "MGMT_AVATAR_DIR=%ROOT%\data\avatars"
if not exist "%MGMT_AVATAR_DIR%" mkdir "%MGMT_AVATAR_DIR%"
"%ROOT%\management-service\bin\management-service.exe"