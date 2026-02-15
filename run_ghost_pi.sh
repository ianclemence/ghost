#!/bin/bash
set -e

# Build if ghost binary does not exist
if [ ! -f "./ghost" ]; then
    echo "Building Ghost AI for Raspberry Pi..."
    
    # Check if we are in the right directory
    if [ ! -d "picoclaw" ]; then
        echo "Error: picoclaw directory not found. Please run this script from the project root."
        exit 1
    fi

    cd picoclaw
    
    # Ensure workspace directory exists for embedding
    # The main.go expects 'workspace' directory in the same folder during build
    echo "Preparing workspace for embedding..."
    if [ -d "workspace" ]; then
        # Remove existing destination if it exists to ensure fresh copy
        rm -rf cmd/picoclaw/workspace
        cp -r workspace cmd/picoclaw/
    else
        echo "Error: picoclaw/workspace not found!"
        exit 1
    fi
    
    echo "Tidying dependencies..."
    go mod tidy
    
    echo "Compiling..."
    go build -o ../ghost cmd/picoclaw/main.go
    
    # Clean up embedded workspace files to keep source tree clean
    rm -rf cmd/picoclaw/workspace
    
    cd ..
    echo "Build complete."
fi

# Set environment variable for config directory
# Using absolute path for safety
export PICOCLAW_CONFIG_DIR=$(pwd)/config

echo "Starting Ghost Pi (Raspberry Pi)..."
echo "Config Dir: $PICOCLAW_CONFIG_DIR"

# Run in agent mode
./ghost agent
