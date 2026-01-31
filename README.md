# Engram

__A physical alteration thought to occur in living neural tissue in response to stimuli, posited as an explanation for memory.__

**Central Memory for AI Agent Lore**

Engram enables AI agents to accumulate, persist, and recall experiential knowledge across sessions, projects, and distributed development environments. When an agent learns something valuable—a debugging insight, an architectural gotcha, a testing strategy—Engram preserves that knowledge and makes it available to all connected agents.

## How It Works

```
┌─────────────────────────────────────────────────────────────────────┐
│                         AI Agent Workflow                           │
│                                                                     │
│   Agent discovers insight  →  Records lore  →  Syncs to Engram      │
│   Agent starts new task    ←  Recalls lore  ←  Queries local DB     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│  Dev Container  │       │     Engram      │       │  Dev Container  │
│   (Agent A)     │       │  Central Lore   │       │   (Agent B)     │
│                 │       │    Service      │       │                 │
│ ┌─────────────┐ │       │ ┌─────────────┐ │       │ ┌─────────────┐ │
│ │   Recall    │─┼──sync─┼▶│    Lore     │◀┼─sync──┼─│   Recall    │ │
│ │  (client)   │◀┼───────┼─│   Store     │─┼───────┼▶│  (client)   │ │
│ └─────────────┘ │       │ └─────────────┘ │       │ └─────────────┘ │
└─────────────────┘       └─────────────────┘       └─────────────────┘
```

| Term | Role | Description |
|------|------|-------------|
| **Engram** | Central service | Stores, indexes, and synchronizes lore across environments |
| **Recall** | Local client | Agent-side library for recording and querying lore |
| **Lore** | The knowledge | Individual learnings with semantic embeddings for search |

## Key Features

- **Semantic Search** — Find relevant knowledge using meaning, not just keywords
- **Confidence Scoring** — Lore quality improves through agent feedback
- **Automatic Deduplication** — Semantically similar entries merge intelligently
- **Delta Synchronization** — Efficient sync keeps all environments current
- **Multi-Store Isolation** — Separate knowledge bases per project or team
- **Background Workers** — Embedding generation, confidence decay, and snapshots run automatically

## Example Use Flow with Agents

The following example shows how agents can integrate with Recall to maintain persistent project memory across the development pipeline.

### Agent Roles

| Agent | Role | Recall Usage |
|-------|------|--------------|
| **Architect** | Architecture & Design | Queries patterns/decisions, records ADRs, runs feedback-loop to harvest validated learnings |
| **Developer** | TDD Implementation | Queries friction points/edge cases, provides feedback on lore usefulness |
| **Code Reviewer** | Code Review | Queries testing strategies/quality patterns, provides feedback |
| **Refactorer** | Refactoring | Queries refactoring patterns, triggers sync before merge |

### Information Flow

```
                    ┌───────────────────────────────────┐
                    │            ENGRAM                 │
                    │      (Lore Knowledge Base)        │
                    └──────────────┬────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │ recall_query       │ recall_sync        │ recall_record
              ▼                    ▲                    ▲
    ┌──────────────────┐           │                    │
    │ Architect Agent  │───────────┼────────────────────┤
    │ query → feedback │           │                    │ (ADRs + feedback-loop)
    └────────┬─────────┘           │                    │
             │                     │                    │
             ▼                     │                    │
    ┌──────────────────┐           │                    │
    │ Developer Agent  │           │                    │
    │ query → feedback │           │                    │
    └────────┬─────────┘           │                    │
             │                     │                    │
             ▼                     │                    │
    ┌──────────────────┐           │                    │
    │ Review Agent     │           │                    │
    │ query → feedback │           │                    │
    └────────┬─────────┘           │                    │
             │                     │                    │
             ▼                     │                    │
    ┌──────────────────┐           │                    │
    │ Refactor Agent   │───────────┘                    │
    │ query → feedback │ (sync at deliver)              │
    └────────┬─────────┘                                │
             │                                          │
             ▼ (merge)                                  │
    ┌─────────────────┐                                 │
    │ Architect Agent │─────────────────────────────────┘
    │ feedback-loop   │ (record validated learnings)
    └─────────────────┘
```

### Workflow Integration

1. **Query at workflow start** — Each agent queries Engram for relevant lore before beginning work
2. **Feedback at workflow end** — Agents report which retrieved lore was helpful, not relevant, or incorrect
3. **Record only validated learnings** — Architect's feedback-loop records learnings only after implementation validates them
4. **Sync at natural boundaries** — Refactor agent triggers sync before merge to persist all feedback

This design ensures:
- All agents benefit from accumulated project knowledge
- Relevance signals continuously improve retrieval quality
- Only implementation-validated learnings enter the knowledge base
- No speculative or hypothetical knowledge pollutes the lore

## Installation

### Homebrew (macOS/Linux)

```bash
brew install hyperengineering/tap/engram
```

### Download Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/hyperengineering/engram/releases):

```bash
# Linux (amd64)
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_linux_amd64.tar.gz
tar -xzf engram_linux_amd64.tar.gz
sudo mv engram /usr/local/bin/

# macOS (Apple Silicon)
curl -LO https://github.com/hyperengineering/engram/releases/latest/download/engram_darwin_arm64.tar.gz
tar -xzf engram_darwin_arm64.tar.gz
sudo mv engram /usr/local/bin/
```

### Docker

```bash
docker pull ghcr.io/hyperengineering/engram:latest

docker run -p 8080:8080 \
  -e OPENAI_API_KEY="sk-..." \
  -e ENGRAM_API_KEY="your-secret-key" \
  -v engram_data:/data \
  ghcr.io/hyperengineering/engram:latest
```

### Build from Source

Requires Go 1.23+:

```bash
git clone https://github.com/hyperengineering/engram.git
cd engram
make build
./dist/engram
```

## Quick Start

1. **Set required environment variables**

   ```bash
   export OPENAI_API_KEY="sk-your-openai-api-key"    # For embedding generation
   export ENGRAM_API_KEY="your-secret-api-key"       # For client authentication
   ```

2. **Start the service**

   ```bash
   engram
   ```

3. **Verify it's running**

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

4. **Record your first lore entry**

   ```bash
   curl -X POST http://localhost:8080/api/v1/lore \
     -H "Authorization: Bearer $ENGRAM_API_KEY" \
     -H "Content-Type: application/json" \
     -d '{
       "source_id": "my-environment",
       "lore": [{
         "content": "Always verify queue consumer idempotency in integration tests",
         "context": "Discovered while debugging flaky test suite",
         "category": "TESTING_STRATEGY",
         "confidence": 0.80
       }]
     }'
   ```

## How Recall Clients Use Engram

[Recall](https://github.com/hyperengineering/recall) is the client library agents use to interact with Engram. It maintains a local database replica for fast semantic search and handles synchronization automatically.

**Typical agent workflow:**

1. **Bootstrap** — On startup, Recall downloads a snapshot from Engram
2. **Query** — During tasks, the agent searches local lore for relevant knowledge
3. **Record** — When the agent learns something new, it records lore locally
4. **Feedback** — After using recalled knowledge, the agent reports if it was helpful
5. **Sync** — Periodically, Recall pushes new lore and pulls updates from Engram

```go
// Initialize Recall client
client, _ := recall.New(recall.Config{
    EngramURL: "https://engram.example.com",
    APIKey:    os.Getenv("ENGRAM_API_KEY"),
    SourceID:  "devcontainer-abc123",
})

// Query for relevant lore
results, _ := client.Query(recall.QueryParams{
    Query: "handling race conditions in message queues",
    Limit: 5,
})

// Record new learning
client.Record(recall.RecordParams{
    Content:    "Consumer acknowledgments should use transactions",
    Context:    "Debugging message loss in production",
    Category:   "DEPENDENCY_BEHAVIOR",
    Confidence: 0.85,
})

// Provide feedback on recalled lore
client.Feedback(recall.FeedbackParams{
    Helpful:     []string{"L1", "L2"},
    NotRelevant: []string{"L3"},
})
```

## API Overview

### Core Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/health` | GET | Service health and metadata |
| `/api/v1/stats` | GET | Extended system metrics |
| `/api/v1/lore` | POST | Ingest lore entries |
| `/api/v1/lore/{id}` | DELETE | Soft-delete a lore entry |
| `/api/v1/lore/snapshot` | GET | Download full database snapshot |
| `/api/v1/lore/delta` | GET | Get changes since timestamp |
| `/api/v1/lore/feedback` | POST | Submit feedback on lore quality |

### Multi-Store Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/stores` | GET | List all stores |
| `/api/v1/stores` | POST | Create a new store |
| `/api/v1/stores/{store_id}` | GET | Get store information |
| `/api/v1/stores/{store_id}` | DELETE | Delete a store |
| `/api/v1/stores/{store_id}/lore/*` | — | Store-scoped lore operations |

All endpoints except `/health` and `/stats` require Bearer token authentication.

Store-scoped lore operations mirror the core `/lore/*` endpoints but operate on a specific store. See the [Multi-Store Guide](docs/multi-store.md) for details.

## Lore Categories

| Category | Description |
|----------|-------------|
| `ARCHITECTURAL_DECISION` | High-level system design choices |
| `PATTERN_OUTCOME` | Results of applying design patterns |
| `INTERFACE_LESSON` | API and contract design insights |
| `EDGE_CASE_DISCOVERY` | Unexpected behaviors found in testing |
| `IMPLEMENTATION_FRICTION` | Design-to-code translation difficulties |
| `TESTING_STRATEGY` | Testing approach insights |
| `DEPENDENCY_BEHAVIOR` | Library and framework gotchas |
| `PERFORMANCE_INSIGHT` | Performance characteristics and optimizations |

## Running as a System Service

For production deployments, run Engram as a systemd service:

```bash
# Install via package (recommended)
sudo dpkg -i engram_1.0.0_linux_amd64.deb

# Configure API keys
sudo nano /etc/engram/environment

# Start and enable
sudo systemctl enable --now engram
```

See [Systemd Setup Guide](docs/systemd-setup-guide.md) for detailed instructions.

## Documentation

- [Getting Started](docs/getting-started.md) — Installation and first steps
- [Configuration](docs/configuration.md) — All configuration options
- [API Usage](docs/api-usage.md) — Endpoint examples and workflows
- [Multi-Store Guide](docs/multi-store.md) — Isolate lore by project
- [Systemd Setup](docs/systemd-setup-guide.md) — Running as a Linux service
- [macOS Service](docs/macos-launchagent-setup.md) — Running as a macOS service via Homebrew
- [Error Reference](docs/errors.md) — Error types and troubleshooting
- [Technical Design](docs/engram.md) — Architecture and design decisions
- [Release Checklist](docs/release-checklist.md) — Release process for maintainers

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
│   └── worker/           # Background workers
├── migrations/           # Database migrations
└── docs/                 # Documentation
```

## License

MIT License — see [LICENSE](LICENSE) for details.

## Author

**Lauri Jutila**
[ljuti@nmux.dev](mailto:ljuti@nmux.dev)

## Sponsorship

This project is sponsored by [NeuralMux](https://neuralmux.com) and is part of the [Hyper Engineering](https://hyperengineering.com) initiative to advance the union of human creativity and machine intelligence to build systems at extremes of scale, resilience, and performance.