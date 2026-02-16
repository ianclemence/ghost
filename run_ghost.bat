@echo off
set GHOST_CONFIG_DIR=%~dp0config
echo Starting Ghost Pi (Native Windows)...
echo Config Dir: %GHOST_CONFIG_DIR%

if not exist "%~dp0ghost.exe" (
    echo Ghost binary not found. Attempting to build...
    echo Tidying dependencies...
    go mod tidy
    echo Compiling...
    go build -o ghost.exe ./cmd/ghost
    if errorlevel 1 (
        echo Build failed. Please install Go and try again.
        pause
        exit /b 1
    )
    echo Build complete.
)

"%~dp0ghost.exe" agent
pause