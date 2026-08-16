.PHONY: all build install uninstall clean help test install-service build-appliance

# Build variables
BINARY_NAME=ghost
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# Appliance binaries
FIRSTBOOT_NAME=ghost-firstboot
UPDATER_NAME=ghost-updater
UPDATE_NAME=ghost-update
SERVICE_NAME=ghost-service
FIRSTBOOT_DIR=cmd/$(FIRSTBOOT_NAME)
UPDATER_DIR=cmd/$(UPDATER_NAME)
UPDATE_DIR=cmd/$(UPDATE_NAME)
SERVICE_DIR=cmd/$(SERVICE_NAME)

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

# Workspace and Skills
GHOST_HOME?=$(HOME)/.ghost
WORKSPACE_DIR?=$(GHOST_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills

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
	@echo "✅ ghost.service installed for $(USER)"

## build-appliance: Build all appliance binaries (firstboot, updater, update)
build-appliance: build
	@echo "Building appliance binaries..."
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(FIRSTBOOT_NAME)-$(PLATFORM)-$(ARCH) ./$(FIRSTBOOT_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(UPDATER_NAME)-$(PLATFORM)-$(ARCH) ./$(UPDATER_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(UPDATE_NAME)-$(PLATFORM)-$(ARCH) ./$(UPDATE_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(SERVICE_NAME)-$(PLATFORM)-$(ARCH) ./$(SERVICE_DIR)
	@ln -sf $(FIRSTBOOT_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(FIRSTBOOT_NAME)
	@ln -sf $(UPDATER_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(UPDATER_NAME)
	@ln -sf $(UPDATE_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(UPDATE_NAME)
	@ln -sf $(SERVICE_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(SERVICE_NAME)
	@echo "Appliance binaries built"

## rebuild-firstboot: Quick rebuild and install firstboot only
rebuild-firstboot:
	@echo "Rebuilding firstboot..."
	@sudo systemctl stop ghost-firstboot 2>/dev/null || true
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(FIRSTBOOT_NAME)-$(PLATFORM)-$(ARCH) ./$(FIRSTBOOT_DIR)
	@sudo cp $(BUILD_DIR)/$(FIRSTBOOT_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(FIRSTBOOT_NAME)
	@sudo systemctl daemon-reload
	@sudo systemctl restart ghost-firstboot
	@echo "Firstboot rebuilt and restarted"

## install-appliance: Install appliance binaries and services
install-appliance: build-appliance
	@echo "Installing Ghost Appliance..."
	@# Stop services before replacing binaries
	@sudo systemctl stop ghost 2>/dev/null || true
	@sudo systemctl stop ghost-firstboot 2>/dev/null || true
	@sudo mkdir -p /var/ghost/config /var/ghost/data /var/ghost/workspace
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/ghost
	@sudo cp $(BUILD_DIR)/$(FIRSTBOOT_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(FIRSTBOOT_NAME)
	@sudo cp $(BUILD_DIR)/$(UPDATER_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(UPDATER_NAME)
	@sudo cp $(BUILD_DIR)/$(UPDATE_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(UPDATE_NAME)
	@sudo cp $(BUILD_DIR)/$(SERVICE_NAME)-$(PLATFORM)-$(ARCH) /usr/local/bin/$(SERVICE_NAME)
	@sudo chmod +x /usr/local/bin/ghost /usr/local/bin/$(FIRSTBOOT_NAME) /usr/local/bin/$(UPDATER_NAME) /usr/local/bin/$(UPDATE_NAME) /usr/local/bin/$(SERVICE_NAME)
	@sudo chown -R $(USER):$(USER) /var/ghost
	@# Install firstboot service
	@sed \
		-e "s|__GHOST_DIR__|/var/ghost|g" \
		-e "s|__BIN_DIR__|/usr/local/bin|g" \
		ghost-firstboot.service.template > ghost-firstboot.service
	@sudo cp ghost-firstboot.service /etc/systemd/system/ghost-firstboot.service
	@# Install main ghost service
	@sed \
		-e "s|__USER__|$(USER)|g" \
		-e "s|__GROUP__|$(USER)|g" \
		-e "s|__GHOST_DIR__|/var/ghost|g" \
		-e "s|__BIN_DIR__|/usr/local/bin|g" \
		ghost-appliance.service.template > ghost-appliance.service
	@sudo cp ghost-appliance.service /etc/systemd/system/ghost.service
	@# Open firewall ports
	@sudo ufw allow 80/tcp 2>/dev/null || true
	@sudo ufw allow 8766/tcp 2>/dev/null || true
	@# Enable and start firstboot service (runs on next boot)
	@sudo systemctl daemon-reload
	@sudo systemctl enable ghost-firstboot
	@sudo systemctl enable ghost
	@echo "✅ Ghost Appliance installed"
	@echo "Run 'sudo systemctl start ghost-firstboot' to begin setup now"
	@echo "Or reboot to start setup automatically"