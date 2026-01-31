# Engram Systemd Setup Guide

This guide covers installing and running Engram as a Linux systemd service.

## Prerequisites

- Linux system with systemd
- OpenAI API key for embedding generation
- Root or sudo access

## Installation Methods

### Method 1: Package Installation (Recommended)

Download the appropriate package from the [releases page](https://github.com/hyperengineering/engram/releases):

**Debian/Ubuntu:**
```bash
# Download the .deb package
curl -LO https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_1.0.0_linux_amd64.deb

# Install
sudo dpkg -i engram_1.0.0_linux_amd64.deb
```

**RHEL/Fedora/CentOS:**
```bash
# Download the .rpm package
curl -LO https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram-1.0.0.x86_64.rpm

# Install
sudo rpm -i engram-1.0.0.x86_64.rpm
```

The package automatically:
- Creates the `engram` user and group
- Installs configuration files to `/etc/engram/`
- Installs the systemd unit file
- Creates the data directory at `/var/lib/engram/`

### Method 2: Manual Installation

Download the binary and set up manually:

```bash
# Download and extract
curl -LO https://github.com/hyperengineering/engram/releases/download/v1.0.0/engram_linux_amd64.tar.gz
tar -xzf engram_linux_amd64.tar.gz

# Install binary
sudo mv engram /usr/bin/engram
sudo chmod +x /usr/bin/engram

# Create user and group
sudo groupadd --system engram
sudo useradd --system --gid engram --home-dir /var/lib/engram --shell /usr/sbin/nologin engram

# Create directories
sudo mkdir -p /etc/engram /var/lib/engram
sudo chown engram:engram /var/lib/engram
sudo chmod 750 /var/lib/engram
```

Create the configuration files (see [Configuration](#configuration) section below), then install the systemd unit:

```bash
# Download service file
sudo curl -o /usr/lib/systemd/system/engram.service \
  https://raw.githubusercontent.com/hyperengineering/engram/main/packaging/engram.service

# Reload systemd
sudo systemctl daemon-reload
```

## Configuration

### API Keys

Edit `/etc/engram/environment` to set your API keys:

```bash
sudo nano /etc/engram/environment
```

Set the following values:

```bash
# Required: Generate a secure key for client authentication
# Run: openssl rand -hex 32
ENGRAM_API_KEY=your-generated-api-key

# Required: OpenAI API key for embedding generation
# Get from: https://platform.openai.com/api-keys
OPENAI_API_KEY=sk-your-openai-api-key
```

Secure the file permissions:

```bash
sudo chmod 640 /etc/engram/environment
sudo chown root:engram /etc/engram/environment
```

### Service Configuration

The main configuration file is `/etc/engram/engram.yaml`:

```yaml
# HTTP server configuration
server:
  port: 8080
  read_timeout: 30s
  write_timeout: 60s
  shutdown_timeout: 30s

# Database configuration
database:
  path: /var/lib/engram/engram.db

# Embedding service configuration
embedding:
  model: text-embedding-3-small
  dimensions: 1536

# Background worker configuration
worker:
  snapshot_interval: 1h
  decay_interval: 24h
  embedding_retry_interval: 5m
  embedding_retry_max_attempts: 3

# Logging configuration
log:
  level: info
  format: json

# Semantic deduplication
deduplication:
  enabled: true
  similarity_threshold: 0.92
```

## Managing the Service

### Start the Service

```bash
sudo systemctl start engram
```

### Enable on Boot

```bash
sudo systemctl enable engram
```

### Check Status

```bash
sudo systemctl status engram
```

### View Logs

```bash
# Recent logs
sudo journalctl -u engram -n 50

# Follow logs in real-time
sudo journalctl -u engram -f

# Logs since last boot
sudo journalctl -u engram -b
```

### Restart After Configuration Changes

```bash
sudo systemctl restart engram
```

### Stop the Service

```bash
sudo systemctl stop engram
```

## Verify Installation

Check that the service is running and responding:

```bash
# Check service status
sudo systemctl status engram

# Test the health endpoint (no authentication required)
curl http://localhost:8080/api/v1/health

# Expected response:
# {"status":"healthy","version":"1.0.0","embedding_model":"text-embedding-3-small","lore_count":0,"last_snapshot":null}
```

## Security Features

The systemd unit file includes security hardening:

| Feature | Description |
|---------|-------------|
| `NoNewPrivileges` | Prevents privilege escalation |
| `ProtectSystem=strict` | Read-only filesystem except allowed paths |
| `ProtectHome` | No access to home directories |
| `PrivateTmp` | Isolated /tmp directory |
| `PrivateDevices` | No access to physical devices |
| `MemoryMax=512M` | Memory limit |
| `TasksMax=100` | Process limit |

## Troubleshooting

### Service Fails to Start

1. Check the logs:
   ```bash
   sudo journalctl -u engram -n 100 --no-pager
   ```

2. Verify API keys are set:
   ```bash
   sudo grep -v '^#' /etc/engram/environment | grep -v '^$'
   ```

3. Check file permissions:
   ```bash
   ls -la /etc/engram/
   ls -la /var/lib/engram/
   ```

### Permission Denied Errors

Ensure the engram user owns the data directory:

```bash
sudo chown -R engram:engram /var/lib/engram
sudo chmod 750 /var/lib/engram
```

### Database Locked Errors

SQLite requires exclusive access. Ensure only one Engram instance is running:

```bash
sudo systemctl stop engram
sudo lsof /var/lib/engram/engram.db
sudo systemctl start engram
```

### Connection Refused

1. Check if the service is running:
   ```bash
   sudo systemctl status engram
   ```

2. Verify the port:
   ```bash
   sudo ss -tlnp | grep 8080
   ```

3. Check firewall rules:
   ```bash
   sudo iptables -L -n | grep 8080
   ```

### OpenAI API Errors

If embedding generation fails:

1. Verify your API key is valid
2. Check your OpenAI account has available credits
3. Review logs for specific error messages:
   ```bash
   sudo journalctl -u engram | grep -i openai
   ```

## Upgrading

### Package Upgrade

```bash
# Debian/Ubuntu
sudo dpkg -i engram_NEW_VERSION_linux_amd64.deb

# RHEL/Fedora
sudo rpm -U engram-NEW_VERSION.x86_64.rpm
```

### Manual Upgrade

```bash
sudo systemctl stop engram
sudo curl -L -o /usr/bin/engram \
  https://github.com/hyperengineering/engram/releases/download/vNEW/engram_linux_amd64
sudo chmod +x /usr/bin/engram
sudo systemctl start engram
```

## Backup

Back up the SQLite database regularly:

```bash
# Stop service for consistent backup
sudo systemctl stop engram
sudo cp /var/lib/engram/engram.db /backup/engram-$(date +%Y%m%d).db
sudo systemctl start engram
```

Or use SQLite's online backup (no downtime):

```bash
sudo -u engram sqlite3 /var/lib/engram/engram.db ".backup '/backup/engram.db'"
```

## Uninstalling

### Package Removal

```bash
# Debian/Ubuntu
sudo apt remove engram

# RHEL/Fedora
sudo dnf remove engram
```

### Manual Removal

```bash
sudo systemctl stop engram
sudo systemctl disable engram
sudo rm /usr/lib/systemd/system/engram.service
sudo rm /usr/bin/engram
sudo rm -rf /etc/engram
# Optionally remove data:
# sudo rm -rf /var/lib/engram
sudo userdel engram
sudo groupdel engram
sudo systemctl daemon-reload
```
