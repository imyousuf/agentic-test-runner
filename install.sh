#!/bin/sh
# ATR (Agentic Test Runner) installer for macOS and Linux
# Usage: curl -fsSL https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.sh | sh
#
# Environment variables:
#   ATR_INSTALL_DIR  - Installation directory (default: /usr/local/bin, falls back to ~/.local/bin)
#   ATR_VERSION      - Version tag to install (default: latest release, or "dev" for development)

set -e

REPO="imyousuf/agentic-test-runner"
BINARY_NAME="atr"

# Determine version
VERSION="${ATR_VERSION:-}"
if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    if [ -z "$VERSION" ]; then
        # No latest release, fall back to dev
        VERSION="dev"
    fi
fi

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "Error: unsupported operating system: $OS"
        echo "This installer supports Linux and macOS. For Windows, use install.ps1."
        exit 1
        ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Determine install directory
INSTALL_DIR="${ATR_INSTALL_DIR:-}"
if [ -z "$INSTALL_DIR" ]; then
    if [ -w /usr/local/bin ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="$HOME/.local/bin"
    fi
fi
mkdir -p "$INSTALL_DIR"

ASSET_NAME="${BINARY_NAME}-${OS}-${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET_NAME}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

echo "Installing ATR ${VERSION} (${OS}/${ARCH})..."
echo "  Download: ${DOWNLOAD_URL}"
echo "  Install:  ${INSTALL_DIR}/${BINARY_NAME}"

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download binary and checksums
echo "Downloading..."
curl -fsSL -o "${TMP_DIR}/${ASSET_NAME}" "$DOWNLOAD_URL"
curl -fsSL -o "${TMP_DIR}/checksums.txt" "$CHECKSUMS_URL"

# Verify checksum
echo "Verifying checksum..."
EXPECTED=$(grep "${ASSET_NAME}" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
    echo "Warning: checksum not found for ${ASSET_NAME}, skipping verification"
else
    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "${TMP_DIR}/${ASSET_NAME}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL=$(shasum -a 256 "${TMP_DIR}/${ASSET_NAME}" | awk '{print $1}')
    else
        echo "Warning: sha256sum/shasum not found, skipping verification"
        ACTUAL="$EXPECTED"
    fi
    if [ "$EXPECTED" != "$ACTUAL" ]; then
        echo "Error: checksum mismatch!"
        echo "  Expected: $EXPECTED"
        echo "  Actual:   $ACTUAL"
        exit 1
    fi
    echo "  Checksum OK"
fi

# Extract and install
echo "Installing..."
tar -xzf "${TMP_DIR}/${ASSET_NAME}" -C "${TMP_DIR}"
chmod +x "${TMP_DIR}/${BINARY_NAME}"

if [ -w "$INSTALL_DIR" ]; then
    mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
else
    echo "  Using sudo to install to ${INSTALL_DIR}"
    sudo mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
fi

# Verify installation
if command -v "$BINARY_NAME" >/dev/null 2>&1; then
    echo ""
    echo "ATR installed successfully!"
    $BINARY_NAME version
else
    echo ""
    echo "ATR installed to ${INSTALL_DIR}/${BINARY_NAME}"
    echo ""
    # Check if install dir is in PATH
    case ":$PATH:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            echo "Note: ${INSTALL_DIR} is not in your PATH."
            echo "Add it with:"
            echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
            echo ""
            echo "Or add to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
            echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.bashrc"
            ;;
    esac
fi
