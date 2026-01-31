# Getting Started with Engram

This guide walks you through installing Engram, starting the service, and verifying everything works correctly.

## What You'll Set Up

By the end of this guide, you'll have:
- A running Engram service accepting lore from agents
- Verified API connectivity
- Your first lore entry stored and searchable

## Prerequisites

- **OpenAI API key** — Required for embedding generation. [Get an API key](https://platform.openai.com/api-keys)
- **Go 1.23+** — Only needed if building from source. [Download Go](https://go.dev/dl/)

## Installation

Choose the installation method that fits your environment.

### Option 1: Homebrew (macOS/Linux)

The simplest installation method:

```bash
brew install hyperengineering/tap/engram
```

Verify installation:

```bash
engram version
```

### Option 2: Download Binary

Download pre-built binaries from [GitHub Releases](https://github.com/hyperengineering/engram/releases).

**Linux (amd64):**

```bash
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_linux_amd64.tar.gz
tar -xzf engram_linux_amd64.tar.gz
sudo mv engram /usr/local/bin/
```

**Linux (arm64):**

```bash
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_linux_arm64.tar.gz
tar -xzf engram_linux_arm64.tar.gz
sudo mv engram /usr/local/bin/
```

**macOS (Apple Silicon):**

```bash
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_darwin_arm64.tar.gz
tar -xzf engram_darwin_arm64.tar.gz
sudo mv engram /usr/local/bin/
```

**macOS (Intel):**

```bash
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_darwin_amd64.tar.gz
tar -xzf engram_darwin_amd64.tar.gz
sudo mv engram /usr/local/bin/
```

### Option 3: Docker

Run Engram in a container:

```bash
docker pull ghcr.io/hyperengineering/engram:latest

docker run -p 8080:8080 \
  -e OPENAI_API_KEY="sk-your-openai-api-key" \
  -e ENGRAM_API_KEY="your-secret-api-key" \
  -v engram_data:/data \
  ghcr.io/hyperengineering/engram:latest
```

### Option 4: Build from Source

Clone and build:

```bash
git clone https://github.com/hyperengineering/engram.git
cd engram
make build
```

The binary is created at `./dist/engram`.

## Configuration

### Required Environment Variables

Set these before starting Engram:

```bash
# OpenAI API key for generating semantic embeddings
export OPENAI_API_KEY="sk-your-openai-api-key"

# API key for authenticating Recall clients
# Generate a secure random string: openssl rand -hex 32
export ENGRAM_API_KEY="your-secret-api-key"
```

### Optional Configuration

Common options you may want to customize:

```bash
# Change the server port (default: 8080)
export ENGRAM_PORT=3000

# Change the database location (default: ./data/engram.db)
export ENGRAM_DB_PATH=/var/lib/engram/lore.db

# Set log level (default: info)
export ENGRAM_LOG_LEVEL=debug
```

See [Configuration Reference](configuration.md) for all options.

## Start the Service

Run Engram:

```bash
engram
```

You should see startup logs:

```
{"level":"info","msg":"Starting Engram","version":"1.0.0","address":"0.0.0.0:8080"}
{"level":"info","msg":"Database initialized","path":"data/engram.db"}
{"level":"info","msg":"Background workers started"}
{"level":"info","msg":"Server listening","address":"0.0.0.0:8080"}
```

## Verify Installation

### Check Service Health

The health endpoint confirms the service is running:

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

### Ingest Your First Lore Entry

Test the lore ingestion API:

```bash
curl -X POST http://localhost:8080/api/v1/lore \
  -H "Authorization: Bearer $ENGRAM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "getting-started-test",
    "lore": [
      {
        "content": "Rails migrations run in alphabetical order by timestamp prefix",
        "context": "Discovered while debugging migration order issue",
        "category": "DEPENDENCY_BEHAVIOR",
        "confidence": 0.85
      }
    ]
  }'
```

Expected response:

```json
{
  "accepted": 1,
  "merged": 0,
  "rejected": 0,
  "errors": []
}
```

### Verify Storage

Check the health endpoint again to confirm lore count increased:

```bash
curl http://localhost:8080/api/v1/health
```

You should see `"lore_count": 1`.

### Check Extended Stats

View detailed system metrics:

```bash
curl http://localhost:8080/api/v1/stats
```

Response includes lore counts, embedding pipeline status, category distribution, and quality metrics.

## Troubleshooting

### Missing API Key Error

**Symptom:** `OPENAI_API_KEY is required` or `ENGRAM_API_KEY is required`

**Solution:** Set both environment variables:

```bash
export OPENAI_API_KEY="sk-..."
export ENGRAM_API_KEY="your-secret-key"
```

### Port Already in Use

**Symptom:** `listen tcp :8080: bind: address already in use`

**Solution:** Either stop the process using port 8080, or change Engram's port:

```bash
export ENGRAM_PORT=3000
engram
```

### Database Permission Denied

**Symptom:** `failed to open database: permission denied`

**Solution:** Ensure the data directory exists and is writable:

```bash
mkdir -p data
chmod 755 data
```

### Dev Mode (Skip OpenAI Key)

For local development without embedding generation:

```bash
export ENGRAM_DEV_MODE=true
engram
```

Lore is accepted but embeddings won't be generated, so semantic search won't work.

### Connection Refused from Docker

If running Engram in Docker and connecting from the host:

```bash
# Ensure the container exposes the port
docker run -p 8080:8080 ...

# Test from host
curl http://localhost:8080/api/v1/health
```

## Next Steps

Now that Engram is running:

1. **Set up Recall clients** — Configure your AI agents to use the [Recall client library](https://github.com/hyperengineering/recall)

2. **Run as a system service** — For production, set up [systemd](systemd-setup-guide.md) to run Engram automatically

3. **Explore the API** — See [API Usage Guide](api-usage.md) for detailed endpoint examples

4. **Configure for production** — Review [Configuration Reference](configuration.md) for all options
