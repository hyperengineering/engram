# Engram

Central Lore Service for AI agent experiential knowledge persistence and synchronization.

## Overview

Engram enables AI agents to accumulate, persist, and recall experiential lore across sessions, projects, and distributed development environments.

| Term | Role | Description |
|------|------|-------------|
| **Engram** | Central service | Where lore is stored and synchronized |
| **Recall** | Local client | How agents retrieve and contribute lore (separate repository) |
| **Lore** | The knowledge | Individual learnings — the substance itself |

## Quick Start

### Prerequisites

- Go 1.23+
- OpenAI API key (for embeddings)

### Local Development

```bash
# Clone the repository
git clone https://github.com/hyperengineering/engram.git
cd engram

# Install dependencies
go mod download

# Copy environment configuration
cp .env.example .env
# Edit .env with your API keys

# Run tests
make test

# Build
make build

# Run the service
./engram
```

### Using Devcontainer

Open the repository in VS Code with the Dev Containers extension installed. The container will automatically set up the development environment.

## Project Structure

```
engram/
├── cmd/engram/           # Service entry point
├── internal/
│   ├── api/              # HTTP handlers, middleware, routing
│   ├── config/           # Configuration management
│   ├── embedding/        # OpenAI embedding client
│   ├── store/            # SQLite lore database operations
│   ├── types/            # Domain types
│   ├── validation/       # Input validation
│   └── worker/           # Background workers (embedding retry, decay, snapshot)
├── migrations/           # Database migrations
├── docs/                 # Documentation
└── .devcontainer/        # Development container configuration
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/health` | GET | Health check and service status |
| `/api/v1/lore` | POST | Ingest lore entries |
| `/api/v1/lore/{id}` | DELETE | Soft-delete a lore entry |
| `/api/v1/lore/snapshot` | GET | Download database snapshot |
| `/api/v1/lore/delta` | GET | Get changes since timestamp |
| `/api/v1/lore/feedback` | POST | Submit feedback on lore quality |

See [docs/openapi.yaml](docs/openapi.yaml) for the complete OpenAPI specification.

## Background Workers

Engram runs several background workers:

| Worker | Interval | Purpose |
|--------|----------|---------|
| Embedding Retry | 30s | Retry failed embedding generations |
| Confidence Decay | 24h | Decay confidence for stale lore |
| Snapshot | 1h | Generate point-in-time database snapshots |

## Configuration

Environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ENGRAM_ADDRESS` | No | `0.0.0.0:8080` | Server listen address |
| `ENGRAM_DB_PATH` | No | `./data/lore.db` | SQLite database path |
| `OPENAI_API_KEY` | Yes | — | OpenAI API key for embeddings |
| `OPENAI_EMBEDDING_MODEL` | No | `text-embedding-3-small` | Embedding model |
| `ENGRAM_API_KEY` | Yes | — | API key for authentication |
| `ENGRAM_LOG_LEVEL` | No | `info` | Log level (debug, info, warn, error) |

See [docs/configuration.md](docs/configuration.md) for detailed configuration options.

## Deployment

Engram can be deployed to any platform that supports Docker containers or Go binaries.

### Docker

```bash
docker build -t engram .
docker run -p 8080:8080 \
  -e OPENAI_API_KEY=sk-... \
  -e ENGRAM_API_KEY=... \
  -v engram_data:/data \
  engram
```

### Binary

```bash
# Build for your platform
make build

# Run with environment variables
OPENAI_API_KEY=sk-... ENGRAM_API_KEY=... ./engram
```

Ensure persistent storage is configured for the SQLite database path (`ENGRAM_DB_PATH`).

## Client Library

The Recall client library is maintained in a separate repository:

- **Repository:** [github.com/hyperengineering/recall](https://github.com/hyperengineering/recall)

## Documentation

- [Getting Started](docs/getting-started.md) — Quick start guide
- [API Usage](docs/api-usage.md) — API examples and patterns
- [Configuration](docs/configuration.md) — Configuration reference
- [Error Handling](docs/errors.md) — Error codes and troubleshooting
- [Technical Design](docs/engram.md) — Full architecture document

## License

MIT License — see [LICENSE](LICENSE) for details.
