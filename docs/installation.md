# Installation

This guide covers all methods for installing ATR.

## Requirements

- **Operating System**: Linux, macOS, or Windows
- **Architecture**: amd64 (x86_64) or arm64

For building from source:
- **Go 1.23** or later

## Installation Methods

### Method 1: Go Install (Recommended)

If you have Go installed:

```bash
go install github.com/imyousuf/agentic-test-runner/cmd/atr@latest
```

This installs the `atr` binary to your `$GOPATH/bin` (usually `~/go/bin`).

Make sure `$GOPATH/bin` is in your PATH:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Method 2: Pre-built Binaries

Download from [GitHub Releases](https://github.com/imyousuf/agentic-test-runner/releases).

#### Linux

```bash
# AMD64 (Intel/AMD)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-linux-amd64.tar.gz | tar xz
sudo mv atr /usr/local/bin/

# ARM64 (Apple Silicon, Raspberry Pi 4+)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-linux-arm64.tar.gz | tar xz
sudo mv atr /usr/local/bin/
```

#### macOS

```bash
# Intel Mac
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-darwin-amd64.tar.gz | tar xz
sudo mv atr /usr/local/bin/

# Apple Silicon (M1/M2/M3)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-darwin-arm64.tar.gz | tar xz
sudo mv atr /usr/local/bin/
```

#### Windows

1. Download `atr-windows-amd64.zip` or `atr-windows-arm64.zip` from [Releases](https://github.com/imyousuf/agentic-test-runner/releases)
2. Extract `atr.exe`
3. Add the directory containing `atr.exe` to your PATH

PowerShell:
```powershell
# Download and extract
Invoke-WebRequest -Uri "https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-windows-amd64.zip" -OutFile "atr.zip"
Expand-Archive -Path "atr.zip" -DestinationPath "C:\Program Files\atr"

# Add to PATH (run as Administrator)
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\Program Files\atr", "Machine")
```

### Method 3: Build from Source

Clone and build:

```bash
git clone https://github.com/imyousuf/agentic-test-runner.git
cd agentic-test-runner
go build -o atr ./cmd/atr
sudo mv atr /usr/local/bin/
```

Or use the Makefile:

```bash
git clone https://github.com/imyousuf/agentic-test-runner.git
cd agentic-test-runner
make install
```

### Building for All Platforms

The Makefile supports cross-compilation:

```bash
# Build for all platforms
make build-all

# Build for specific platform
make build-linux-amd64
make build-darwin-arm64
make build-windows-amd64
```

This creates archives in the `dist/` directory.

## Verify Installation

```bash
atr version
```

Expected output:
```
atr version dev-abc1234
```

## Shell Completion (Optional)

Enable tab completion for bash or zsh:

```bash
atr install-completion
```

This auto-detects your shell and installs completion scripts. Follow the printed instructions to enable completions.

## Verify LLM Connectivity

Test that ATR can connect to your LLM provider:

```bash
# Set your API key
export GEMINI_API_KEY="your-api-key"

# Run a simple test
atr run --cmd "echo hello"
```

If configured correctly, you'll see:
```
Executing: echo hello
Directory: /current/directory

hello

✓ Command completed successfully (exit code: 0, duration: 5ms)
```

## Browser for Behavior Testing

For behavior testing, ATR uses [rod](https://go-rod.github.io/) which automatically downloads Chromium on first use:

```bash
# First behavior test will download Chromium (~150MB)
atr run --behavior tests/example.test.txt
```

The browser is cached in `~/.cache/rod/browser/`.

To use a different browser:
```yaml
# ~/.atr/config.yaml
behavior:
  browser:
    executable: /path/to/chrome
```

## Troubleshooting

### "command not found: atr"

Ensure the binary is in your PATH:
```bash
# Check if atr is found
which atr

# If using go install, add GOPATH/bin to PATH
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Permission denied on Linux/macOS

Make the binary executable:
```bash
chmod +x /usr/local/bin/atr
```

### Chromium download fails

If behind a proxy, set environment variables:
```bash
export HTTP_PROXY="http://proxy:8080"
export HTTPS_PROXY="http://proxy:8080"
```

Or manually specify a browser path in config.

## Updating

### Go Install
```bash
go install github.com/imyousuf/agentic-test-runner/cmd/atr@latest
```

### Binary
Download the latest release and replace the existing binary.

### From Source
```bash
git pull
make install
```

## Uninstalling

```bash
# Remove binary
sudo rm /usr/local/bin/atr

# Remove configuration (optional)
rm -rf ~/.atr

# Remove browser cache (optional)
rm -rf ~/.cache/rod
```
