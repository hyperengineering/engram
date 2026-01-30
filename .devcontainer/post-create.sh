#!/bin/bash
set -e

echo "=== Post-create setup ==="

# Download Go modules
echo "Downloading Go modules..."
go mod download

# Create data directory
mkdir -p /workspaces/engram/data

# Install additional tools
echo "Installing development tools..."
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
curl -fsSL https://claude.ai/install.sh | bash
curl -fsSL https://opencode.ai/install | bash
curl -fsSL https://raw.githubusercontent.com/steveyegge/beads/main/scripts/install.sh | bash

echo "=== Setup complete ==="
