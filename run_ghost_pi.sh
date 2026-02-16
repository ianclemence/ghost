#!/bin/bash
set -e

# Build if ghost binary does not exist
if [ ! -f "./ghost" ]; then
    echo "Building Ghost AI for Raspberry Pi..."
    
    # Ensure workspace directory exists for embedding
    echo "Preparing workspace for embedding..."
    if [ -d "workspace" ]; then
        # Remove existing destination if it exists to ensure fresh copy
        rm -rf cmd/ghost/workspace
        cp -r workspace cmd/ghost/
    else
        echo "Error: workspace not found!"
        exit 1
    fi
    
    echo "Tidying dependencies..."
    go mod tidy
    
    echo "Compiling..."
    go build -o ghost cmd/ghost/main.go
    
    # Clean up embedded workspace files to keep source tree clean
    rm -rf cmd/ghost/workspace
    
    echo "Build complete."
fi

# Set environment variable for config directory
# Using absolute path for safety
export GHOST_CONFIG_DIR=$(pwd)/config

echo "Starting Ghost Pi (Raspberry Pi)..."
echo "Config Dir: $GHOST_CONFIG_DIR"

# Run in agent mode
./ghost agent
