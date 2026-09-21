@echo off
setlocal
cd /d "%~dp0.."
powershell.exe -NoLogo -NoProfile -NoExit -ExecutionPolicy Bypass -File "%~dp0watch-room-log.ps1"