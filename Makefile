.PHONY: build web install clean test lint fmt help \
	build-linux-amd64 build-linux-arm64 \
	build-darwin-amd64 build-darwin-arm64 \
	build-windows-amd64 build-windows-arm64 \
	build-all install-deps-linux

# Binary name
BINARY_NAME=atr
# Version information (all overridable by CI)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE?=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)
# Build directory
BUILD_DIR=bin

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Build flags
#
# The version variables live in internal/cli, not package main. Setting
# -X main.version here would be silently ignored by the linker.
CLI_PKG=github.com/imyousuf/agentic-test-runner/internal/cli
LDFLAGS=-ldflags "-s -w \
	-X $(CLI_PKG).Version=$(VERSION) \
	-X $(CLI_PKG).Commit=$(COMMIT) \
	-X $(CLI_PKG).BuildDate=$(BUILD_DATE)"

# bin/ as an order-only prerequisite: plain mkdir works under both sh and
# cmd.exe, unlike "mkdir -p". Only "web", "build" and the targets they depend on
# are cmd.exe-safe -- the cross-compile targets below use POSIX shell (mkdir -p,
# tar, zip) and are meant for a Unix host.
$(BUILD_DIR):
	mkdir $(BUILD_DIR)

# Default target
all: build

# NPM_STAMP records the last successful install. Gating npm ci on the mere
# presence of web/node_modules let a dependency bump through silently: WEB_SRC
# lists web/package-lock.json, so bumping one does rebuild web/dist, but npm ci
# was skipped and vite then compiled against the previous install. The stamp
# lives inside node_modules so that deleting that tree invalidates it too, and
# it is written after npm ci because npm ci starts by wiping the directory.
# "echo >" rather than "touch" keeps this target usable under cmd.exe.
NPM_STAMP=web/node_modules/.atr-npm-stamp

$(NPM_STAMP): web/package.json web/package-lock.json
	cd web && npm ci --no-audit --no-fund
	@echo stamp > $(NPM_STAMP)

## web: Build the live view web application into web/dist (needs Node 22+)
##      Run this once before "make build"; web/dist is not committed.
web: $(NPM_STAMP)
	cd web && npm run build

# web/dist is gitignored build output, and every target that compiles Go needs
# it to exist for //go:embed. Listing the sources means make rebuilds it when
# they change and skips it otherwise, so a fresh clone works and an ordinary
# build does not shell out to npm.
#
# rwildcard walks the tree, because a plain $(wildcard web/src/*) stops at the
# top level: the day someone adds web/src/components/, edits there would stop
# invalidating web/dist and the embedded assets would go stale with no sign of
# it. It is built from make's own wildcard rather than from find, which is a
# different program under cmd.exe, because this list feeds a prerequisite of
# "build".
rwildcard=$(foreach d,$(wildcard $(1)*),$(call rwildcard,$(d)/,$(2)) $(filter $(subst *,%,$(2)),$(d)))

WEB_SRC=$(call rwildcard,web/src/,*) web/index.html web/package.json \
	web/package-lock.json web/vite.config.ts web/tsconfig.json

web/dist/index.html: $(WEB_SRC)
	@$(MAKE) web

## build: Build the binary (run "make web" first: web/dist is embedded)
build: web/dist/index.html | $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/atr

## install: Install the binary to GOPATH/bin
install: web/dist/index.html
	$(GOBUILD) $(LDFLAGS) -o $(shell go env GOPATH)/bin/$(BINARY_NAME) ./cmd/atr

## build-linux-amd64: Build for Linux x86_64
build-linux-amd64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/linux-amd64
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_linux_amd64.tar.gz -C $(BUILD_DIR)/linux-amd64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/linux-amd64

## build-linux-arm64: Build for Linux ARM64
build-linux-arm64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/linux-arm64
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/linux-arm64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_linux_arm64.tar.gz -C $(BUILD_DIR)/linux-arm64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/linux-arm64

## build-darwin-amd64: Build for macOS x86_64
build-darwin-amd64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/darwin-amd64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64.tar.gz -C $(BUILD_DIR)/darwin-amd64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/darwin-amd64

## build-darwin-arm64: Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/darwin-arm64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_darwin_arm64.tar.gz -C $(BUILD_DIR)/darwin-arm64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/darwin-arm64

## build-windows-amd64: Build for Windows x86_64
build-windows-amd64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/windows-amd64
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/$(BINARY_NAME).exe ./cmd/atr
	cd $(BUILD_DIR)/windows-amd64 && zip ../$(BINARY_NAME)_windows_amd64.zip $(BINARY_NAME).exe
	@rm -rf $(BUILD_DIR)/windows-amd64

## build-windows-arm64: Build for Windows ARM64
build-windows-arm64: web/dist/index.html
	@mkdir -p $(BUILD_DIR)/windows-arm64
	GOOS=windows GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/windows-arm64/$(BINARY_NAME).exe ./cmd/atr
	cd $(BUILD_DIR)/windows-arm64 && zip ../$(BINARY_NAME)_windows_arm64.zip $(BINARY_NAME).exe
	@rm -rf $(BUILD_DIR)/windows-arm64

## build-all: Build for all platforms
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64 build-windows-amd64 build-windows-arm64
	@echo "Built archives for all platforms in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/*.tar.gz $(BUILD_DIR)/*.zip 2>/dev/null || true

## clean: Clean build artifacts
clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR) web/dist

## test: Run tests
test: web/dist/index.html
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage
test-coverage: web/dist/index.html
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint: web/dist/index.html
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

## fmt: Format code
fmt:
	$(GOFMT) -s -w .

## tidy: Tidy and verify dependencies
tidy:
	$(GOMOD) tidy
	$(GOMOD) verify

## deps: Download dependencies
deps:
	$(GOMOD) download

## install-deps-linux: Install Linux system libraries required for desktop control (X11)
install-deps-linux:
	sudo apt-get update
	sudo apt-get install -y \
		libxtst-dev libxss-dev libpng-dev \
		libxkbcommon-dev libx11-dev xclip xsel

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/ /'
