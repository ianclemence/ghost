@echo off
setlocal EnableDelayedExpansion
title Ghost Setup (Windows)

echo ===================================================
echo   Ghost: Your Sovereign Intelligence (Windows Setup)
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

:: 1.5 Ensure .env and bridge secret
echo.
echo [1.5/3] Ensuring .env and bridge secret...
if not exist ".env" (
    if exist ".env.example" (
        copy /y ".env.example" ".env" >nul
        echo [INFO] Created .env from .env.example
    ) else (
        echo [WARNING] .env and .env.example not found.
    )
)

set "BRIDGE_SECRET="
for /f "tokens=1,* delims==" %%A in ('findstr /b /i "BRIDGE_SECRET=" ".env" 2^>nul') do (
    set "BRIDGE_SECRET=%%B"
)
if "%BRIDGE_SECRET%"=="" (
    for /f %%S in ('powershell -NoProfile -Command "$bytes=New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Fill($bytes); [Convert]::ToBase64String($bytes)"') do set "BRIDGE_SECRET=%%S"
    echo BRIDGE_SECRET=%BRIDGE_SECRET%>>".env"
    echo [OK] Generated BRIDGE_SECRET
) else (
    if /i "%BRIDGE_SECRET%"=="pick_a_strong_secret_here" (
        for /f %%S in ('powershell -NoProfile -Command "$bytes=New-Object byte[] 32; [Security.Cryptography.RandomNumberGenerator]::Fill($bytes); [Convert]::ToBase64String($bytes)"') do set "BRIDGE_SECRET=%%S"
        powershell -NoProfile -Command "(Get-Content '.env') -replace '^BRIDGE_SECRET=.*','BRIDGE_SECRET=%BRIDGE_SECRET%' | Set-Content '.env'"
        echo [OK] Updated BRIDGE_SECRET
    ) else (
        echo [OK] BRIDGE_SECRET already set
    )
)

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
