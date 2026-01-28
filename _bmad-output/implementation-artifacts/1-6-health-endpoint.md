# Story 1.6: Health Endpoint

Status: dev-complete

## Story

As a **Recall client or operator checking Engram status**,
I want a health endpoint that reports service status and configuration,
so that I can verify the service is running and check embedding model compatibility.

## Acceptance Criteria

1. **Given** the Engram service is running
   **When** a GET request is made to `/api/v1/health`
   **Then** the response includes: `status` ("healthy"), `version`, `embedding_model`, `lore_count`, `last_snapshot` timestamp
   **And** the response is JSON with snake_case field names
   **And** the response returns within 100ms (NFR5)
   **And** no authentication is required

2. **Given** the store is accessible
   **When** the health endpoint queries store stats
   **Then** `lore_count` reflects the current number of lore entries
   **And** `last_snapshot` reflects the last snapshot generation time (or null if none)

## Tasks / Subtasks

- [x] Task 1: Define health response type (AC: #1)
  - [x] Define `HealthResponse` struct in handler or types:
    ```go
    type HealthResponse struct {
        Status         string     `json:"status"`
        Version        string     `json:"version"`
        EmbeddingModel string     `json:"embedding_model"`
        LoreCount      int64      `json:"lore_count"`
        LastSnapshot   *time.Time `json:"last_snapshot"`
    }
    ```
  - [x] Use pointer for `last_snapshot` to marshal as `null` when no snapshot exists
- [x] Task 2: Implement health handler (AC: #1, #2)
  - [x] Create or update health handler in `internal/api/handlers.go`
  - [x] Handler depends on `store.Store` interface (for `GetStats()`)
  - [x] Handler depends on `embedding.Embedder` interface (for `ModelName()`)
  - [x] Handler depends on app version string (from config or build-time variable)
  - [x] Query `Store.GetStats(ctx)` for lore_count and last_snapshot
  - [x] Query `Embedder.ModelName()` for embedding_model
  - [x] Return JSON response with `Content-Type: application/json`
- [x] Task 3: Register health route (AC: #1)
  - [x] Ensure `/api/v1/health` is registered in `internal/api/routes.go`
  - [x] Health endpoint must be OUTSIDE the auth middleware group (unauthenticated)
- [x] Task 4: Set version string (AC: #1)
  - [x] Define version as a build-time variable via `ldflags` or config constant
  - [x] Example: `var Version = "dev"` with `-ldflags "-X main.Version=1.0.0"` at build time
  - [x] Pass version to handler setup
- [x] Task 5: Write unit tests
  - [x] Test health endpoint returns 200 with correct JSON structure
  - [x] Test all snake_case field names present
  - [x] Test `last_snapshot` is null when no snapshots exist
  - [x] Test `lore_count` reflects mock store value
  - [x] Test no auth required (no Authorization header)
  - [x] Test response Content-Type is application/json

## Dev Notes

### Critical Architecture Compliance

- **NFR5**: Health endpoint must respond within 100ms. Keep the handler lightweight — one store query, no complex processing.
- **NFR8**: Health is the ONLY unauthenticated endpoint. Ensure route registration places it outside auth middleware.
- **FR11**: The health endpoint serves as machine-readable configuration discovery (embedding model, version).
- **FR26**: Must include service version, embedding model, lore count, and last snapshot time.
- **snake_case** JSON field names per architecture naming conventions.

### Health Response Example

```json
{
  "status": "healthy",
  "version": "1.0.0",
  "embedding_model": "text-embedding-3-small",
  "lore_count": 142,
  "last_snapshot": "2026-01-28T10:30:00Z"
}
```

When no snapshots exist:
```json
{
  "status": "healthy",
  "version": "1.0.0",
  "embedding_model": "text-embedding-3-small",
  "lore_count": 0,
  "last_snapshot": null
}
```

### Handler Dependencies

The health handler needs:
1. `store.Store` — for `GetStats()` → `StoreStats{LoreCount, LastSnapshot}`
2. `embedding.Embedder` — for `ModelName()` → string
3. Version string — from build-time variable or config

These are injected at handler setup time via a handler struct or closure pattern.

### Existing Code

- `internal/api/handlers.go` — EXISTS, review for existing handler patterns
- `internal/api/routes.go` — EXISTS, add health route outside auth group

### Project Structure Notes

- `internal/api/handlers.go` — EXISTS, add health handler
- `internal/api/handlers_test.go` — NEW or UPDATE, health endpoint tests
- `internal/api/routes.go` — EXISTS, register health route

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Health & Observability]
- [Source: _bmad-output/planning-artifacts/prd.md#Endpoint Specification - /api/v1/health]
- [Source: _bmad-output/planning-artifacts/prd.md#NFR Performance - Health endpoint within 100ms]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.6]

## Technical Design

### Gap Analysis: Current vs. Required

| Component | Current State | Required State | Gap |
|-----------|--------------|----------------|-----|
| `HealthResponse` type | Exists in `types.go:96-102` with `LastSnapshot string` | `LastSnapshot *time.Time` for proper null marshaling | **Type mismatch** |
| `Handler.Health()` | Exists in `handlers.go:30-42`, uses `store.Count()`, hardcoded version | Needs `store.GetStats()`, version injection | **Missing GetStats, version** |
| `Store.GetStats()` | Interface defined at `store.go:26` | Implementation missing in `sqlite.go` | **Implementation missing** |
| `StoreStats` type | Exists in `types.go:174-177` | Matches requirements | None |
| Version variable | Not defined | Build-time variable | **Missing** |
| Route registration | Correctly outside auth group at `routes.go:20` | As required | None |
| Unit tests | `handlers_test.go` does not exist | Full test coverage | **Missing** |

### Interfaces & Contracts

**HealthResponse (Update Required):**
```go
// types/types.go — update existing type
type HealthResponse struct {
    Status         string     `json:"status"`
    Version        string     `json:"version"`
    EmbeddingModel string     `json:"embedding_model"`
    LoreCount      int64      `json:"lore_count"`
    LastSnapshot   *time.Time `json:"last_snapshot"`
}
```

**Handler Dependencies (Update Required):**
```go
// api/handlers.go — update Handler struct
type Handler struct {
    store    store.Store           // Use interface, not concrete type
    embedder embedding.Embedder    // Use interface, not concrete type
    apiKey   string
    version  string                // Injected at construction
}
```

**Store.GetStats Implementation Contract:**
```go
// store/sqlite.go — new method
func (s *SQLiteStore) GetStats(ctx context.Context) (*types.StoreStats, error)
```
- Query lore count: `SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL`
- Return `nil` for `LastSnapshot` until Snapshot Management story implements tracking
- Must complete within NFR5 budget (trivial for COUNT query)

### Implementation Approach

| File | Action | Scope |
|------|--------|-------|
| `internal/types/types.go` | Edit | Fix `HealthResponse.LastSnapshot` type to `*time.Time` |
| `internal/store/sqlite.go` | Add | Implement `GetStats(ctx)` method |
| `internal/api/handlers.go` | Edit | Update `Handler` struct, `NewHandler`, `Health()` |
| `internal/api/handlers_test.go` | Create | Unit tests for health endpoint |
| `cmd/engram/main.go` or `root.go` | Edit | Define `Version` variable with ldflags support |

**Version Injection Pattern:**
```go
// cmd/engram/main.go
var Version = "dev"  // Overwritten by: -ldflags "-X main.Version=1.0.0"
```

### Test Seeds

| Test | Scenario | Expected |
|------|----------|----------|
| `TestHealth_ReturnsHealthyStatus` | Service running | 200, `status: "healthy"` |
| `TestHealth_ReturnsCorrectJSONStructure` | Any request | All 5 fields present, snake_case |
| `TestHealth_LoreCountReflectsStoreValue` | Store has 42 entries | `lore_count: 42` |
| `TestHealth_LastSnapshotNullWhenNone` | No snapshots | `last_snapshot: null` |
| `TestHealth_LastSnapshotReturnsTimestamp` | Snapshot exists | `last_snapshot: "2026-01-28T..."` |
| `TestHealth_NoAuthRequired` | No Authorization header | 200 (not 401) |
| `TestHealth_ContentTypeJSON` | Any request | `Content-Type: application/json` |
| `TestHealth_EmbeddingModelFromEmbedder` | Embedder configured | Matches `Embedder.ModelName()` |

**Edge Cases:**
- Store query fails → Return 500 (store accessibility is prerequisite)
- Empty database → `lore_count: 0`, `last_snapshot: null`

### Assumptions

1. **Snapshot tracking not yet implemented:** `GetStats` returns `nil` for `LastSnapshot` until Snapshot Management story adds tracking. Health endpoint marshals as `null`.

2. **Store interface adoption:** Handler updated to use `store.Store` interface instead of `*SQLiteStore` concrete type per architecture decisions.

3. **Version default:** If ldflags not provided, version defaults to `"dev"`.

### Design Decision

No separate tech design doc needed — story is straightforward (one endpoint, one store method, type updates, tests).

---

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5 (Spark Agent - TDD Implementation Specialist)

### Debug Log References

N/A

### Completion Notes List

1. **TDD Red-Green Cycle Complete:**
   - Wrote 10 failing tests first (RED)
   - Implemented code to pass all tests (GREEN)
   - 100% coverage on Health function

2. **Type Changes:**
   - Updated `HealthResponse.LoreCount` from `int` to `int64`
   - Updated `HealthResponse.LastSnapshot` from `string` to `*time.Time` for proper null marshaling

3. **Interface Adoption:**
   - Renamed `OpenAI.Model()` to `ModelName()` to match `Embedder` interface
   - Handler now uses `HealthStore` interface for health endpoint
   - Added `legacyStore` field for backward compatibility with other handlers

4. **Version Injection:**
   - Added `Version` variable in `cmd/engram/root.go` with ldflags support
   - Default value is `"dev"`

5. **Deviation from Technical Design:**
   - Created `HealthStore` interface instead of using full `store.Store` in Handler
   - Reason: Other handlers (IngestLore) use methods not in Store interface yet
   - Added `NewHandlerWithLegacyStore` constructor for production use
   - This allows clean test mocking while maintaining backward compatibility

### File List

| File | Action | Description |
|------|--------|-------------|
| `internal/types/types.go` | Modified | Updated `HealthResponse` type (LoreCount int64, LastSnapshot *time.Time) |
| `internal/embedding/openai.go` | Modified | Renamed `Model()` to `ModelName()` to match Embedder interface |
| `internal/store/sqlite.go` | Modified | Added `GetStats(ctx)` implementation |
| `internal/api/handlers.go` | Modified | Updated Handler to use interfaces, added version field, updated Health() |
| `internal/api/handlers_test.go` | Created | 10 unit tests for health endpoint |
| `cmd/engram/root.go` | Modified | Added Version variable with ldflags support |

### Tests Written

| Test | Acceptance Criteria |
|------|---------------------|
| `TestHealth_ReturnsHealthyStatus` | AC#1: status is "healthy" |
| `TestHealth_ReturnsCorrectJSONStructure` | AC#1: all 5 fields present, snake_case |
| `TestHealth_LoreCountReflectsStoreValue` | AC#2: lore_count reflects store value |
| `TestHealth_LastSnapshotNullWhenNone` | AC#2: last_snapshot null when no snapshots |
| `TestHealth_LastSnapshotReturnsTimestamp` | AC#2: last_snapshot returns timestamp |
| `TestHealth_NoAuthRequired` | AC#1: no authentication required |
| `TestHealth_ContentTypeJSON` | AC#1: Content-Type is application/json |
| `TestHealth_EmbeddingModelFromEmbedder` | AC#1: embedding_model from embedder |
| `TestHealth_VersionFromConfig` | AC#1: version from config |
| `TestHealth_StoreErrorReturns500` | Edge case: store error returns 500 |
