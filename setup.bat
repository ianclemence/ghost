@echo off
setlocal EnableDelayedExpansion
title Ghost Setup (Windows)

echo ===================================================
echo   Ghost: Your Sovereign AI Presence (Windows Setup)
echo ===================================================
echo.

:: 1. Check for Go
echo [1/3] Checking Go environment...
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed or not in PATH.
    echo Please install Go from https://go.dev/dl/
    pause
    exit /b 1
)
echo [OK] Go found.

:: 2. Install Skills (PowerShell Wrapper)
echo.
echo [2/3] Installing Ghost Skills (Camera, System, Calendar, Local LLM)...
echo This may ask for Administrator permissions to install tools via Winget.
powershell -ExecutionPolicy Bypass -File "%~dp0setup_ghost_skills.ps1"
if %errorlevel% neq 0 (
    echo [WARNING] Skill installation had issues. Some features might not work.
) else (
    echo [OK] Skills installed.
)

:: 3. Build Ghost
echo.
echo [3/3] Building Ghost binary...
go build -o ghost.exe ./cmd/ghost
if %errorlevel% neq 0 (
    echo [ERROR] Build failed. Check the error messages above.
    pause
    exit /b 1
)
echo [OK] Build successful: ghost.exe

echo.
echo ===================================================
echo   Setup Complete!
echo ===================================================
echo.
set /p RUN_NOW="Do you want to start Ghost now? (Y/N): "
if /i "%RUN_NOW%"=="Y" (
    cls
    .\ghost.exe gateway --debug
)

pause
