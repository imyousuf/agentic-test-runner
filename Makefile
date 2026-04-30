.PHONY: build install clean test lint fmt help \
	build-linux-amd64 build-linux-arm64 \
	build-darwin-amd64 build-darwin-arm64 \
	build-windows-amd64 build-windows-arm64 \
	build-all install-deps-linux

# Binary name
BINARY_NAME=atr
# Version (can be overridden)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
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
LDFLAGS=-ldflags "-s -w"

# Default target
all: build

## build: Build the binary
build:
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/atr

## install: Install the binary to GOPATH/bin
install:
	$(GOBUILD) $(LDFLAGS) -o $(GOPATH)/bin/$(BINARY_NAME) ./cmd/atr

## build-linux-amd64: Build for Linux x86_64
build-linux-amd64:
	@mkdir -p $(BUILD_DIR)/linux-amd64
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/linux-amd64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_linux_amd64.tar.gz -C $(BUILD_DIR)/linux-amd64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/linux-amd64

## build-linux-arm64: Build for Linux ARM64
build-linux-arm64:
	@mkdir -p $(BUILD_DIR)/linux-arm64
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/linux-arm64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_linux_arm64.tar.gz -C $(BUILD_DIR)/linux-arm64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/linux-arm64

## build-darwin-amd64: Build for macOS x86_64
build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)/darwin-amd64
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/darwin-amd64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_darwin_amd64.tar.gz -C $(BUILD_DIR)/darwin-amd64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/darwin-amd64

## build-darwin-arm64: Build for macOS ARM64 (Apple Silicon)
build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)/darwin-arm64
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/darwin-arm64/$(BINARY_NAME) ./cmd/atr
	tar -czf $(BUILD_DIR)/$(BINARY_NAME)_darwin_arm64.tar.gz -C $(BUILD_DIR)/darwin-arm64 $(BINARY_NAME)
	@rm -rf $(BUILD_DIR)/darwin-arm64

## build-windows-amd64: Build for Windows x86_64
build-windows-amd64:
	@mkdir -p $(BUILD_DIR)/windows-amd64
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/windows-amd64/$(BINARY_NAME).exe ./cmd/atr
	cd $(BUILD_DIR)/windows-amd64 && zip ../$(BINARY_NAME)_windows_amd64.zip $(BINARY_NAME).exe
	@rm -rf $(BUILD_DIR)/windows-amd64

## build-windows-arm64: Build for Windows ARM64
build-windows-arm64:
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
	rm -rf $(BUILD_DIR)

## test: Run tests
test:
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint:
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
