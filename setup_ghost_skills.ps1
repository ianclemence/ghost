Write-Host "Checking Ghost Skills Dependencies..." -ForegroundColor Cyan

# Function to check command availability
function Check-Command {
    param($Name)
    if (Get-Command $Name -ErrorAction SilentlyContinue) {
        Write-Host "✅ $Name found" -ForegroundColor Green
        return $true
    }
    Write-Host "❌ $Name not found" -ForegroundColor Red
    return $false
}

# 1. Python & gcalcli
if (Check-Command "python") {
    if (-not (Check-Command "gcalcli")) {
        Write-Host "Installing gcalcli..." -ForegroundColor Yellow
        pip install --user gcalcli
    }
} else {
    Write-Host "Python is missing. Please install Python to use Calendar skill." -ForegroundColor Red
}

# 2. Winget for other tools
if (Check-Command "winget") {
    # nircmd
    if (-not (Check-Command "nircmd")) {
        Write-Host "Installing nircmd (for System Control)..." -ForegroundColor Yellow
        winget install nirsoft.nircmd --accept-source-agreements --accept-package-agreements
    }
    
    # ffmpeg
    if (-not (Check-Command "ffmpeg")) {
        Write-Host "Installing ffmpeg (for Camera)..." -ForegroundColor Yellow
        winget install Gyan.FFmpeg --accept-source-agreements --accept-package-agreements
    }

    # nmap (Network Scanner)
    if (-not (Check-Command "nmap")) {
        Write-Host "Installing Nmap (for Network Scanning)..." -ForegroundColor Yellow
        winget install Insecure.Nmap --accept-source-agreements --accept-package-agreements
    }

    # ADB (Android Debug Bridge)
    if (-not (Check-Command "adb")) {
        Write-Host "Installing ADB (for Mobile Control)..." -ForegroundColor Yellow
        winget install Google.PlatformTools --accept-source-agreements --accept-package-agreements
    }
} else {
    Write-Host "Winget not found. Please manually install:" -ForegroundColor Yellow
    Write-Host "- nircmd: https://www.nirsoft.net/utils/nircmd.html"
    Write-Host "- ffmpeg: https://ffmpeg.org/download.html"
}

# 3. PicoLM (Local LLM) Setup
Write-Host "Checking PicoLM (Local LLM)..." -ForegroundColor Cyan
$PicoLMDir = "$env:USERPROFILE\.picolm"
$PicoLMBinDir = "$PicoLMDir\bin"
$PicoLMModelDir = "$PicoLMDir\models"
$PicoLMBinary = "$PicoLMBinDir\picolm.exe"
$PicoLMModel = "$PicoLMModelDir\tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"

# Create directories
if (-not (Test-Path $PicoLMBinDir)) { New-Item -ItemType Directory -Force -Path $PicoLMBinDir | Out-Null }
if (-not (Test-Path $PicoLMModelDir)) { New-Item -ItemType Directory -Force -Path $PicoLMModelDir | Out-Null }

# Check/Download Model
if (-not (Test-Path $PicoLMModel)) {
    Write-Host "Downloading TinyLlama model (638MB)..." -ForegroundColor Yellow
    try {
        Invoke-WebRequest -Uri "https://huggingface.co/TinyLlama/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf" -OutFile $PicoLMModel
        Write-Host "✅ Model downloaded to $PicoLMModel" -ForegroundColor Green
    } catch {
        Write-Host "❌ Failed to download model. Please download manually." -ForegroundColor Red
        Write-Host "URL: https://huggingface.co/TinyLlama/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"
    }
} else {
    Write-Host "✅ Model found at $PicoLMModel" -ForegroundColor Green
}

# Check Binary
if (-not (Test-Path $PicoLMBinary)) {
    Write-Host "⚠️ PicoLM binary not found at $PicoLMBinary" -ForegroundColor Yellow
    Write-Host "To use Local LLM on Windows:"
    Write-Host "1. You need to compile PicoLM from source (https://github.com/picolm/picolm)"
    Write-Host "2. Or download a pre-built Windows binary if available."
    Write-Host "3. Place 'picolm.exe' in: $PicoLMBinDir"
    Write-Host "Skipping binary installation (requires manual build on Windows)."
} else {
    Write-Host "✅ PicoLM binary found." -ForegroundColor Green
}


Write-Host "Done. Please restart your terminal to update PATH." -ForegroundColor Cyan
