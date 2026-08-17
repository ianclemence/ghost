.PHONY: all build install uninstall clean help test install-service build-ghost rebuild-web

# Build variables
BINARY_NAME=ghost
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# Web console binary (setup wizard + admin dashboard, always-on on port 80)
WEB_NAME=ghost-web
WEB_DIR=cmd/$(WEB_NAME)

# Version
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell $(GO) version | awk '{print $$3}')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME) -X main.goVersion=$(GO_VERSION)"

# Go variables
GO?=go
GOFLAGS?=-v

# Installation
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1

# Appliance runtime workspace (kept outside the install tree so user data
# never mixes with the deployment or blocks git pulls in checkout layouts)
WORKSPACE_DIR?=/var/lib/ghost/workspace

# OS detection
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# Platform-specific settings
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		# Detect 32-bit userland (Raspberry Pi OS 64-bit kernel with 32-bit userspace)
		ifeq ($(shell dpkg --print-architecture 2>/dev/null),armhf)
			ARCH=arm
			export GOARCH=arm
			export GOARM=7
			export CGO_ENABLED=1
		else
			ARCH=arm64
		endif
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH=arm64
	else
		ARCH=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ARCH=$(UNAME_M)
endif

BINARY_PATH=$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)

# Default target
all: build

## generate: Run generate
generate:
	@echo "Run generate..."
	@rm -r ./$(CMD_DIR)/workspace 2>/dev/null || true
	@$(GO) generate ./...
	@echo "Run generate complete"

## build: Build the ghost binary for current platform
build: generate
	@echo "Building $(BINARY_NAME) for $(PLATFORM)/$(ARCH)..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "Build complete: $(BINARY_PATH)"
	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-all: Build ghost for all platforms
build-all: generate
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "All builds complete"

## deps: Install system dependencies (Debian/Ubuntu)
deps:
	@echo "Installing system dependencies..."
	@sudo apt-get update && sudo apt-get install -y golang git python3 python3-pip ffmpeg alsa-utils espeak fswebcam adb nmap poppler-utils pandoc chromium avahi-utils coreutils

## install: Stop service, install ghost binary, restart service
install: build
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $(INSTALL_BIN_DIR)
	@# Stop the service before replacing the binary to avoid "Text file busy" error.
	@# The binary cannot be overwritten while it is being executed by systemd.
	@sudo systemctl stop ghost 2>/dev/null || true
	@sudo systemctl stop ghost-web 2>/dev/null || true
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@cp $(BINARY_PATH) $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@chmod +x $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "Installed binary to $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@# Restart the service if it was previously enabled
	@sudo systemctl start ghost 2>/dev/null || true
	@echo "Installation complete!"

## install-service: Generate service file from template and install it
install-service:
	@echo "Installing ghost.service for user: $(USER)"
	@sed \
		-e "s|__USER__|$(USER)|g" \
		-e "s|__HOME__|$(HOME)|g" \
		ghost.service.template > ghost.service
	@sudo cp ghost.service /etc/systemd/system/ghost.service
	@sudo systemctl daemon-reload
	@sudo systemctl enable ghost
	@sudo systemctl restart ghost
	@echo "ghost.service installed for $(USER)"

## build-ghost: Build all Ghost binaries
build-ghost: build
	@echo "Building web console binary..."
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(WEB_NAME)-$(PLATFORM)-$(ARCH) ./$(WEB_DIR)
	@ln -sf $(WEB_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(WEB_NAME)
	@echo "Build complete"

## install-ghost: Build, install binaries and services, then restart Ghost
install-ghost: build-ghost
	@echo "Installing Ghost..."
	@# Stop services before replacing binaries
	@sudo systemctl stop ghost 2>/dev/null || true
	@sudo systemctl stop ghost-web 2>/dev/null || true
	@sudo mkdir -p /var/ghost/config /var/ghost/data /var/ghost/workspace
	@sudo mkdir -p $(WORKSPACE_DIR)
	@# Copy to temp then rename so a running 'ghost update' binary can be replaced (ETXTBSY)
	@sudo cp $(BINARY_PATH) /usr/local/bin/ghost.new
	@sudo mv -f /usr/local/bin/ghost.new /usr/local/bin/ghost
	@sudo cp $(BUILD_DIR)/$(WEB_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(WEB_NAME).new
	@sudo mv -f /usr/local/bin/$(WEB_NAME).new /usr/local/bin/$(WEB_NAME)
	@sudo chmod +x /usr/local/bin/ghost /usr/local/bin/$(WEB_NAME)
	@sudo chown -R $(USER):$(USER) /var/ghost
	@sudo chown -R $(USER):$(USER) /var/lib/ghost
	@# Build and deploy update tooling
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/ghost-update-$(PLATFORM)-$(ARCH) ./cmd/ghost-update
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/ghost-updater-$(PLATFORM)-$(ARCH) ./cmd/ghost-updater
	@ln -sf ghost-update-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/ghost-update
	@ln -sf ghost-updater-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/ghost-updater
	@sudo cp $(BUILD_DIR)/ghost-update-$(PLATFORM)-$(ARCH) /usr/local/bin/ghost-update.new
	@sudo mv -f /usr/local/bin/ghost-update.new /usr/local/bin/ghost-update
	@sudo cp $(BUILD_DIR)/ghost-updater-$(PLATFORM)-$(ARCH) /usr/local/bin/ghost-updater.new
	@sudo mv -f /usr/local/bin/ghost-updater.new /usr/local/bin/ghost-updater
	@sudo chmod +x /usr/local/bin/ghost-update /usr/local/bin/ghost-updater
	@# Install web console service
	@sed \
		-e "s|__GHOST_DIR__|/var/ghost|g" \
		-e "s|__BIN_DIR__|/usr/local/bin|g" \
		ghost-web.service.template > ghost-web.service
	@sudo cp ghost-web.service /etc/systemd/system/ghost-web.service
	@# Install main ghost service
	@sed \
		-e "s|__USER__|$(USER)|g" \
		-e "s|__GROUP__|$(USER)|g" \
		-e "s|__GHOST_DIR__|/var/ghost|g" \
		-e "s|__BIN_DIR__|/usr/local/bin|g" \
		ghost.service.template > ghost.service
	@sudo cp ghost.service /etc/systemd/system/ghost.service
	@# Open firewall ports
	@sudo ufw allow 80/tcp 2>/dev/null || true
	@sudo ufw allow 8766/tcp 2>/dev/null || true
	@# Enable and restart services
	@sudo systemctl daemon-reload
	@sudo systemctl enable ghost-web
	@sudo systemctl enable ghost
	@sudo systemctl restart ghost 2>/dev/null || true
	@sudo systemctl restart ghost-web 2>/dev/null || true
	@# Restore repo ownership to the developer user
	@sudo chown -R $(shell stat -c '%U' .):$(shell stat -c '%G' .) $(BUILD_DIR) $(CMD_DIR)/workspace ghost.service ghost-web.service 2>/dev/null || true
	@echo "Ghost installed"
	@if [ ! -f /var/ghost/.setup-complete ]; then echo "Run 'sudo systemctl start ghost-web' to begin setup now"; echo "Or reboot to start setup automatically"; fi

## rebuild-web: Quick rebuild and install web console only
rebuild-web:
	@echo "Rebuilding web console..."
	@sudo systemctl stop ghost-web 2>/dev/null || true
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(WEB_NAME)-$(PLATFORM)-$(ARCH) ./$(WEB_DIR)
	@sudo cp $(BUILD_DIR)/$(WEB_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(WEB_NAME)
	@sudo systemctl daemon-reload
	@sudo systemctl restart ghost-web
	@echo "Web console rebuilt and restarted"
