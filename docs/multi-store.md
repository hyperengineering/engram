# Multi-Store Guide

This guide covers deploying Engram with multiple isolated stores for multi-project and multi-team environments.

## Overview

By default, Engram uses a single `default` store. Multi-store support enables:

- **Project Isolation** — Each project maintains independent lore
- **Team Separation** — Teams can have dedicated knowledge bases
- **Organizational Hierarchy** — Use path-style IDs like `org/team/project`

## Quick Start

### 1. Create a Store

```bash
curl -X POST \
  -H "Authorization: Bearer $ENGRAM_API_KEY" \
  -H "Content-Type: application/json" \
  https://engram.example.com/api/v1/stores \
  -d '{"store_id": "myproject", "description": "My project lore"}'
```

### 2. Configure Recall Client

```bash
export ENGRAM_STORE="myproject"
```

### 3. Use Store-Scoped Endpoints

All lore operations now target `myproject`:

```bash
# Ingest
curl -X POST .../api/v1/stores/myproject/lore -d '{...}'

# Sync
curl .../api/v1/stores/myproject/lore/delta?since=...
```

## Store ID Format

### Rules

| Rule | Valid | Invalid |
|------|-------|---------|
| Lowercase only | `myproject` | `MyProject` |
| Alphanumeric + hyphens | `my-project` | `my_project` |
| No leading/trailing hyphens | `my-project` | `-myproject` |
| Max 4 path segments | `a/b/c/d` | `a/b/c/d/e` |
| Max 128 characters | `short-name` | (129+ chars) |
| No empty segments | `org/project` | `org//project` |

### Examples

```
# Simple
default
myproject
team-alpha

# Hierarchical
neuralmux/engram
neuralmux/recall
acme-corp/frontend/web
```

### URL Encoding

When using store IDs in URL paths, slashes must be URL-encoded:

| Store ID | URL Path |
|----------|----------|
| `default` | `/stores/default/lore` |
| `neuralmux/engram` | `/stores/neuralmux%2Fengram/lore` |

## The Default Store

The `default` store is special:

1. **Auto-created** on first access (other stores require explicit creation)
2. **Cannot be deleted** (403 Forbidden)
3. **Used by legacy routes** (`/api/v1/lore/*` → `default` store)

This ensures backward compatibility for existing deployments.

## Configuration

### Server-Side

Configure the root directory for all stores:

```yaml
# config/engram.yaml
stores:
  root_path: "/var/lib/engram/stores"
```

Or via environment:

```bash
export ENGRAM_STORES_ROOT="/var/lib/engram/stores"
```

### Client-Side (Recall)

Configure which store Recall uses:

**Option 1: Environment Variable**

```bash
export ENGRAM_STORE="neuralmux/engram"
```

**Option 2: BMAD Config**

```yaml
# _bmad/apexflow/config.yaml
engram_store: "neuralmux/engram"
```

**Option 3: Explicit in API Calls**

Use store-scoped endpoints directly.

## Common Patterns

### Organization/Project Hierarchy

```
acme-corp/
├── frontend/
│   ├── web/           # acme-corp/frontend/web
│   └── mobile/        # acme-corp/frontend/mobile
└── backend/
    ├── api/           # acme-corp/backend/api
    └── workers/       # acme-corp/backend/workers
```

### Team-Based Isolation

```
team-alpha/            # Team Alpha's shared lore
team-beta/             # Team Beta's shared lore
shared/                # Cross-team patterns
```

### Environment Separation

```
production/            # Production learnings
staging/               # Staging discoveries
development/           # Dev experiments
```

## Migration from Single-Store

Existing single-store deployments can migrate gradually:

### Step 1: Enable Multi-Store

Add to configuration:

```yaml
stores:
  root_path: "~/.engram/stores"
```

### Step 2: Existing Data

Your existing data remains accessible via the `default` store. The legacy database location is automatically used.

### Step 3: Create New Stores

Create stores for new projects:

```bash
curl -X POST .../api/v1/stores -d '{"store_id": "new-project"}'
```

### Step 4: Update Recall Clients

Configure each project's Recall client with its store ID.

## API Reference

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/stores` | GET | List all stores |
| `/api/v1/stores` | POST | Create store |
| `/api/v1/stores/{id}` | GET | Get store info |
| `/api/v1/stores/{id}` | DELETE | Delete store |
| `/api/v1/stores/{id}/lore` | POST | Ingest to store |
| `/api/v1/stores/{id}/lore/snapshot` | GET | Store snapshot |
| `/api/v1/stores/{id}/lore/delta` | GET | Store delta |
| `/api/v1/stores/{id}/lore/feedback` | POST | Store feedback |
| `/api/v1/stores/{id}/lore/{lore_id}` | DELETE | Delete from store |
| `/api/v1/health?store={id}` | GET | Store-specific health |

See [API Specification](api-specification.md) for full details.

## Troubleshooting

### Store Not Found (404)

Stores must be created explicitly before use:

```bash
# Create first
curl -X POST .../api/v1/stores -d '{"store_id": "mystore"}'

# Then use
curl -X POST .../api/v1/stores/mystore/lore -d '{...}'
```

### Invalid Store ID (400)

Check store ID format rules. Common issues:
- Uppercase letters
- Underscores (use hyphens)
- More than 4 path segments

### Cannot Delete Default Store (403)

The `default` store is protected. Create a new store and migrate data if needed.

### URL Encoding Issues

Store IDs with slashes need URL encoding:

```bash
# Wrong
curl .../api/v1/stores/org/project/lore

# Correct
curl .../api/v1/stores/org%2Fproject/lore
```
