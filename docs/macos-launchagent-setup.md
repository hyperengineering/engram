# macOS LaunchAgent Setup

Run Engram as a background service on macOS using launchd.

## Prerequisites

- Engram installed via Homebrew (`brew install hyperengineering/tap/engram`)
- OpenAI API key for embedding generation
- Engram API key for client authentication

## Installation

### 1. Create required directories

```bash
# All paths use Homebrew prefix (works for both Apple Silicon and Intel)
mkdir -p "$(brew --prefix)/etc/engram"
mkdir -p "$(brew --prefix)/var/engram"
mkdir -p "$(brew --prefix)/var/log/engram"
```

### 2. Install the wrapper script

The wrapper script sources environment variables before starting Engram (similar to Linux systemd setup):

```bash
cp docs/engram-wrapper.sh "$(brew --prefix)/bin/engram-wrapper"
chmod +x "$(brew --prefix)/bin/engram-wrapper"
```

### 3. Configure API keys

Create the environment file:

```bash
nano "$(brew --prefix)/etc/engram/environment"
```

Add your API keys:

```bash
# Required: Generate a secure key for client authentication
# Run: openssl rand -hex 32
ENGRAM_API_KEY=your-generated-api-key

# Required: OpenAI API key for embedding generation
# Get from: https://platform.openai.com/api-keys
OPENAI_API_KEY=sk-your-openai-api-key

# Optional: Override default port (8080)
# ENGRAM_PORT=8080

# Optional: Override default host (localhost)
# ENGRAM_HOST=0.0.0.0

# Optional: Override default data directory
# ENGRAM_DATA_DIR=/path/to/data
```

Secure the file permissions:

```bash
chmod 600 "$(brew --prefix)/etc/engram/environment"
```

### 4. Generate and install the LaunchAgent

Generate the plist with your Homebrew prefix:

```bash
sed "s|{{HOMEBREW_PREFIX}}|$(brew --prefix)|g" \
    docs/com.hyperengineering.engram.plist.template \
    > ~/Library/LaunchAgents/com.hyperengineering.engram.plist
```

### 5. Load the service

```bash
launchctl load ~/Library/LaunchAgents/com.hyperengineering.engram.plist
```

## Managing the Service

### Check status

```bash
launchctl list | grep engram
```

A running service shows a PID in the first column:
```
1234    0    com.hyperengineering.engram
```

### Stop the service

```bash
launchctl stop com.hyperengineering.engram
```

### Start the service

```bash
launchctl start com.hyperengineering.engram
```

### Unload (disable) the service

```bash
launchctl unload ~/Library/LaunchAgents/com.hyperengineering.engram.plist
```

### Reload after configuration changes

```bash
launchctl unload ~/Library/LaunchAgents/com.hyperengineering.engram.plist
launchctl load ~/Library/LaunchAgents/com.hyperengineering.engram.plist
```

## Viewing Logs

```bash
# Follow logs in real-time
tail -f "$(brew --prefix)/var/log/engram/engram.log"

# View recent logs
tail -100 "$(brew --prefix)/var/log/engram/engram.log"
```

## Verify the Service

```bash
curl http://localhost:8080/api/v1/health
```

Expected response:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "embedding_model": "text-embedding-3-small",
  "lore_count": 0,
  "last_snapshot": null
}
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ENGRAM_API_KEY` | Yes | — | Secret key for client authentication |
| `OPENAI_API_KEY` | Yes | — | OpenAI API key for embeddings |
| `ENGRAM_PORT` | No | 8080 | Port to listen on |
| `ENGRAM_HOST` | No | localhost | Host to bind to |
| `ENGRAM_DATA_DIR` | No | `$(brew --prefix)/var/engram` | Data directory |

## Troubleshooting

### Service won't start

1. Check the log file for errors:
   ```bash
   cat "$(brew --prefix)/var/log/engram/engram.log"
   ```

2. Verify the wrapper script exists and is executable:
   ```bash
   ls -la "$(brew --prefix)/bin/engram-wrapper"
   ```

3. Verify the environment file exists:
   ```bash
   cat "$(brew --prefix)/etc/engram/environment"
   ```

4. Test running the wrapper manually:
   ```bash
   "$(brew --prefix)/bin/engram-wrapper"
   ```

### Permission denied errors

Ensure directories have correct permissions:
```bash
chmod 755 "$(brew --prefix)/var/engram"
chmod 755 "$(brew --prefix)/var/log/engram"
```

### Port already in use

Change the port in the environment file:
```bash
echo "ENGRAM_PORT=8081" >> "$(brew --prefix)/etc/engram/environment"
```

Then reload the service.

## Security Notes

- The environment file contains your API keys. Ensure it has appropriate permissions:
  ```bash
  chmod 600 "$(brew --prefix)/etc/engram/environment"
  ```

- By default, Engram binds to `localhost` only. To allow remote connections, set `ENGRAM_HOST=0.0.0.0` in the environment file, but ensure you have appropriate firewall rules.

## File Locations

All paths are relative to your Homebrew prefix (`brew --prefix`):

| Path | Purpose |
|------|---------|
| `$(brew --prefix)/etc/engram/environment` | API keys and configuration |
| `$(brew --prefix)/var/engram/` | Database and data files |
| `$(brew --prefix)/var/log/engram/engram.log` | Service logs |
| `$(brew --prefix)/bin/engram-wrapper` | Wrapper script |
| `$(brew --prefix)/bin/engram` | Engram binary |
| `~/Library/LaunchAgents/com.hyperengineering.engram.plist` | LaunchAgent definition |

**Actual paths by architecture:**

| Architecture | Homebrew Prefix |
|--------------|-----------------|
| Apple Silicon | `/opt/homebrew` |
| Intel | `/usr/local` |
