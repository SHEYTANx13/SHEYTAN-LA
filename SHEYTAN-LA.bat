@echo off
REM SHEYTAN-LA v1.3.0 (SHEYTAN Local Agent) launcher
REM
REM Double-click to launch the native desktop GUI.
REM (c) 2024-2026 Parsaetak. All rights reserved.
REM SHEYTAN is a trademark of Parsaetak (https://github.com/Parsaetak).
REM Everything (models, sessions, logs, charts) lives in this folder - portable.
REM
REM v1.2.0+: the executable is SHEYTAN-LA.exe (unified product identity:
REM AppUserModelID Parsaetak.SHEYTAN-LA, ProductName SHEYTAN-LA). The
REM launcher falls back to the legacy sheytan-local-agent.exe name so old
REM portable folders keep working after an in-place update.

setlocal

set "SCRIPT_DIR=%~dp0"
cd /d "%SCRIPT_DIR%"

if exist "%SCRIPT_DIR%SHEYTAN-LA.exe" (
    start "" "%SCRIPT_DIR%SHEYTAN-LA.exe" %*
    goto :done
)

if exist "%SCRIPT_DIR%sheytan-local-agent.exe" (
    start "" "%SCRIPT_DIR%sheytan-local-agent.exe" %*
    goto :done
)

where SHEYTAN-LA.exe >nul 2>&1
if %errorlevel%==0 (
    start "" SHEYTAN-LA.exe %*
    goto :done
)

echo SHEYTAN-LA.exe was not found next to this launcher.
echo Extract the full zip and run it from the SHEYTAN-LA folder.
pause

:done
endlocal
