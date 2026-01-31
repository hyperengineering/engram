# macOS Service Setup

Run Engram as a background service on macOS using Homebrew services.

## Installation

```bash
brew install hyperengineering/tap/engram
```

This automatically:
- Installs the `engram` binary
- Creates the wrapper script at `$(brew --prefix)/bin/engram-wrapper`
- Creates directories for config, data, and logs
- Creates an environment template at `$(brew --prefix)/etc/engram/environment`

## Configuration

Configure your API keys:

```bash
nano "$(brew --prefix)/etc/engram/environment"
```

Set the required values:

```bash
# Required: Generate a secure random key
# Run: openssl rand -hex 32
ENGRAM_API_KEY=your-generated-api-key

# Required: OpenAI API key for embedding generation
# Get from: https://platform.openai.com/api-keys
OPENAI_API_KEY=sk-your-openai-api-key
```

## Start the Service

```bash
brew services start engram
```

That's it. Engram is now running as a background service.

## Managing the Service

### Check status

```bash
brew services info engram
```

### Stop the service

```bash
brew services stop engram
```

### Restart after configuration changes

```bash
brew services restart engram
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

## Viewing Logs

```bash
# Follow logs in real-time
tail -f "$(brew --prefix)/var/log/engram/engram.log"

# View recent logs
tail -100 "$(brew --prefix)/var/log/engram/engram.log"
```

## Configuration Reference

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ENGRAM_API_KEY` | Yes | — | Secret key for client authentication |
| `OPENAI_API_KEY` | Yes | — | OpenAI API key for embeddings |
| `ENGRAM_PORT` | No | 8080 | Port to listen on |
| `ENGRAM_HOST` | No | localhost | Host to bind to |

## File Locations

All paths relative to Homebrew prefix (`brew --prefix`):

| Path | Purpose |
|------|---------|
| `etc/engram/environment` | API keys and configuration |
| `var/engram/` | Database and data files |
| `var/log/engram/engram.log` | Service logs |
| `bin/engram` | Engram binary |
| `bin/engram-wrapper` | Wrapper script (sources environment) |

**Actual paths by architecture:**

| Architecture | Homebrew Prefix |
|--------------|-----------------|
| Apple Silicon | `/opt/homebrew` |
| Intel | `/usr/local` |

## Troubleshooting

### Service won't start

1. Check the log file:
   ```bash
   cat "$(brew --prefix)/var/log/engram/engram.log"
   ```

2. Verify API keys are set:
   ```bash
   grep -v '^#' "$(brew --prefix)/etc/engram/environment" | grep -v '^$'
   ```

3. Test running manually:
   ```bash
   "$(brew --prefix)/bin/engram-wrapper"
   ```

### Port already in use

Change the port in the environment file:
```bash
echo "ENGRAM_PORT=8081" >> "$(brew --prefix)/etc/engram/environment"
brew services restart engram
```

## Security Notes

- The environment file contains your API keys. Verify permissions:
  ```bash
  ls -la "$(brew --prefix)/etc/engram/environment"
  # Should show: -rw------- (600)
  ```

- By default, Engram binds to `localhost` only. To allow remote connections, set `ENGRAM_HOST=0.0.0.0`, but ensure you have appropriate firewall rules.

## Uninstalling

```bash
brew services stop engram
brew uninstall engram

# Optionally remove data and config:
rm -rf "$(brew --prefix)/etc/engram"
rm -rf "$(brew --prefix)/var/engram"
rm -rf "$(brew --prefix)/var/log/engram"
```
