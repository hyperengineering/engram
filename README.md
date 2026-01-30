# Engram

Central Lore Service for AI agent experiential knowledge persistence and synchronization.

## Overview

Engram enables AI agents to accumulate, persist, and recall experiential lore across sessions, projects, and distributed development environments.

| Term | Role | Description |
|------|------|-------------|
| **Engram** | Central service | Where lore is stored and synchronized |
| **Recall** | Local client | How agents retrieve and contribute lore |
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
make run
```

### Using Devcontainer

Open the repository in VS Code with the Dev Containers extension installed. The container will automatically set up the development environment.

## Project Structure

```
engram/
├── cmd/engram/           # Engram central service binary
├── internal/
│   ├── api/              # HTTP API layer
│   ├── config/           # Configuration management
│   ├── embedding/        # Embedding service client
│   ├── store/            # Lore database operations
│   └── types/            # Shared types
├── pkg/recall/           # Recall client library (importable by Forge)
├── docs/                 # Documentation
└── .devcontainer/        # Development container configuration
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/health` | GET | Health check |
| `/api/v1/lore` | POST | Ingest lore |
| `/api/v1/lore/snapshot` | GET | Download database snapshot |
| `/api/v1/lore/delta` | GET | Get changes since timestamp |
| `/api/v1/lore/feedback` | POST | Submit feedback on lore |

## Recall Client Library

The Recall client library is available at `github.com/hyperengineering/engram/pkg/recall`:

```go
import "github.com/hyperengineering/engram/pkg/recall"

client, err := recall.New(recall.Config{
    LocalPath:    "/path/to/local/lore.db",
    EngramURL:    "https://engram.forge.dev",
    APIKey:       os.Getenv("ENGRAM_API_KEY"),
    SyncInterval: 5 * time.Minute,
})

// Query lore
result, err := client.Query(recall.QueryParams{
    Query: "queue consumer patterns",
    K:     5,
})

// Record new lore
lore, err := client.Record(recall.RecordParams{
    Content:  "Queue consumers benefit from idempotency checks",
    Category: recall.CategoryPatternOutcome,
    Context:  "story-2.1 implementation",
})

// Provide feedback
result, err := client.Feedback(recall.FeedbackParams{
    Helpful: []string{"L1", "L2"},
})
```

## Deployment

### Fly.io

```bash
# Create app
fly apps create engram

# Create volume for SQLite persistence
fly volumes create engram_data --size 1 --region ord

# Set secrets
fly secrets set OPENAI_API_KEY=sk-...
fly secrets set ENGRAM_API_KEY=...

# Deploy
fly deploy
```

## Documentation

See [docs/engram.md](docs/engram.md) for the full technical design document.

## License

Copyright (c) Hyperengineering
