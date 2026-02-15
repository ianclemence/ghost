@echo off
set PICOCLAW_CONFIG_DIR=%~dp0config
echo Starting Ghost Pi (Native Windows)...
echo Config Dir: %PICOCLAW_CONFIG_DIR%
"%~dp0ghost.exe" agent
pause