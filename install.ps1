# ATR (Agentic Test Runner) installer for Windows
# Usage: irm https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.ps1 | iex
#
# Environment variables:
#   ATR_INSTALL_DIR  - Installation directory (default: %LOCALAPPDATA%\atr)
#   ATR_VERSION      - Version tag to install (default: latest release, or "dev" for development)

$ErrorActionPreference = "Stop"

$Repo = "imyousuf/agentic-test-runner"
$BinaryName = "atr"

# Determine version
$Version = $env:ATR_VERSION
if (-not $Version) {
    try {
        $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -ErrorAction SilentlyContinue
        $Version = $Release.tag_name
    } catch {
        $Version = "dev"
    }
    if (-not $Version) {
        $Version = "dev"
    }
}

# Detect architecture
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default {
        Write-Error "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE"
        exit 1
    }
}

# Determine install directory
$InstallDir = $env:ATR_INSTALL_DIR
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "atr"
}
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$AssetName = "$BinaryName-windows-$Arch.zip"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Version/$AssetName"
$ChecksumsUrl = "https://github.com/$Repo/releases/download/$Version/checksums.txt"

Write-Host "Installing ATR $Version (windows/$Arch)..."
Write-Host "  Download: $DownloadUrl"
Write-Host "  Install:  $InstallDir\$BinaryName.exe"

# Create temp directory
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "atr-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    # Download binary and checksums
    Write-Host "Downloading..."
    $AssetPath = Join-Path $TmpDir $AssetName
    $ChecksumsPath = Join-Path $TmpDir "checksums.txt"
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $AssetPath -UseBasicParsing
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $ChecksumsPath -UseBasicParsing

    # Verify checksum
    Write-Host "Verifying checksum..."
    $ChecksumsContent = Get-Content $ChecksumsPath
    $ExpectedLine = $ChecksumsContent | Where-Object { $_ -match $AssetName }
    if ($ExpectedLine) {
        $Expected = ($ExpectedLine -split '\s+')[0]
        $Actual = (Get-FileHash -Path $AssetPath -Algorithm SHA256).Hash.ToLower()
        if ($Expected -ne $Actual) {
            Write-Error "Checksum mismatch!`n  Expected: $Expected`n  Actual:   $Actual"
            exit 1
        }
        Write-Host "  Checksum OK"
    } else {
        Write-Host "  Warning: checksum not found for $AssetName, skipping verification"
    }

    # Extract and install
    Write-Host "Installing..."
    Expand-Archive -Path $AssetPath -DestinationPath $TmpDir -Force
    $ExePath = Join-Path $TmpDir "$BinaryName.exe"
    Copy-Item -Path $ExePath -Destination (Join-Path $InstallDir "$BinaryName.exe") -Force

    # Add to PATH if not already there
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$InstallDir*") {
        Write-Host ""
        Write-Host "Adding $InstallDir to user PATH..."
        [Environment]::SetEnvironmentVariable("Path", "$InstallDir;$UserPath", "User")
        $env:Path = "$InstallDir;$env:Path"
        Write-Host "  PATH updated (restart your terminal for changes to take effect)"
    }

    # Verify installation
    Write-Host ""
    Write-Host "ATR installed successfully!"
    & (Join-Path $InstallDir "$BinaryName.exe") version
} finally {
    # Cleanup
    Remove-Item -Path $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
