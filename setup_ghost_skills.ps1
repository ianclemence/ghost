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

Write-Host "Done. Please restart your terminal to update PATH." -ForegroundColor Cyan
