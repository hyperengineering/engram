# Configuration Reference

Complete reference for all Engram configuration options.

## Minimal Configuration

To run Engram, you only need two environment variables:

```bash
export OPENAI_API_KEY="sk-your-openai-api-key"
export ENGRAM_API_KEY="your-secret-api-key"
engram
```

Everything else has sensible defaults. This section covers all available options for production tuning.

## Configuration Sources

Engram loads configuration from multiple sources with the following precedence (highest to lowest):

1. **Environment variables** — Override all other sources
2. **YAML config file** — `config/engram.yaml` by default
3. **Default values** — Built-in defaults

## Environment Variables

All environment variables use the `ENGRAM_` prefix, except `OPENAI_API_KEY` which follows industry convention.

### Quick Reference

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `ENGRAM_API_KEY` | string | (required) | API key for client authentication |
| `OPENAI_API_KEY` | string | (required) | OpenAI API key for embedding generation |
| `ENGRAM_PORT` | integer | `8080` | HTTP server port |
| `ENGRAM_DB_PATH` | string | `data/engram.db` | SQLite database path |
| `ENGRAM_CONFIG_PATH` | string | `config/engram.yaml` | YAML config file path |
| `ENGRAM_DEV_MODE` | boolean | `false` | Skip API key validation (dev only) |
| `ENGRAM_LOG_LEVEL` | string | `info` | Logging level |
| `ENGRAM_LOG_FORMAT` | string | `json` | Log output format |
| `ENGRAM_STORES_ROOT` | string | `~/.engram/stores` | Root directory for multi-store data |

## Configuration Options

### Server Configuration

#### `ENGRAM_PORT`

**Type:** integer
**Default:** `8080`
**YAML path:** `server.port`

The TCP port the HTTP server listens on.

```bash
export ENGRAM_PORT=3000
```

```yaml
server:
  port: 3000
```

---

#### `ENGRAM_READ_TIMEOUT`

**Type:** duration
**Default:** `30s`
**YAML path:** `server.read_timeout`

Maximum duration for reading the entire request, including the body.

```bash
export ENGRAM_READ_TIMEOUT=60s
```

```yaml
server:
  read_timeout: "60s"
```

---

#### `ENGRAM_WRITE_TIMEOUT`

**Type:** duration
**Default:** `30s`
**YAML path:** `server.write_timeout`

Maximum duration before timing out writes of the response.

```bash
export ENGRAM_WRITE_TIMEOUT=60s
```

```yaml
server:
  write_timeout: "60s"
```

---

#### `ENGRAM_SHUTDOWN_TIMEOUT`

**Type:** duration
**Default:** `15s`
**YAML path:** `server.shutdown_timeout`

Maximum duration to wait for active connections to close during graceful shutdown.

```bash
export ENGRAM_SHUTDOWN_TIMEOUT=30s
```

```yaml
server:
  shutdown_timeout: "30s"
```

---

### Database Configuration

#### `ENGRAM_DB_PATH`

**Type:** string
**Default:** `data/engram.db`
**YAML path:** `database.path`

Path to the SQLite database file. The directory must exist and be writable.

```bash
export ENGRAM_DB_PATH=/var/lib/engram/lore.db
```

```yaml
database:
  path: "/var/lib/engram/lore.db"
```

---

### Multi-Store Configuration

Engram supports multiple isolated stores for different projects. Each store has its own database, snapshots, and metadata.

#### `ENGRAM_STORES_ROOT`

**Type:** string
**Default:** `~/.engram/stores`
**YAML path:** `stores.root_path`

Root directory where store data is persisted. Each store creates a subdirectory under this path.

```bash
export ENGRAM_STORES_ROOT=/var/lib/engram/stores
```

```yaml
stores:
  root_path: "/var/lib/engram/stores"
```

**Directory Structure:**

```
~/.engram/stores/
├── default/
│   ├── engram.db
│   ├── meta.yaml
│   └── snapshots/
├── neuralmux/
│   └── engram/
│       ├── engram.db
│       ├── meta.yaml
│       └── snapshots/
└── myproject/
    ├── engram.db
    ├── meta.yaml
    └── snapshots/
```

---

#### Multi-Project Example

For organizations running multiple projects, configure each project's Recall client with a specific store:

**Server Configuration:**

```yaml
stores:
  root_path: "/data/engram/stores"
```

**Recall Client Configuration (per project):**

```yaml
# In project's _bmad config
engram_store: "neuralmux/engram"
```

Or via environment variable:

```bash
export ENGRAM_STORE="neuralmux/engram"
```

See the [Multi-Store Guide](multi-store.md) for detailed configuration options.

---

### Embedding Configuration

#### `OPENAI_API_KEY`

**Type:** string
**Default:** (required)
**YAML path:** Not available (environment variable only for security)

OpenAI API key for embedding generation. This is required unless `ENGRAM_DEV_MODE=true`.

```bash
export OPENAI_API_KEY=sk-your-api-key
```

---

#### `ENGRAM_EMBEDDING_MODEL`

**Type:** string
**Default:** `text-embedding-3-small`
**YAML path:** `embedding.model`

OpenAI embedding model to use. The default model produces 1536-dimensional vectors.

```bash
export ENGRAM_EMBEDDING_MODEL=text-embedding-3-large
```

```yaml
embedding:
  model: "text-embedding-3-large"
```

---

### Authentication Configuration

#### `ENGRAM_API_KEY`

**Type:** string
**Default:** (required)
**YAML path:** Not available (environment variable only for security)

API key that clients must provide in the `Authorization: Bearer` header. Choose a secure, random string.

```bash
export ENGRAM_API_KEY=your-secret-api-key
```

---

### Worker Configuration

#### `ENGRAM_SNAPSHOT_INTERVAL`

**Type:** duration
**Default:** `1h`
**YAML path:** `worker.snapshot_interval`

Interval between automatic snapshot generation.

```bash
export ENGRAM_SNAPSHOT_INTERVAL=30m
```

```yaml
worker:
  snapshot_interval: "30m"
```

---

#### `ENGRAM_DECAY_INTERVAL`

**Type:** duration
**Default:** `24h`
**YAML path:** `worker.decay_interval`

Interval between confidence decay checks for stale lore.

```bash
export ENGRAM_DECAY_INTERVAL=12h
```

```yaml
worker:
  decay_interval: "12h"
```

---

#### `ENGRAM_EMBEDDING_RETRY_INTERVAL`

**Type:** duration
**Default:** `5m`
**YAML path:** `worker.embedding_retry_interval`

Interval between retry attempts for failed embedding generation.

```bash
export ENGRAM_EMBEDDING_RETRY_INTERVAL=10m
```

```yaml
worker:
  embedding_retry_interval: "10m"
```

---

#### `ENGRAM_EMBEDDING_RETRY_MAX_ATTEMPTS`

**Type:** integer
**Default:** `10`
**YAML path:** `worker.embedding_retry_max_attempts`

Maximum number of retry attempts for embedding generation before marking as failed.

```bash
export ENGRAM_EMBEDDING_RETRY_MAX_ATTEMPTS=5
```

```yaml
worker:
  embedding_retry_max_attempts: 5
```

---

### Logging Configuration

#### `ENGRAM_LOG_LEVEL`

**Type:** string
**Default:** `info`
**YAML path:** `log.level`

Logging verbosity level. Valid values: `debug`, `info`, `warn`, `error`.

```bash
export ENGRAM_LOG_LEVEL=debug
```

```yaml
log:
  level: "debug"
```

---

#### `ENGRAM_LOG_FORMAT`

**Type:** string
**Default:** `json`
**YAML path:** `log.format`

Log output format. Valid values: `json`, `text`.

```bash
export ENGRAM_LOG_FORMAT=text
```

```yaml
log:
  format: "text"
```

---

### Deduplication Configuration

#### `ENGRAM_DEDUPLICATION_ENABLED`

**Type:** boolean
**Default:** `true`
**YAML path:** `deduplication.enabled`

Enable semantic deduplication of incoming lore. When enabled, similar lore entries are merged rather than duplicated.

```bash
export ENGRAM_DEDUPLICATION_ENABLED=false
```

```yaml
deduplication:
  enabled: false
```

---

#### `ENGRAM_SIMILARITY_THRESHOLD`

**Type:** float
**Default:** `0.92`
**YAML path:** `deduplication.similarity_threshold`

Cosine similarity threshold for considering two lore entries as duplicates. Higher values require closer matches.

```bash
export ENGRAM_SIMILARITY_THRESHOLD=0.95
```

```yaml
deduplication:
  similarity_threshold: 0.95
```

---

### Development Configuration

#### `ENGRAM_DEV_MODE`

**Type:** boolean
**Default:** `false`

When set to `true`, skips API key validation at startup. Use only for local development.

```bash
export ENGRAM_DEV_MODE=true
```

---

#### `ENGRAM_CONFIG_PATH`

**Type:** string
**Default:** `config/engram.yaml`

Path to the YAML configuration file. If the file doesn't exist, defaults are used.

```bash
export ENGRAM_CONFIG_PATH=/etc/engram/config.yaml
```

---

## Example Configuration File

Create `config/engram.yaml`:

```yaml
# Engram Configuration

server:
  port: 8080
  read_timeout: "30s"
  write_timeout: "30s"
  shutdown_timeout: "15s"

database:
  path: "data/engram.db"

stores:
  root_path: "~/.engram/stores"

embedding:
  model: "text-embedding-3-small"
  dimensions: 1536

worker:
  snapshot_interval: "1h"
  decay_interval: "24h"
  embedding_retry_interval: "5m"
  embedding_retry_max_attempts: 10

log:
  level: "info"
  format: "json"

deduplication:
  enabled: true
  similarity_threshold: 0.92
```

## Duration Format

Duration values support Go duration strings:

- `30s` — 30 seconds
- `5m` — 5 minutes
- `1h` — 1 hour
- `24h` — 24 hours
- `1h30m` — 1 hour 30 minutes

## Docker Deployment

When deploying with Docker, pass configuration via environment variables:

```bash
docker run -p 8080:8080 \
  -e ENGRAM_DB_PATH="/data/lore.db" \
  -e ENGRAM_LOG_LEVEL="info" \
  -e ENGRAM_LOG_FORMAT="json" \
  -e OPENAI_API_KEY="sk-..." \
  -e ENGRAM_API_KEY="your-secret-key" \
  -v engram_data:/data \
  engram
```

For production deployments, use your platform's secret management for API keys.
