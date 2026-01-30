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

# Install GitHub CLI
echo "Installing GitHub CLI..."
(type -p wget >/dev/null || (sudo apt update && sudo apt-get install wget -y)) \
  && sudo mkdir -p -m 755 /etc/apt/keyrings \
  && out=$(mktemp) \
  && wget -nv -O$out https://cli.github.com/packages/githubcli-archive-keyring.gpg \
  && cat $out | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null \
  && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
  && sudo apt update \
  && sudo apt install gh -y

# Install Engram from release
echo "Installing Engram..."
ENGRAM_VERSION="1.0.0"
curl -fsSL "https://github.com/hyperengineering/engram/releases/download/v${ENGRAM_VERSION}/engram_linux_amd64.tar.gz" | tar -xzf - -C /tmp engram
sudo mv /tmp/engram /usr/bin/engram
sudo chmod +x /usr/bin/engram

# Create engram user and directories (mirrors deb/rpm package setup)
sudo groupadd --system engram 2>/dev/null || true
sudo useradd --system --gid engram --home-dir /var/lib/engram --shell /usr/sbin/nologin engram 2>/dev/null || true
sudo mkdir -p /etc/engram /var/lib/engram
sudo cp /workspaces/engram/packaging/engram.yaml /etc/engram/
sudo cp /workspaces/engram/packaging/environment /etc/engram/
sudo chown -R engram:engram /var/lib/engram
sudo chmod 750 /var/lib/engram
sudo chmod 640 /etc/engram/environment
sudo chown root:engram /etc/engram/environment

echo "Engram $(engram version) installed. Configure API keys in /etc/engram/environment"

echo "=== Setup complete ==="
