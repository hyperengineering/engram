# Universal Sync: Integration & E2E Test Plan

**Status**: Draft
**Author**: Clario (Architecture Agent)
**Date**: 2026-02-18
**Scope**: Engram server, Recall client, Tract client
**Epic**: 8 — Universal Sync Protocol

---

## 1. Test Strategy Overview

### 1.1 Architecture

Tests use **real pre-built binaries** of Engram, Recall, and Tract, orchestrated by Go test functions via `os/exec`. Curated fixture files provide realistic test data checked into the repository.

```
┌─────────────────────────────────────────────────────────────┐
│  Go test orchestrator (test/e2e/*_test.go)                  │
│                                                             │
│  ┌──────────┐   ┌───────────┐   ┌──────────┐               │
│  │ Engram   │   │ Recall    │   │ Tract    │   Pre-built   │
│  │ server   │   │ CLI       │   │ CLI      │   binaries    │
│  │ (binary) │   │ (binary)  │   │ (binary) │   via env var │
│  └────┬─────┘   └─────┬─────┘   └────┬─────┘               │
│       │  HTTP         │  HTTP        │  HTTP               │
│       └───────────────┴──────────────┘                      │
│                    localhost:PORT                            │
│                                                             │
│  test/fixtures/                                             │
│  ├── recall/          Curated lore entries                  │
│  ├── tract/           Curated entity graphs                 │
│  └── scenarios/       Multi-client test scripts             │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 Layers

```
┌─────────────────────────────────────────────────────┐
│  Layer 4: Failure & Resilience                      │  Real binaries,
│  (retry, schema drift, compaction, concurrency)     │  fault injection
├─────────────────────────────────────────────────────┤
│  Layer 3: Multi-Client E2E                          │  Real binaries,
│  (Recall↔Recall, Recall↔Tract, legacy interop)     │  curated fixtures
├─────────────────────────────────────────────────────┤
│  Layer 2: Single-Client E2E                         │  Real binary,
│  (Recall full cycle, Tract full cycle, bootstrap)   │  curated fixtures
├─────────────────────────────────────────────────────┤
│  Layer 1: Server Integration                        │  In-process,
│  (push→delta, push→snapshot, backward compat)       │  httptest (existing)
└─────────────────────────────────────────────────────┘
```

### 1.3 Principles

- **Real binaries, real data.** Layers 2-4 use actual Engram, Recall, and Tract executables with curated fixture data. No mocks for client behavior.
- **Layer 1 stays in-process.** Server-side endpoint integration tests remain fast and mock-free using the existing `httptest` + in-memory SQLite approach.
- **Curated fixtures, not generated data.** Hand-crafted JSON fixtures representing realistic lore entries, Tract entity graphs, and multi-client scenarios. Checked into the repo.
- **Skip gracefully when binaries unavailable.** Tests call `t.Skip()` if a required binary is not found. CI pipeline downloads the correct versions.
- **Isolated temp directories.** Each test gets its own temp dir for Engram data, Recall databases, and Tract databases. Cleanup via `t.Cleanup()`.

### 1.4 File Organization

```
test/
  e2e/
    main_test.go               ← TestMain: binary discovery, shared setup
    helpers.go                  ← Process management, fixture loading, DB inspection
    server_integration_test.go  ← Layer 1 (in-process, no build tag)
    recall_e2e_test.go          ← Layer 2: Single Recall client E2E
    tract_e2e_test.go           ← Layer 2: Single Tract client E2E
    multi_client_test.go        ← Layer 3: Multi-client scenarios
    resilience_test.go          ← Layer 4: Failure and edge cases
  fixtures/
    recall/
      lore-architectural-decisions.json
      lore-pattern-outcomes.json
      lore-mixed-categories.json
      lore-confidence-spectrum.json
      feedback-batch.json
    tract/
      reasoning-layer.json       ← goals, csfs, ncs, sos, capabilities
      planning-layer.json        ← fwus, boundaries, dependencies
      execution-layer.json       ← impl contexts, entity specs, test seeds
      full-entity-graph.json     ← complete FK-valid graph across all layers
    scenarios/
      conflict-resolution.json   ← two clients editing same entries
      high-volume.json           ← 500+ entries for pagination testing
      mixed-operations.json      ← interleaved upserts, deletes, feedback
```

---

## 2. Binary Management & Configuration

### 2.1 Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ENGRAM_BIN` | Layer 2-4 | Path to Engram server binary |
| `RECALL_BIN` | Recall tests | Path to Recall CLI binary |
| `TRACT_BIN` | Tract tests | Path to Tract CLI binary |
| `E2E_API_KEY` | All layers | API key for test Engram server (default: `e2e-test-key`) |
| `E2E_KEEP_LOGS` | Optional | Set to `1` to preserve temp dirs and logs after test run |

### 2.2 Binary Discovery in TestMain

```go
//go:build e2e

package e2e

import (
    "os"
    "os/exec"
    "testing"
)

var (
    engramBin string
    recallBin string
    tractBin  string
)

func TestMain(m *testing.M) {
    engramBin = os.Getenv("ENGRAM_BIN")
    if engramBin == "" {
        engramBin = lookPath("engram")
    }
    recallBin = os.Getenv("RECALL_BIN")
    if recallBin == "" {
        recallBin = lookPath("recall")
    }
    tractBin = os.Getenv("TRACT_BIN")
    if tractBin == "" {
        tractBin = lookPath("tract")
    }
    os.Exit(m.Run())
}

func lookPath(name string) string {
    p, _ := exec.LookPath(name)
    return p
}

func requireEngram(t *testing.T) {
    t.Helper()
    if engramBin == "" {
        t.Skip("ENGRAM_BIN not set and engram not in PATH")
    }
}

func requireRecall(t *testing.T) {
    t.Helper()
    requireEngram(t)
    if recallBin == "" {
        t.Skip("RECALL_BIN not set and recall not in PATH")
    }
}

func requireTract(t *testing.T) {
    t.Helper()
    requireEngram(t)
    if tractBin == "" {
        t.Skip("TRACT_BIN not set and tract not in PATH")
    }
}
```

### 2.3 Engram Server Process Management

```go
// engramServer manages an Engram server subprocess.
type engramServer struct {
    cmd      *exec.Cmd
    dataDir  string
    address  string // "localhost:{port}"
    apiKey   string
    storeDir string
    logFile  string
}

// startEngram launches the Engram server on a random available port.
// Blocks until the health endpoint responds or timeout expires.
func startEngram(t *testing.T) *engramServer

// stop gracefully shuts down the Engram server.
func (s *engramServer) stop()

// baseURL returns the full base URL (e.g., "http://localhost:51234").
func (s *engramServer) baseURL() string

// createStore creates a store via the API and returns the store ID.
func (s *engramServer) createStore(t *testing.T, name, storeType string) string

// storeDB opens the server-side SQLite database for a store (for assertions).
func (s *engramServer) storeDB(t *testing.T, storeID string) *sql.DB

// waitHealthy polls GET /api/v1/health until 200 or timeout.
func (s *engramServer) waitHealthy(timeout time.Duration) error

// generateSnapshot triggers snapshot generation for a store.
func (s *engramServer) generateSnapshot(t *testing.T, storeID string)
```

### 2.4 Recall CLI Wrapper

```go
// recallCLI wraps the Recall binary for test invocations.
type recallCLI struct {
    bin      string
    dbPath   string        // path to local Recall database
    storeID  string
    sourceID string
    env      []string      // ENGRAM_URL, ENGRAM_API_KEY, ENGRAM_STORE_ID
}

// newRecallCLI creates a Recall CLI wrapper pointed at the test Engram server.
func newRecallCLI(t *testing.T, server *engramServer, storeID string) *recallCLI

// record invokes `recall record` (or the MCP equivalent) with the given content.
func (r *recallCLI) record(t *testing.T, content, category string, confidence float64) string

// feedback invokes `recall feedback` with helpful/incorrect/not_relevant refs.
func (r *recallCLI) feedback(t *testing.T, helpful, incorrect, notRelevant []string)

// query invokes `recall query` and returns matched lore.
func (r *recallCLI) query(t *testing.T, queryStr string, k int) []loreResult

// sync invokes `recall sync` (the full push+pull cycle).
func (r *recallCLI) sync(t *testing.T) syncResult

// exec runs an arbitrary Recall CLI command and returns stdout/stderr.
func (r *recallCLI) exec(t *testing.T, args ...string) (string, string, error)

// db opens the Recall local SQLite database for direct inspection.
func (r *recallCLI) db(t *testing.T) *sql.DB

// loreCount returns the count of non-deleted local lore entries.
func (r *recallCLI) loreCount(t *testing.T) int

// loreEntry returns a specific lore entry by ID from the local DB.
func (r *recallCLI) loreEntry(t *testing.T, id string) *loreEntry
```

### 2.5 Tract CLI Wrapper

```go
// tractCLI wraps the Tract binary for test invocations.
type tractCLI struct {
    bin      string
    dbPath   string
    storeID  string
    sourceID string
    env      []string
}

// newTractCLI creates a Tract CLI wrapper pointed at the test Engram server.
func newTractCLI(t *testing.T, server *engramServer, storeID string) *tractCLI

// createGoal invokes Tract to create a goal entity.
func (tr *tractCLI) createGoal(t *testing.T, name, description string) string

// createCSF invokes Tract to create a CSF linked to a goal.
func (tr *tractCLI) createCSF(t *testing.T, goalID, name string) string

// createEpic invokes Tract to create an epic.
func (tr *tractCLI) createEpic(t *testing.T, name string) string

// sync invokes `tract sync` (push+pull cycle).
func (tr *tractCLI) sync(t *testing.T) syncResult

// exec runs an arbitrary Tract CLI command.
func (tr *tractCLI) exec(t *testing.T, args ...string) (string, string, error)

// db opens the Tract local SQLite database for direct inspection.
func (tr *tractCLI) db(t *testing.T) *sql.DB

// tableCount returns the row count of a specific table.
func (tr *tractCLI) tableCount(t *testing.T, tableName string) int
```

---

## 3. Curated Fixture Files

### 3.1 Recall Fixtures

#### `test/fixtures/recall/lore-architectural-decisions.json`

Realistic architectural decision lore entries (5 entries):

```json
[
  {
    "content": "Use sequence-based ordering for sync instead of timestamps to eliminate clock skew issues across distributed clients.",
    "context": "Engram universal sync protocol design",
    "category": "ARCHITECTURAL_DECISION",
    "confidence": 0.8,
    "sources": ["design-review-2026-02"]
  },
  {
    "content": "Separate sync_meta from store_metadata to maintain clear separation between store configuration and sync protocol state.",
    "context": "Story 8.1 change log schema design",
    "category": "ARCHITECTURAL_DECISION",
    "confidence": 0.7,
    "sources": ["clario-design-8.1"]
  },
  {
    "content": "All-or-nothing push semantics prevent partial state corruption when FK constraints span multiple entries in a batch.",
    "context": "Sync push transaction design clarification",
    "category": "ARCHITECTURAL_DECISION",
    "confidence": 0.75,
    "sources": ["clarification-5"]
  },
  {
    "content": "Domain plugins validate and optionally reorder push entries before replay, enabling tool-specific FK ordering without coupling the generic sync layer.",
    "context": "DomainPlugin interface design",
    "category": "ARCHITECTURAL_DECISION",
    "confidence": 0.7,
    "sources": ["story-8.2"]
  },
  {
    "content": "Push idempotency via client-generated push_id with 24-hour server-side cache prevents duplicate processing on retry without requiring client-side dedup tracking.",
    "context": "Push idempotency clarification",
    "category": "ARCHITECTURAL_DECISION",
    "confidence": 0.85,
    "sources": ["clarification-2"]
  }
]
```

#### `test/fixtures/recall/lore-pattern-outcomes.json`

Pattern outcome entries with varying confidence (5 entries):

```json
[
  {
    "content": "Separating new database domain operations into dedicated files (sqlite_changelog.go) rather than appending to the main implementation file improves maintainability and code review.",
    "context": "Story 8.1 feedback loop",
    "category": "PATTERN_OUTCOME",
    "confidence": 0.7,
    "sources": ["feedback-loop-8.1"]
  },
  {
    "content": "Tabular test seeds in Given/When/Then format translate directly to Go test functions with high fidelity. Developers expand naturally from seeds without design consultations.",
    "context": "Story 8.1 feedback loop",
    "category": "PATTERN_OUTCOME",
    "confidence": 0.7,
    "sources": ["feedback-loop-8.1"]
  },
  {
    "content": "Using INSERT OR REPLACE for idempotent upserts in SQLite is simpler than separate INSERT/UPDATE paths but resets any columns not in the VALUES clause.",
    "context": "SQLite upsert patterns",
    "category": "PATTERN_OUTCOME",
    "confidence": 0.6,
    "sources": ["implementation-review"]
  },
  {
    "content": "In-memory SQLite databases with MaxOpenConns=1 provide fast, isolated test environments that closely match production behavior for single-writer workloads.",
    "context": "Test infrastructure design",
    "category": "PATTERN_OUTCOME",
    "confidence": 0.8,
    "sources": ["test-infra-review"]
  },
  {
    "content": "The nullablePayload helper pattern for converting between Go types and SQL nullable values is clean and reusable across all change log operations.",
    "context": "Story 8.1 feedback loop",
    "category": "PATTERN_OUTCOME",
    "confidence": 0.6,
    "sources": ["feedback-loop-8.1"]
  }
]
```

#### `test/fixtures/recall/lore-mixed-categories.json`

Entries spanning all 8 categories for comprehensive testing (8 entries, one per category).

#### `test/fixtures/recall/lore-confidence-spectrum.json`

Entries at confidence boundary values: 0.0, 0.1, 0.5, 0.7, 0.9, 1.0.

#### `test/fixtures/recall/feedback-batch.json`

A feedback operation referencing multiple lore entries:

```json
{
  "helpful": ["L1", "L2"],
  "incorrect": ["L3"],
  "not_relevant": ["L4", "L5"]
}
```

### 3.2 Tract Fixtures

#### `test/fixtures/tract/reasoning-layer.json`

A coherent reasoning layer entity graph:

```json
{
  "goals": [
    {"id": "goal-1", "name": "Improve developer productivity", "description": "Reduce friction in daily development workflows"}
  ],
  "csfs": [
    {"id": "csf-1", "goal_id": "goal-1", "name": "Fast feedback loops", "description": "Tests and builds complete in under 60 seconds"}
  ],
  "ncs": [
    {"id": "nc-1", "csf_id": "csf-1", "name": "Incremental compilation", "description": "Only recompile changed modules"}
  ],
  "sos": [
    {"id": "so-1", "name": "Build system", "description": "Core build toolchain"}
  ],
  "capabilities": [
    {"id": "cap-1", "name": "Parallel test execution", "description": "Run test suites concurrently"}
  ]
}
```

#### `test/fixtures/tract/planning-layer.json`

FWUs linked to reasoning layer entities with proper FK references.

#### `test/fixtures/tract/execution-layer.json`

Implementation contexts and entity specs linked to planning layer.

#### `test/fixtures/tract/full-entity-graph.json`

Complete, FK-valid graph across all three layers (~50 entities total). This is the primary fixture for Tract E2E tests.

### 3.3 Scenario Fixtures

#### `test/fixtures/scenarios/conflict-resolution.json`

Two clients modifying the same lore entry with different content:

```json
{
  "description": "Two clients edit the same entry — last writer wins",
  "shared_entry_id": "conflict-entry-001",
  "client_a": {
    "content": "Event sourcing provides natural audit trails in financial systems.",
    "confidence": 0.6
  },
  "client_b": {
    "content": "Event sourcing provides natural audit trails but adds significant complexity in financial systems. Consider CQRS as a complement.",
    "confidence": 0.85
  },
  "expected_winner": "client_b",
  "reason": "client_b pushes second, higher sequence wins"
}
```

#### `test/fixtures/scenarios/high-volume.json`

500+ lore entries for pagination and performance testing.

#### `test/fixtures/scenarios/mixed-operations.json`

Interleaved sequence of records, feedback updates, and deletes.

---

## 4. Layer 1: Server Integration Tests (In-Process)

**Location**: `test/e2e/server_integration_test.go`
**Build tag**: None (runs with `go test ./test/e2e/`)
**Approach**: In-process `httptest` + real SQLiteStore. No binaries needed.

This layer is retained from the original plan — fast, in-process tests validating endpoint interplay. See sections 2.1-2.6 of the previous revision for the full test matrix. Key tests:

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestSync_PushThenDelta` | Push 5 entries → GET delta after=0 | All 5 returned with server-assigned sequences |
| `TestSync_PushThenDelta_Pagination` | Push 30 entries → paginate with limit=10 | 3 pages, no duplicates, has_more correct |
| `TestSync_PushIdempotent_NoDoubleSideEffects` | Push push_id=X twice → GET delta | Only one set of entries |
| `TestSync_LoreIngestVisibleInDelta` | POST /lore/ingest → GET /sync/delta | Legacy writes visible in sync delta |
| `TestSync_RecallPlugin_AllOrNothing` | Push 9 valid + 1 invalid | 422, accepted=0 |
| `TestSync_PushClientAhead` | Client schema=3, server=2 → push | 409 schema mismatch |

~20 tests, < 5s, no external dependencies.

---

## 5. Layer 2: Single-Client E2E Tests (Real Binaries)

**Location**: `test/e2e/recall_e2e_test.go`, `test/e2e/tract_e2e_test.go`
**Build tag**: `//go:build e2e`
**Requires**: `ENGRAM_BIN` + `RECALL_BIN` or `TRACT_BIN`

### 5.1 Recall: Record → Sync → Verify

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestRecallE2E_RecordAndSync` | Load `lore-architectural-decisions.json` → Recall records each entry → `recall sync` → query Engram delta | Server change_log has 5 entries with correct payloads |
| `TestRecallE2E_SyncPullsRemote` | Manually push 5 entries to Engram (different source_id) → `recall sync` | Recall local DB gains 5 new entries |
| `TestRecallE2E_FeedbackAndSync` | Record 5 entries → sync → feedback (helpful on 2, incorrect on 1) → sync | Confidence adjusted locally; feedback not in server change_log |
| `TestRecallE2E_DeleteAndSync` | Record entry → sync → delete entry → sync → check Engram delta | Delta shows upsert + delete for same entity_id |
| `TestRecallE2E_QueryAfterSync` | Push 10 entries from external source → `recall sync` → `recall query "architecture"` | Query returns relevant entries from synced data |
| `TestRecallE2E_BootstrapFromSnapshot` | Push 50 entries to Engram → generate snapshot → fresh Recall client bootstraps → verify | Recall local DB has 50 entries, sync_meta initialized |
| `TestRecallE2E_BootstrapThenDelta` | Bootstrap (50 entries) → push 10 more to server → `recall sync` | Recall has 60 entries total |
| `TestRecallE2E_MixedCategories` | Load `lore-mixed-categories.json` → record all 8 → sync → verify | All 8 categories present in server change_log |
| `TestRecallE2E_ConfidenceBoundaries` | Load `lore-confidence-spectrum.json` → record entries at 0.0, 0.5, 1.0 → sync | All confidence values preserved through round-trip |
| `TestRecallE2E_SourceFiltering` | Client A syncs 5 entries → Client A syncs again → inspect local DB | No duplicate entries (own entries filtered on pull) |

### 5.2 Tract: Entity Graph → Sync → Verify

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestTractE2E_ReasoningLayerSync` | Load `reasoning-layer.json` → Tract creates goals, csfs, ncs → `tract sync` | Engram change_log has entries for all reasoning tables |
| `TestTractE2E_FullEntityGraph` | Load `full-entity-graph.json` → Tract creates all entities → `tract sync` | Server has ~50 entries across all Tract tables |
| `TestTractE2E_FKOrderPreserved` | Create child before parent locally → `tract sync` | Push succeeds (plugin reorders), FK integrity maintained on server |
| `TestTractE2E_DeleteCascade` | Create goal → create csf referencing goal → sync → delete goal → sync | Delete entries in correct FK order in change_log |
| `TestTractE2E_SyncPullsRemoteEntities` | Push Tract entities from external source → `tract sync` | Tract local DB has all entities with FK relationships intact |
| `TestTractE2E_BootstrapFullGraph` | Push full entity graph → generate snapshot → fresh Tract bootstraps | Tract local DB has complete graph, all FKs valid |

---

## 6. Layer 3: Multi-Client E2E Tests (Real Binaries)

**Location**: `test/e2e/multi_client_test.go`
**Build tag**: `//go:build e2e`

### 6.1 Two Recall Clients → Convergence

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestMulti_TwoRecall_Convergence` | Load `lore-architectural-decisions.json` → Client A records 5 → sync → Client B records 5 from `lore-pattern-outcomes.json` → sync → both sync again | Both clients have 10 identical entries |
| `TestMulti_TwoRecall_InterleavedSync` | A records 2 → sync → B records 2 → sync → A sync → B sync → repeat 3 rounds | Both converge after each round |
| `TestMulti_TwoRecall_ConflictResolution` | Load `conflict-resolution.json` → A records entry X → sync → B records entry X (different content) → sync → A sync | A has B's version (last writer wins), content matches fixture expectation |
| `TestMulti_TwoRecall_DeletePropagation` | A records entry → sync → B syncs (has entry) → A deletes → sync → B syncs | B's entry is soft-deleted |
| `TestMulti_TwoRecall_BootstrapAndDelta` | A records 50 entries → sync → B bootstraps → A records 10 more → sync → B syncs via delta | B has 60 entries |

### 6.2 Three Recall Clients → Eventual Consistency

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestMulti_ThreeRecall_EventualConsistency` | A, B, C each record 10 entries → all sync → all sync again | All three have 30 identical entries |
| `TestMulti_ThreeRecall_CascadingSync` | A records → sync → B syncs → B records → sync → C syncs | C has entries from both A and B |

### 6.3 Recall + Tract: Store Isolation

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestMulti_RecallTract_StoreIsolation` | Recall pushes 5 lore to store-recall → Tract pushes entities to store-tract → both sync | Recall store has only lore, Tract store has only entities, zero cross-leakage |
| `TestMulti_RecallTract_IndependentSequences` | Push to both stores → delta on each | Independent sequence spaces (both start at 1) |
| `TestMulti_RecallTract_ConcurrentSync` | Recall and Tract sync simultaneously | Both succeed, no interference or deadlocks |

### 6.4 Legacy + New Client Interop

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestMulti_LegacyRecall_VisibleToNewClient` | Old-protocol Recall pushes via `/lore/ingest` → new Recall syncs via `/sync/delta` | New client receives all entries |
| `TestMulti_NewRecall_VisibleToLegacyClient` | New Recall pushes via `/sync/push` → old Recall pulls via `/lore/delta` | Legacy client receives entries in lore format |
| `TestMulti_MixedClients_FullConvergence` | Legacy pushes 3 + new pushes 3 → new client syncs → verify 6 entries | Full convergence across protocol versions |

---

## 7. Layer 4: Failure & Resilience Tests (Real Binaries)

**Location**: `test/e2e/resilience_test.go`
**Build tag**: `//go:build e2e`

### 7.1 Push Retry / Idempotency

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_PushRetryIdempotent` | Recall records 10 entries → first sync → kill Engram mid-response → restart Engram → Recall re-syncs | No duplicate entries on server, Recall converges |
| `TestResilience_PushRetryResponseMatch` | Recall syncs → capture server change_log count → Recall syncs same data again | Change_log count unchanged (idempotent) |

### 7.2 Server Restart During Sync

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_ServerRestart_PushRecovery` | Recall records entries → start sync → stop Engram → restart Engram → re-sync | All entries eventually on server, no corruption |
| `TestResilience_ServerRestart_PullRecovery` | Push entries to server → Recall starts pull → stop Engram → restart → Recall re-pulls | Recall has all entries, no gaps |

### 7.3 Schema Mismatch

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_SchemaMismatch_GracefulHalt` | Configure client schema ahead of server → Recall sync | Sync fails gracefully with clear error message, no data corruption |

### 7.4 Concurrent Clients

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_ConcurrentRecallClients` | 5 Recall instances each record 20 entries → all sync simultaneously | Server has 100 entries, no gaps, no duplicates, all clients converge |
| `TestResilience_ConcurrentRecallTract` | Recall and Tract sync simultaneously to different stores | Both succeed independently, no cross-store interference |

### 7.5 High Volume

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_HighVolume_500Entries` | Load `high-volume.json` → Recall records 500+ entries → sync → second client pulls | All entries transferred, pagination works correctly |
| `TestResilience_HighVolume_ManySmallSyncs` | Recall records 10 entries → sync → repeat 50 times | All 500 entries on server, monotonic sequences |

### 7.6 Compaction During Active Sync

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_CompactionDuringPull` | Push 200 entries → Client A starts pulling → trigger compaction → Client A continues | Client A gets all non-compacted entries, no errors |
| `TestResilience_CompactionPreservesRecent` | Push entries → wait → push more entries → compact → delta from 0 | Recent entries preserved, old entries compacted, audit file written |

### 7.7 Bootstrap Edge Cases

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_BootstrapEmpty` | Create empty store → Recall bootstraps | Recall initializes with empty DB, sync_meta correct |
| `TestResilience_BootstrapThenImmediateRecord` | Bootstrap → immediately record + sync | Entry arrives on server |
| `TestResilience_BootstrapIntegrityCheck` | Bootstrap → open local DB → PRAGMA integrity_check | Returns "ok" |

### 7.8 Mixed Operations Stress

| Test | Steps | Assertion |
|------|-------|-----------|
| `TestResilience_MixedOps` | Load `mixed-operations.json` → execute interleaved records, feedback, deletes → sync → verify | Server state matches expected final state from fixture |

---

## 8. Database Inspection Helpers

Tests verify outcomes by **directly inspecting SQLite databases** — both the Engram server's store databases and the clients' local databases.

```go
// serverLoreCount returns lore entry count in a server store.
func serverLoreCount(t *testing.T, server *engramServer, storeID string) int

// serverChangeLogEntries reads change_log entries from a server store.
func serverChangeLogEntries(t *testing.T, server *engramServer, storeID string) []changeLogRow

// clientLoreEntries reads all non-deleted lore entries from a Recall client DB.
func clientLoreEntries(t *testing.T, dbPath string) []loreRow

// clientChangeLogCount returns the change_log count in a client DB.
func clientChangeLogCount(t *testing.T, dbPath string) int

// clientSyncMeta reads a sync_meta value from a client DB.
func clientSyncMeta(t *testing.T, dbPath string, key string) string

// tractTableRows reads all rows from a Tract table.
func tractTableRows(t *testing.T, dbPath string, tableName string) []map[string]interface{}

// assertLoreSetsEqual compares two sets of lore entries by ID and content.
func assertLoreSetsEqual(t *testing.T, expected, actual []loreRow)

// assertFKIntegrity runs FK integrity checks on a Tract database.
func assertFKIntegrity(t *testing.T, dbPath string)
```

---

## 9. Fixture Loading Helpers

```go
// loadRecallFixture loads a Recall fixture file and returns parsed entries.
func loadRecallFixture(t *testing.T, name string) []recallFixtureEntry

// loadTractFixture loads a Tract fixture file and returns entity graph.
func loadTractFixture(t *testing.T, name string) tractFixtureGraph

// loadScenario loads a multi-client scenario fixture.
func loadScenario(t *testing.T, name string) scenarioFixture

// fixturesDir returns the absolute path to test/fixtures/.
func fixturesDir() string
```

---

## 10. Test Matrix Summary

### By Layer

| Layer | Test Count | Build Tag | Binaries Required | Run Time Target |
|-------|-----------|-----------|-------------------|-----------------|
| Layer 1: Server Integration | ~20 | None | None | < 5s |
| Layer 2: Single-Client E2E | ~16 | `e2e` | Engram + (Recall or Tract) | < 60s |
| Layer 3: Multi-Client E2E | ~13 | `e2e` | Engram + Recall + Tract | < 90s |
| Layer 4: Failure & Resilience | ~15 | `e2e` | Engram + Recall (+ Tract) | < 120s |
| **Total** | **~64** | | | **< ~5min** |

### By Binary Requirement

| Binary | Required For | Skip Behavior |
|--------|-------------|---------------|
| None | Layer 1 (in-process) | Always runs |
| Engram only | — | — |
| Engram + Recall | Layer 2 Recall, Layer 3 Recall pairs, Layer 4 | Skip if `RECALL_BIN` not set |
| Engram + Tract | Layer 2 Tract | Skip if `TRACT_BIN` not set |
| Engram + Recall + Tract | Layer 3 isolation tests | Skip if either missing |

---

## 11. CI/CD Integration

### 11.1 Makefile Additions

```makefile
# Download pre-built binaries for E2E tests
e2e-setup:
	@mkdir -p bin/e2e
	@echo "Downloading Recall binary..."
	gh release download --repo hyperengineering/recall -p "recall_linux_amd64.tar.gz" -D bin/e2e
	tar -xzf bin/e2e/recall_linux_amd64.tar.gz -C bin/e2e
	@echo "Downloading Tract binary..."
	gh release download --repo hyperengineering/tract -p "tract_linux_amd64.tar.gz" -D bin/e2e
	tar -xzf bin/e2e/tract_linux_amd64.tar.gz -C bin/e2e

# Run E2E tests with real binaries
test-e2e: build e2e-setup
	ENGRAM_BIN=./dist/engram \
	RECALL_BIN=./bin/e2e/recall \
	TRACT_BIN=./bin/e2e/tract \
	go test -v -tags=e2e ./test/e2e/ -timeout 300s

# Run E2E tests (Recall only — no Tract binary needed)
test-e2e-recall: build
	ENGRAM_BIN=./dist/engram \
	RECALL_BIN=./bin/e2e/recall \
	go test -v -tags=e2e -run "Recall|Multi_TwoRecall|Resilience" ./test/e2e/ -timeout 300s

# Run server integration only (no binaries)
test-server-integration:
	go test -v ./test/e2e/ -run "TestSync_" -timeout 30s
```

### 11.2 GitHub Actions: E2E Job

```yaml
  e2e:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - name: Build Engram
        run: go build -o bin/engram ./cmd/engram

      - name: Download Recall binary
        run: |
          gh release download --repo hyperengineering/recall \
            -p "recall_linux_amd64.tar.gz" -D bin/
          tar -xzf bin/recall_linux_amd64.tar.gz -C bin/
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Download Tract binary
        run: |
          gh release download --repo hyperengineering/tract \
            -p "tract_linux_amd64.tar.gz" -D bin/
          tar -xzf bin/tract_linux_amd64.tar.gz -C bin/
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        continue-on-error: true  # Tract may not have a release yet

      - name: Run E2E tests
        run: |
          go test -v -tags=e2e ./test/e2e/ -timeout 300s
        env:
          ENGRAM_BIN: ./bin/engram
          RECALL_BIN: ./bin/recall
          TRACT_BIN: ./bin/tract
          E2E_API_KEY: e2e-ci-test-key

      - name: Upload test logs on failure
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: e2e-logs
          path: /tmp/engram-e2e-*/
          retention-days: 7
```

### 11.3 Test Naming Convention

| Prefix | Layer | Scope |
|--------|-------|-------|
| `TestSync_` | 1 | Server endpoint integration (in-process) |
| `TestRecallE2E_` | 2 | Single Recall client with real binary |
| `TestTractE2E_` | 2 | Single Tract client with real binary |
| `TestMulti_` | 3 | Multi-client E2E scenarios |
| `TestResilience_` | 4 | Failure, recovery, and stress tests |

---

## 12. Implementation Priority

### Phase 1: Ship with Epic 8

- Layer 1 in-process tests (no binaries needed)
- Test infrastructure: `test/e2e/`, helpers, fixture loading
- Curated fixtures: all Recall fixtures, scenario fixtures
- Makefile targets and CI pipeline skeleton

### Phase 2: After Recall Client Ships

- Layer 2 Recall E2E tests (real Recall binary)
- Layer 3 multi-Recall tests (convergence, conflict, interop)
- Layer 4 resilience tests (retry, server restart, high volume)
- CI pipeline downloads Recall binary from releases

### Phase 3: After Tract Client Ships

- Layer 2 Tract E2E tests (real Tract binary)
- Layer 3 Recall+Tract isolation tests
- Tract curated fixtures
- CI pipeline downloads both binaries

---

## 13. Acceptance Criteria

The E2E test suite is complete when:

- [ ] Layer 1 passes on every CI run with no external dependencies
- [ ] Two real Recall binaries can record, sync, and converge to identical state through a real Engram server
- [ ] A real Tract binary can create an FK-constrained entity graph, sync it, and another Tract client can pull it with FKs intact
- [ ] Legacy Recall (`/lore/*`) and new Recall (`/sync/*`) interoperate — entries visible across both protocols
- [ ] Recall and Tract on separate stores show complete data isolation
- [ ] Push retry after server interruption produces no duplicate entries
- [ ] 500+ entries sync correctly with pagination
- [ ] All curated fixtures round-trip without data loss or corruption
- [ ] CI pipeline runs full E2E suite in under 5 minutes
- [ ] Tests skip gracefully when optional binaries are unavailable

---

*The path is clear. Build well.*
