# Story 1.1: Define Core Interfaces and Domain Types

Status: complete

## Story

As a **developer implementing Engram**,
I want well-defined Store and Embedder interface contracts with shared domain types,
so that all components can be implemented against stable contracts with clear boundaries.

## Acceptance Criteria

1. **Given** the architecture defines Store and Embedder interfaces
   **When** the interfaces package is implemented
   **Then** `internal/store/store.go` defines the `Store` interface with all methods from the architecture document

2. **Given** the architecture defines an Embedder interface
   **When** the embedding package is implemented
   **Then** `internal/embedding/embedder.go` defines the `Embedder` interface with `Embed`, `EmbedBatch`, and `ModelName` methods

3. **Given** the architecture defines shared domain types
   **When** the types package is implemented
   **Then** `internal/types/types.go` defines `LoreEntry`, `NewLoreEntry`, `IngestResult`, `DeltaResult`, `FeedbackEntry`, `FeedbackResult`, `StoreMetadata`, `StoreStats` types

4. **Given** the architecture requires domain error types
   **When** the store errors package is implemented
   **Then** `internal/store/errors.go` defines sentinel errors: `ErrNotFound`, `ErrDuplicateLore`, `ErrEmbeddingUnavailable`, `ErrEmbeddingPending`

5. **Given** all types are defined
   **When** JSON serialization is used
   **Then** all types use `snake_case` JSON struct tags

6. **Given** all types are defined
   **When** timestamps are serialized
   **Then** all timestamp fields use `time.Time` (RFC 3339 serialization)

7. **Given** all types are defined
   **When** IDs are used
   **Then** all ID fields use ULID string representation

## Tasks / Subtasks

- [ ] Task 1: Define Store interface in `internal/store/store.go` (AC: #1)
  - [ ] Create new file `internal/store/store.go` (existing code is in `sqlite.go`)
  - [ ] Define `Store` interface with all methods from architecture contract
  - [ ] Import `io` for `io.ReadCloser` return type on `GetSnapshot`
  - [ ] Import `time` for `time.Time` parameters
  - [ ] Import `internal/types` for domain types
- [ ] Task 2: Define Embedder interface in `internal/embedding/embedder.go` (AC: #2)
  - [ ] Create new file `internal/embedding/embedder.go`
  - [ ] Define `Embedder` interface with `Embed`, `EmbedBatch`, `ModelName`
  - [ ] Existing `openai.go` will need to implement this interface (but do NOT modify it in this story)
- [ ] Task 3: Define domain types in `internal/types/types.go` (AC: #3, #5, #6, #7)
  - [ ] Review existing `internal/types/types.go` for any types already defined
  - [ ] Define or update `LoreEntry` with all fields: ID (ULID string), Content, Context, Category, Confidence, Embedding, SourceID, Sources, ValidationCount, CreatedAt, UpdatedAt, DeletedAt, LastValidatedAt, EmbeddingStatus
  - [ ] Define `NewLoreEntry` (input type without generated fields)
  - [ ] Define `IngestResult` with Accepted, Merged, Rejected, Errors fields
  - [ ] Define `DeltaResult` with Lore, DeletedIDs, AsOf fields
  - [ ] Define `FeedbackEntry` with LoreID, Type (helpful/not_relevant/incorrect), SourceID
  - [ ] Define `FeedbackResult` with Updates array (LoreID, PreviousConfidence, CurrentConfidence)
  - [ ] Define `StoreMetadata` with SchemaVersion, EmbeddingModel fields
  - [ ] Define `StoreStats` with LoreCount, LastSnapshot fields
  - [ ] Ensure all JSON struct tags use `snake_case`
  - [ ] Ensure all timestamp fields are `time.Time`
  - [ ] Ensure empty arrays marshal as `[]` not `null` (use pointer or custom marshaling)
- [ ] Task 4: Define sentinel errors in `internal/store/errors.go` (AC: #4)
  - [ ] Create new file `internal/store/errors.go`
  - [ ] Define `ErrNotFound` using `errors.New`
  - [ ] Define `ErrDuplicateLore` using `errors.New`
  - [ ] Define `ErrEmbeddingUnavailable` using `errors.New`
  - [ ] Define `ErrEmbeddingPending` using `errors.New`
- [ ] Task 5: Verify compilation
  - [ ] Run `go build ./...` to confirm all packages compile
  - [ ] Run `go vet ./...` to check for issues

## Dev Notes

### Critical Architecture Compliance

- **Store interface** is the foundation for ALL subsequent stories. Every handler and worker depends on this interface. Get the method signatures exactly right per the architecture document.
- **Embedder interface** is consumed by the ingest handler (Story 2.3) and the embedding retry worker (Story 2.4). Keep it minimal.
- **Domain types** are shared across all packages. They must be in `internal/types/` to avoid circular imports.
- **Sentinel errors** are used by handlers to map domain errors to RFC 7807 responses (Story 1.5).

### Store Interface Contract (from Architecture)

```go
type Store interface {
    IngestLore(ctx context.Context, entries []NewLoreEntry) (*IngestResult, error)
    FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]LoreEntry, error)
    MergeLore(ctx context.Context, targetID string, source NewLoreEntry) error
    GetLore(ctx context.Context, id string) (*LoreEntry, error)
    GetMetadata(ctx context.Context) (*StoreMetadata, error)
    GetSnapshot(ctx context.Context) (io.ReadCloser, error)
    GetDelta(ctx context.Context, since time.Time) (*DeltaResult, error)
    GenerateSnapshot(ctx context.Context) error
    GetSnapshotPath(ctx context.Context) (string, error)
    RecordFeedback(ctx context.Context, feedback []FeedbackEntry) (*FeedbackResult, error)
    DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
    GetPendingEmbeddings(ctx context.Context, limit int) ([]LoreEntry, error)
    UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
    GetStats(ctx context.Context) (*StoreStats, error)
    Close() error
}
```

### Embedder Interface Contract (from Architecture)

```go
type Embedder interface {
    Embed(ctx context.Context, content string) ([]float32, error)
    EmbedBatch(ctx context.Context, contents []string) ([][]float32, error)
    ModelName() string
}
```

### Existing Code Awareness

The following files already exist and may need to be reviewed/updated:
- `internal/types/types.go` — may have partial type definitions
- `internal/store/sqlite.go` — existing SQLite implementation (will implement Store interface in Story 2.1)
- `internal/embedding/openai.go` — existing OpenAI client (will implement Embedder interface in Story 2.2)

**Do NOT modify `sqlite.go` or `openai.go` in this story.** Only define the interfaces and types. Implementation conformance is for Stories 2.1 and 2.2.

### Naming Conventions (from Architecture)

- Exported identifiers: `PascalCase` (e.g., `LoreEntry`, `NewStore`)
- JSON struct tags: `snake_case` (e.g., `json:"source_id"`)
- Files: `snake_case.go`
- Packages: short, lowercase, no underscores

### Project Structure Notes

- `internal/store/store.go` — NEW file for Store interface (separate from `sqlite.go` implementation)
- `internal/embedding/embedder.go` — NEW file for Embedder interface (separate from `openai.go` implementation)
- `internal/types/types.go` — EXISTS, review and update with all required types
- `internal/store/errors.go` — NEW file for sentinel domain errors

### References

- [Source: _bmad-output/planning-artifacts/architecture.md#Interface Contracts]
- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions]
- [Source: _bmad-output/planning-artifacts/architecture.md#Naming Patterns]
- [Source: _bmad-output/planning-artifacts/prd.md#Data Schemas]
- [Source: _bmad-output/planning-artifacts/epics.md#Story 1.1]

## Technical Design

### Gap Analysis: Current State vs. Required

**`internal/store/store.go`** — Does not exist.
- Current `sqlite.go` defines a concrete `Store` struct (not an interface)
- Architecture requires a `Store` **interface** with 15 methods
- **Naming conflict**: the concrete struct and required interface share the name `Store` in the same package

**`internal/embedding/embedder.go`** — Does not exist.
- Current `openai.go` has `OpenAI` struct with `Embed`, `EmbedBatch`, `Model` methods
- Architecture requires `Embedder` interface with `Embed`, `EmbedBatch`, `ModelName`
- No naming conflict (different names), but `Model()` vs `ModelName()` signature mismatch exists — that's a Story 2.2 concern

**`internal/types/types.go`** — Exists with partial/misaligned types:

| Current Type | Required Type | Gaps |
|---|---|---|
| `Lore` | `LoreEntry` | Renamed. Missing `DeletedAt`, `EmbeddingStatus`. `Embedding` should be `[]float32` not `[]byte`. `LastValidated` → `LastValidatedAt`. |
| `IngestResponse` | `IngestResult` | Renamed. Semantically similar but naming must align. |
| — | `NewLoreEntry` | Missing entirely. Input type without generated fields. |
| `DeltaResponse` | `DeltaResult` | Renamed. `AsOf` should be `time.Time` not `string`. |
| `FeedbackItem` | `FeedbackEntry` | Renamed. Field names differ: `ID`→`LoreID`, `Outcome`→`Type`, missing `SourceID`. |
| `FeedbackResponse` | `FeedbackResult` | Renamed. Inner type fields differ. |
| — | `StoreMetadata` | Missing entirely. |
| — | `StoreStats` | Missing entirely. |
| `IngestRequest`, `FeedbackRequest`, `HealthResponse` | — | API-layer types. Not part of domain contract. Keep or move later. |

**`internal/store/errors.go`** — Does not exist. All four sentinel errors missing.

### Design Decisions

**Decision 1: Store naming conflict resolution**

The concrete `Store` struct in `sqlite.go` must be renamed to `SQLiteStore` so the interface can claim the name `Store`. The story says "Do NOT modify sqlite.go," but this creates an uncompilable package.

**Resolution: Rename struct in `sqlite.go` to `SQLiteStore`.** A single-line rename (`type SQLiteStore struct`) plus updating `New()` return type is the minimal change. The constructor `New` should also be renamed to `NewSQLiteStore` to follow architecture patterns. This keeps `go build ./...` passing per Task 5.

**Decision 2: Existing type evolution strategy**

The existing types (`Lore`, `IngestResponse`, etc.) are used by `sqlite.go` and potentially other code. Define the new architecture-aligned types (`LoreEntry`, `NewLoreEntry`, `IngestResult`, etc.) alongside the existing types for now. The old types can be removed when `sqlite.go` is rewritten in Story 2.1.

**Decision 3: Embedding field in LoreEntry**

The `Store` interface methods pass embeddings as `[]float32`. The domain type `LoreEntry` uses `[]float32` for the `Embedding` field — the store layer handles byte packing/unpacking internally.

**Decision 4: Empty array marshaling**

Architecture requires empty arrays marshal as `[]` not `null`. For slice fields like `Sources` and `Errors`, use non-pointer slices. Constructors and factory functions must initialize slices to empty (not nil).

**Decision 5: Category field type**

`LoreEntry.Category` is `string` to match the `FindSimilar` interface contract. The existing `LoreCategory` enum type can coexist. Validation of category values belongs in the validation layer (Story 1.5), not the domain type.

### Interfaces

**Store Interface** (`internal/store/store.go`):

```go
package store

import (
    "context"
    "io"
    "time"

    "github.com/hyperengineering/engram/internal/types"
)

type Store interface {
    IngestLore(ctx context.Context, entries []types.NewLoreEntry) (*types.IngestResult, error)
    FindSimilar(ctx context.Context, embedding []float32, category string, threshold float64) ([]types.LoreEntry, error)
    MergeLore(ctx context.Context, targetID string, source types.NewLoreEntry) error
    GetLore(ctx context.Context, id string) (*types.LoreEntry, error)
    GetMetadata(ctx context.Context) (*types.StoreMetadata, error)
    GetSnapshot(ctx context.Context) (io.ReadCloser, error)
    GetDelta(ctx context.Context, since time.Time) (*types.DeltaResult, error)
    GenerateSnapshot(ctx context.Context) error
    GetSnapshotPath(ctx context.Context) (string, error)
    RecordFeedback(ctx context.Context, feedback []types.FeedbackEntry) (*types.FeedbackResult, error)
    DecayConfidence(ctx context.Context, threshold time.Time, amount float64) (int64, error)
    GetPendingEmbeddings(ctx context.Context, limit int) ([]types.LoreEntry, error)
    UpdateEmbedding(ctx context.Context, id string, embedding []float32) error
    GetStats(ctx context.Context) (*types.StoreStats, error)
    Close() error
}
```

**Embedder Interface** (`internal/embedding/embedder.go`):

```go
package embedding

import "context"

type Embedder interface {
    Embed(ctx context.Context, content string) ([]float32, error)
    EmbedBatch(ctx context.Context, contents []string) ([][]float32, error)
    ModelName() string
}
```

### Domain Types

New types added to `internal/types/types.go` (alongside existing types until Story 2.1):

```go
type LoreEntry struct {
    ID              string     `json:"id"`
    Content         string     `json:"content"`
    Context         string     `json:"context,omitempty"`
    Category        string     `json:"category"`
    Confidence      float64    `json:"confidence"`
    Embedding       []float32  `json:"embedding,omitempty"`
    SourceID        string     `json:"source_id"`
    Sources         []string   `json:"sources"`
    ValidationCount int        `json:"validation_count"`
    CreatedAt       time.Time  `json:"created_at"`
    UpdatedAt       time.Time  `json:"updated_at"`
    DeletedAt       *time.Time `json:"deleted_at,omitempty"`
    LastValidatedAt *time.Time `json:"last_validated_at,omitempty"`
    EmbeddingStatus string     `json:"embedding_status"`
}

type NewLoreEntry struct {
    Content    string  `json:"content"`
    Context    string  `json:"context,omitempty"`
    Category   string  `json:"category"`
    Confidence float64 `json:"confidence"`
    SourceID   string  `json:"source_id"`
}

type IngestResult struct {
    Accepted int      `json:"accepted"`
    Merged   int      `json:"merged"`
    Rejected int      `json:"rejected"`
    Errors   []string `json:"errors"`
}

type DeltaResult struct {
    Lore       []LoreEntry `json:"lore"`
    DeletedIDs []string    `json:"deleted_ids"`
    AsOf       time.Time   `json:"as_of"`
}

type FeedbackEntry struct {
    LoreID   string `json:"lore_id"`
    Type     string `json:"type"`
    SourceID string `json:"source_id"`
}

type FeedbackResult struct {
    Updates []FeedbackResultUpdate `json:"updates"`
}

type FeedbackResultUpdate struct {
    LoreID             string  `json:"lore_id"`
    PreviousConfidence float64 `json:"previous_confidence"`
    CurrentConfidence  float64 `json:"current_confidence"`
}

type StoreMetadata struct {
    SchemaVersion  string `json:"schema_version"`
    EmbeddingModel string `json:"embedding_model"`
}

type StoreStats struct {
    LoreCount    int        `json:"lore_count"`
    LastSnapshot *time.Time `json:"last_snapshot,omitempty"`
}
```

### Sentinel Errors

`internal/store/errors.go`:

```go
package store

import "errors"

var (
    ErrNotFound             = errors.New("lore entry not found")
    ErrDuplicateLore        = errors.New("duplicate lore entry")
    ErrEmbeddingUnavailable = errors.New("embedding service unavailable")
    ErrEmbeddingPending     = errors.New("embedding generation pending")
)
```

### Test Seeds

| Scenario | Type | Description |
|---|---|---|
| Store interface compilation | Contract | Any type implementing all 15 methods satisfies `Store` |
| Embedder interface compilation | Contract | Any type implementing `Embed`, `EmbedBatch`, `ModelName` satisfies `Embedder` |
| LoreEntry JSON round-trip | Behavioral | Marshal/unmarshal preserves all fields with snake_case keys |
| LoreEntry empty Sources | Behavioral | `Sources: []string{}` marshals as `[]` not `null` |
| DeltaResult empty arrays | Behavioral | `Lore` and `DeletedIDs` marshal as `[]` when empty |
| IngestResult empty Errors | Behavioral | `Errors` marshals as `[]` when empty |
| Timestamp RFC 3339 | Behavioral | `CreatedAt` marshals to RFC 3339 string |
| Sentinel error identity | Behavioral | `errors.Is(err, ErrNotFound)` returns true for wrapped errors |
| FeedbackEntry fields | Contract | JSON tags produce `lore_id`, `type`, `source_id` |

### Assumptions

- Renaming the concrete `Store` struct in `sqlite.go` to `SQLiteStore` is acceptable as a minimal prerequisite for compilation, despite the story's "do not modify sqlite.go" guidance. This is a type rename only — no logic changes. **PO validation recommended.**

### Impact Assessment

- **No downstream breakage** if new types are added alongside existing ones
- `sqlite.go` struct rename (`Store` → `SQLiteStore`) is the only modification to existing code
- Stories 2.1 and 2.2 will make `SQLiteStore` and `OpenAI` implement the new interfaces
- All handler stories (1.5, 1.6, Epic 2+) depend on these types and interfaces being defined first

### Sign-Off

The path is clear. Build well.

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
