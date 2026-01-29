---
story: "6.2"
title: "User Documentation"
status: implemented
designedBy: Clario
designedAt: "2026-01-29"
implementedBy: Spark
implementedAt: "2026-01-29"
frs: []
nfrs: []
---

# Technical Design: Story 6.2 - User Documentation

## 1. Story Summary

As a **developer deploying or integrating with Engram**, I want comprehensive user documentation covering setup, configuration, and API usage, so that I can deploy and operate Engram without reading the source code.

**Key Deliverables:**
- `docs/getting-started.md` — Deployment and first-run guide
- `docs/configuration.md` — Complete configuration reference
- `docs/errors.md` — RFC 7807 error type reference
- `docs/api-usage.md` — Practical API examples and workflows

## 2. Gap Analysis

### Current State

| Artifact | Status | Notes |
|----------|--------|-------|
| Technical design doc | Exists | `docs/engram.md` — internal, not user-facing |
| Architecture doc | Exists | `_bmad-output/planning-artifacts/architecture.md` — internal |
| Getting started guide | Missing | No deployment instructions |
| Configuration reference | Missing | Config options scattered in code |
| Error reference | Missing | Errors defined in code only |
| API usage guide | Missing | No curl examples or workflows |

### Source Documents for Extraction

1. **Architecture Document** — Configuration approach, error format, API patterns
2. **Config Package** (`internal/config/`) — All configuration options
3. **Problem Package** (`internal/api/problem.go`) — Error types and URIs
4. **Handler Package** (`internal/api/handlers.go`) — Endpoint behavior
5. **Story Files** — Request/response formats from technical designs

## 3. Document Specifications

### 3.1 Getting Started Guide (`docs/getting-started.md`)

**Purpose:** Get a developer from zero to running Engram in under 10 minutes.

**Structure:**

```markdown
# Getting Started with Engram

## Prerequisites

- Go 1.23 or later
- OpenAI API key (for embedding generation)
- (Optional) Docker for containerized deployment

## Quick Start

### Option 1: Run from Source

1. Clone the repository
2. Set environment variables
3. Build and run
4. Verify health

### Option 2: Docker

1. Pull/build image
2. Run with volume mount
3. Verify health

## First Run Verification

### Check Health

curl http://localhost:8080/api/v1/health

### Ingest First Lore Entry

curl -X POST ... (example)

### Verify Storage

curl ... (delta or health check)

## Next Steps

- [Configuration Reference](configuration.md)
- [API Usage Guide](api-usage.md)
- [Error Reference](errors.md)
```

**Content Requirements:**

| Section | Details |
|---------|---------|
| Prerequisites | Go version, OpenAI API key requirement, optional Docker |
| Clone & Build | `git clone`, `make build`, binary location |
| Environment Setup | `ENGRAM_API_KEY`, `OPENAI_API_KEY`, `ENGRAM_DB_PATH` |
| Run Commands | Direct binary, Docker, Fly.io |
| Health Check | curl command with expected output |
| First Ingest | curl POST with minimal valid payload |
| Troubleshooting | Common issues (missing API key, port in use, DB permissions) |

### 3.2 Configuration Reference (`docs/configuration.md`)

**Purpose:** Document every configuration option with defaults, types, and examples.

**Structure:**

```markdown
# Configuration Reference

## Configuration Sources

Engram loads configuration from multiple sources with the following precedence:
1. Environment variables (highest priority)
2. YAML config file
3. Default values (lowest priority)

## Environment Variables

All environment variables use the `ENGRAM_` prefix.

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `ENGRAM_API_KEY` | string | (required) | API key for client authentication |
| `ENGRAM_DB_PATH` | string | `data/engram.db` | SQLite database path |
| ... | ... | ... | ... |

## Config File

Default location: `config/engram.yaml`

### Example Configuration

```yaml
# config/engram.yaml
server:
  port: 8080
  ...
```

## Configuration Options

### Server Configuration
### Database Configuration
### Embedding Configuration
### Worker Configuration
### Logging Configuration
```

**Content Requirements:**

| Category | Options to Document |
|----------|---------------------|
| Server | `port`, `host`, `read_timeout`, `write_timeout`, `shutdown_timeout` |
| Database | `db_path`, `wal_mode`, `busy_timeout` |
| Embedding | `openai_api_key`, `embedding_model`, `batch_size`, `retry_max`, `retry_delay` |
| Workers | `snapshot_interval`, `decay_interval`, `decay_threshold`, `decay_amount` |
| Logging | `log_level`, `log_format` |
| Auth | `api_key` |

**Format per Option:**

```markdown
### `server.port`

**Type:** integer
**Default:** `8080`
**Environment:** `ENGRAM_SERVER_PORT`

The TCP port the HTTP server listens on.

**Example:**
```yaml
server:
  port: 3000
```
```

### 3.3 Error Reference (`docs/errors.md`)

**Purpose:** Document all RFC 7807 error types with causes and resolutions.

**Structure:**

```markdown
# Error Reference

Engram returns errors in [RFC 7807 Problem Details](https://www.rfc-editor.org/rfc/rfc7807) format.

## Error Response Format

All error responses include:
- `type`: URI identifying the error type
- `title`: Human-readable summary
- `status`: HTTP status code
- `detail`: Detailed explanation
- `instance`: Request path that caused the error

## Error Types

### Unauthorized (401)

**Type URI:** `https://engram.dev/errors/unauthorized`

**Causes:**
- Missing Authorization header
- Invalid Bearer token format
- Incorrect API key

**Resolution:**
- Verify API key is correct
- Ensure header format: `Authorization: Bearer <api_key>`

**Example Response:**
```json
{
  "type": "https://engram.dev/errors/unauthorized",
  "title": "Unauthorized",
  "status": 401,
  "detail": "Missing or invalid Authorization header",
  "instance": "/api/v1/lore"
}
```

### Validation Error (422)
### Not Found (404)
### Bad Request (400)
### Internal Server Error (500)
### Service Unavailable (503)
```

**Error Types to Document:**

| Type | Status | When Returned |
|------|--------|---------------|
| `unauthorized` | 401 | Missing/invalid API key |
| `validation-error` | 422 | Request validation failed |
| `not-found` | 404 | Lore ID doesn't exist |
| `bad-request` | 400 | Malformed JSON, invalid timestamp |
| `internal-error` | 500 | Unexpected server error |
| `service-unavailable` | 503 | Snapshot not ready |

### 3.4 API Usage Guide (`docs/api-usage.md`)

**Purpose:** Practical examples for common integration workflows.

**Structure:**

```markdown
# API Usage Guide

## Authentication

All endpoints except `/health` require Bearer token authentication.

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" ...
```

## Common Workflows

### Initial Bootstrap (New Environment)

1. Check service health
2. Download snapshot
3. Store locally for semantic search

### Incremental Sync (Existing Environment)

1. Query delta since last sync
2. Apply changes to local store
3. Update last_sync timestamp

### Recording Lore (Agent Learning)

1. Validate lore locally
2. Submit batch to Engram
3. Handle merge responses

### Providing Feedback (Agent Experience)

1. Collect feedback during session
2. Submit batch feedback on shutdown
3. Process confidence adjustments

## Endpoint Examples

### Health Check

```bash
curl http://localhost:8080/api/v1/health
```

Response:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  ...
}
```

### Ingest Lore

```bash
curl -X POST http://localhost:8080/api/v1/lore \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "source_id": "devcontainer-abc123",
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

### Download Snapshot

### Get Delta

### Submit Feedback

## Error Handling

### Validation Errors
### Authentication Failures
### Rate Limiting (Future)
```

**Content Requirements:**

| Section | Details |
|---------|---------|
| Authentication | Bearer token format, where to get API key |
| Health Check | curl example, expected response, when to use |
| Ingest Lore | Full request example, all fields explained, response interpretation |
| Snapshot | Download command, file handling, when to use |
| Delta Sync | Query format, response handling, pagination (if any) |
| Feedback | Request format, feedback types explained, confidence math |
| Error Handling | How to parse Problem Details, retry logic |
| Best Practices | Batch sizes, sync intervals, shutdown flush |

## 4. Writing Standards

### 4.1 Tone and Voice

- **Direct and practical** — No marketing language
- **Second person** — "You can configure..." not "Users can configure..."
- **Active voice** — "Run the command" not "The command should be run"
- **Specific and concrete** — Include exact values, not vague descriptions

### 4.2 Code Examples

- All curl commands must be copy-paste ready
- Include both request and response for each example
- Use realistic but obviously fake data (e.g., `YOUR_API_KEY`, `devcontainer-abc123`)
- Test all examples against running service before publishing

### 4.3 Formatting

- Use tables for reference data (config options, error types)
- Use code blocks with language hints for all examples
- Use admonitions for warnings/notes (if markdown processor supports)
- Keep sections focused — one concept per heading

## 5. Implementation Checklist

### Getting Started Guide

- [x] Write Prerequisites section
- [x] Write Quick Start (source) section
- [x] Write Quick Start (Fly.io) section
- [x] Write First Run Verification section
- [x] Add troubleshooting for common issues
- [x] Test all commands against codebase

### Configuration Reference

- [x] Extract all config options from code
- [x] Document server configuration
- [x] Document database configuration
- [x] Document embedding configuration
- [x] Document worker configuration
- [x] Document logging configuration
- [x] Document deduplication configuration
- [x] Create example config file
- [x] Verify all defaults match code

### Error Reference

- [x] Document error response format
- [x] Document 401 Unauthorized
- [x] Document 400 Bad Request
- [x] Document 404 Not Found
- [x] Document 409 Conflict
- [x] Document 422 Validation Error
- [x] Document 500 Internal Error
- [x] Document 503 Service Unavailable
- [x] Add example responses for each

### API Usage Guide

- [x] Write Authentication section
- [x] Write Initial Bootstrap workflow
- [x] Write Incremental Sync workflow
- [x] Write Recording Lore workflow
- [x] Write Providing Feedback workflow
- [x] Add curl examples for all endpoints
- [x] Add error handling guidance
- [x] Add best practices section
- [x] All curl examples verified against handlers.go

### Final Validation

- [x] All code examples verified against implementation
- [x] No placeholder text or TODOs remain
- [x] Cross-links between documents work
- [x] Technical accuracy verified against source code

## 6. Design Decisions

### D1: Separate Files vs Single Document

**Decision:** Four separate focused documents rather than one large guide.

**Rationale:** Different audiences, different purposes. Operators need config reference, integrators need API usage, troubleshooters need error reference. Separate files allow targeted reading.

### D2: curl for Examples

**Decision:** Use curl for all API examples, not language-specific clients.

**Rationale:** Universal — works regardless of client language. Readers can translate to their preferred HTTP client.

### D3: Realistic but Fake Data

**Decision:** Examples use realistic-looking but obviously fake values.

**Rationale:** Copy-paste safety — users won't accidentally use real credentials. `YOUR_API_KEY` and `devcontainer-abc123` are clearly placeholders.

### D4: No Version-Specific Paths

**Decision:** Document current version only, no multi-version documentation.

**Rationale:** Single version (v1) in production. Version documentation adds maintenance burden for no current benefit.

---

## 7. Implementation Notes

### Deliverables Created

| File | Description |
|------|-------------|
| `docs/getting-started.md` | Zero-to-running guide with source and Fly.io deployment |
| `docs/configuration.md` | Complete config reference extracted from `internal/config/config.go` |
| `docs/errors.md` | RFC 7807 error types extracted from `internal/api/problem.go` |
| `docs/api-usage.md` | Practical curl examples verified against `internal/api/handlers.go` |

### Deviations from Design

1. **Fly.io instead of Docker** — The codebase uses Fly.io for deployment (per `fly.toml`), not Docker. Documentation reflects actual deployment method.

2. **Added 409 Conflict error** — Found in `problem.go` but not in original design. Documented for completeness.

3. **Added deduplication configuration** — The `deduplication` config section exists in code but wasn't in the original design spec. Documented in configuration.md.

4. **Feedback API field names** — The actual implementation uses `lore_id` and `type` fields (per `handlers.go`), documented accordingly.

### Verification Summary

All documentation was verified against actual source code:

- **Configuration defaults** — Verified against `newDefaults()` in `config.go:153-182`
- **Environment variables** — Verified against `applyEnvOverrides()` in `config.go:206-286`
- **Error types** — Verified against `problemTypes` map in `problem.go:23-55`
- **Endpoint routes** — Verified against `NewRouter()` in `routes.go:8-33`
- **Request/response formats** — Verified against handlers in `handlers.go`
- **Validation rules** — Verified against `validation.go`

---

The path is clear. Build well.
