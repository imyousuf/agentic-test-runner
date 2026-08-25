# Installation

This guide covers all methods for installing ATR.

## Requirements

- **Operating System**: Linux, macOS, or Windows
- **Architecture**: amd64 (x86_64) or arm64

For building from source:
- **Go 1.25** or later
- **Node 22** or later, plus npm — the `atr rdp` live view is a web application
  that is compiled into the binary. `web/dist` is build output and is not
  committed, so the Makefile builds it as part of any target that compiles Go.

## Installation Methods

### Method 1: Install Script (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.sh | sh
```

This downloads the release binary for your platform and verifies it against the
published checksums. Nothing else is needed — no Go, no Node.

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
# Apple Silicon (M1/M2/M3)
curl -L https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-darwin-arm64.tar.gz | tar xz
sudo mv atr /usr/local/bin/
```

> **Intel Macs:** No pre-built binary is shipped. Build from source with `make build`.

#### Windows

1. Download `atr-windows-amd64.zip` from [Releases](https://github.com/imyousuf/agentic-test-runner/releases)
2. Extract `atr.exe`
3. Add the directory containing `atr.exe` to your PATH

> **Windows on ARM:** No pre-built binary is shipped — GitHub's `windows-11-arm` runners do not yet ship a CGo toolchain that can compile robotgo. Build from source with `make build`.

PowerShell:
```powershell
# Download and extract
Invoke-WebRequest -Uri "https://github.com/imyousuf/agentic-test-runner/releases/download/dev/atr-windows-amd64.zip" -OutFile "atr.zip"
Expand-Archive -Path "atr.zip" -DestinationPath "C:\Program Files\atr"

# Add to PATH (run as Administrator)
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\Program Files\atr", "Machine")
```

### Method 3: Build from Source

Building needs **Go 1.25+ and Node 22+**. The `atr rdp` live view is a web
application that is embedded in the binary, and `web/dist` is build output
rather than a checked-in artifact, so it is built first:

```bash
git clone https://github.com/imyousuf/agentic-test-runner.git
cd agentic-test-runner
make install    # builds web/dist if needed, then installs to GOPATH/bin
```

`make build` puts the binary in `bin/atr` instead of installing it.

`build`, `install`, `test`, `lint` and the cross-compile targets all depend on
`web/dist`, and the Makefile rebuilds it whenever anything under `web/src` (or
the web config) has changed. `make web` forces a rebuild.

Building the Go code directly with `go build ./cmd/atr` skips that step, so on a
fresh checkout it fails with `pattern all:dist: no matching files found`. That
is the embedded web application missing, not a broken checkout — run `make web`
first, or go through the Makefile.

> **Note on `go install`:** `go install github.com/imyousuf/agentic-test-runner/cmd/atr@latest`
> no longer works on its own, because the module does not carry the built web
> assets. Use the install script or build from source.

#### Without Node

If you do not want Node, the `noweb` build tag compiles everything except the
live view's web application, which is then served as a short placeholder page.
Every other command, including `atr browser` and `atr computer`, is unaffected:

```bash
go build -tags noweb -o atr ./cmd/atr
```

### Building for All Platforms

The Makefile supports cross-compilation:

These targets compile the Go binary only. Run `make web` first.

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

# If installed with "make install", add GOPATH/bin to PATH
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

### Built-in updater
```bash
atr update
```

### Install script
```bash
curl -fsSL https://raw.githubusercontent.com/imyousuf/agentic-test-runner/main/install.sh | sh
```

### Binary
Download the latest release and replace the existing binary.

### From Source
```bash
git pull
make web
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
