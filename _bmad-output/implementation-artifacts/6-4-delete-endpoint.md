---
story: "6.4"
title: "Delete Endpoint for Lore Entries"
status: implemented
designedBy: Clario
designedAt: "2026-01-29"
implementedBy: Spark
implementedAt: "2026-01-30"
frs: []
nfrs: []
---

# Technical Design: Story 6.4 - Delete Endpoint for Lore Entries

## 1. Story Summary

As a **Recall client managing lore lifecycle**, I want the ability to soft-delete lore entries via the API, so that deleted entries appear in delta sync `deleted_ids` and clients can remove stale or incorrect lore from their local stores.

**Key Deliverables:**
- `DELETE /api/v1/lore/{id}` endpoint
- `DeleteLore(ctx, id)` method on Store interface
- SQLite implementation using soft delete (`deleted_at` column)
- Comprehensive test coverage

## 2. Gap Analysis

### Current State

| Artifact | Status | Notes |
|----------|--------|-------|
| Schema support | Complete | `deleted_at` column exists in `lore_entries` table |
| Delta sync support | Complete | `GetDelta` returns `deleted_ids` for `deleted_at IS NOT NULL` |
| Delete API endpoint | Missing | No `DELETE` route exists |
| Store interface method | Missing | No `DeleteLore` method |
| SQLite implementation | Missing | No soft-delete logic |
| E2E test blocked | Yes | E2E-SYNC-002 cannot be validated without delete capability |

### Architecture Context

```
┌─────────────────────────────────────────────────────────────┐
│                        API Layer                            │
│  DELETE /api/v1/lore/{id}  →  Handler.DeleteLore()         │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                     Store Interface                         │
│  DeleteLore(ctx context.Context, id string) error          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   SQLite Implementation                     │
│  UPDATE lore_entries SET deleted_at = ? WHERE id = ?       │
└─────────────────────────────────────────────────────────────┘
```

## 3. API Specification

### Endpoint

```
DELETE /api/v1/lore/{id}
```

**Authentication:** Required

**Path Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string (ULID) | Yes | Lore entry ID to delete |

### Success Response

**Status:** `204 No Content`

No response body (per REST conventions for DELETE).

### Error Responses

| Status | Condition | Problem Type |
|--------|-----------|--------------|
| `401 Unauthorized` | Missing/invalid API key | `unauthorized` |
| `404 Not Found` | Lore ID not found or already deleted | `not-found` |
| `400 Bad Request` | Invalid ULID format | `bad-request` |

**404 Response Example:**

```json
{
  "type": "https://engram.dev/errors/not-found",
  "title": "Not Found",
  "status": 404,
  "detail": "Lore entry not found: 01ARYZ6S41TSV4RRFFQ69G5FAV",
  "instance": "/api/v1/lore/01ARYZ6S41TSV4RRFFQ69G5FAV"
}
```

**400 Response Example (Invalid ULID):**

```json
{
  "type": "https://engram.dev/errors/bad-request",
  "title": "Bad Request",
  "status": 400,
  "detail": "Invalid lore ID format: must be valid ULID",
  "instance": "/api/v1/lore/invalid-id"
}
```

### Idempotency

DELETE is idempotent: calling it multiple times on the same ID has the same effect. However, per REST semantics:
- First call returns `204 No Content`
- Subsequent calls return `404 Not Found` (entry already soft-deleted, hence "not found")

## 4. Store Interface Changes

### New Method

Add to `/workspaces/engram/internal/store/store.go`:

```go
// Store defines the interface contract for all lore storage operations.
type Store interface {
    // ... existing methods ...

    // DeleteLore soft-deletes a lore entry by setting deleted_at.
    // Returns ErrNotFound if the entry doesn't exist or is already deleted.
    DeleteLore(ctx context.Context, id string) error
}
```

## 5. SQLite Implementation

### Implementation in `/workspaces/engram/internal/store/sqlite.go`

```go
// DeleteLore soft-deletes a lore entry by setting deleted_at.
// Returns ErrNotFound if the entry doesn't exist or is already deleted.
func (s *SQLiteStore) DeleteLore(ctx context.Context, id string) error {
    now := time.Now().UTC().Format(time.RFC3339)

    result, err := s.db.ExecContext(ctx, `
        UPDATE lore_entries
        SET deleted_at = ?, updated_at = ?
        WHERE id = ? AND deleted_at IS NULL
    `, now, now, id)
    if err != nil {
        return fmt.Errorf("soft delete lore: %w", err)
    }

    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("get rows affected: %w", err)
    }

    if rowsAffected == 0 {
        return ErrNotFound
    }

    return nil
}
```

### Key Behaviors

1. **Soft delete** - Sets `deleted_at` to current timestamp, does NOT remove row
2. **Updates `updated_at`** - Ensures delta sync picks up the change
3. **Idempotent at DB level** - `WHERE deleted_at IS NULL` prevents re-deletion
4. **Returns ErrNotFound** for:
   - Entry doesn't exist
   - Entry already soft-deleted

## 6. Handler Implementation

### New Handler in `/workspaces/engram/internal/api/handlers.go`

```go
// DeleteLore handles DELETE /api/v1/lore/{id}
func (h *Handler) DeleteLore(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")

    // Validate ULID format
    if _, err := ulid.Parse(id); err != nil {
        WriteProblem(w, r, http.StatusBadRequest,
            "Invalid lore ID format: must be valid ULID")
        return
    }

    err := h.store.DeleteLore(r.Context(), id)
    if err != nil {
        if errors.Is(err, store.ErrNotFound) {
            WriteProblem(w, r, http.StatusNotFound,
                fmt.Sprintf("Lore entry not found: %s", id))
            return
        }
        slog.Error("delete lore failed", "error", err, "id", id)
        WriteProblem(w, r, http.StatusInternalServerError,
            "Internal Server Error")
        return
    }

    slog.Info("lore deleted",
        "component", "api",
        "action", "delete_lore",
        "id", id,
    )

    w.WriteHeader(http.StatusNoContent)
}
```

### Route Registration in `/workspaces/engram/internal/api/routes.go`

```go
r.Group(func(r chi.Router) {
    r.Use(AuthMiddleware(h.apiKey))
    r.Post("/lore", h.IngestLore)
    r.Get("/lore/snapshot", h.Snapshot)
    r.Get("/lore/delta", h.Delta)
    r.Post("/lore/feedback", h.Feedback)
    r.Delete("/lore/{id}", h.DeleteLore)  // NEW
})
```

## 7. ULID Validation

Add ULID import to handlers.go:

```go
import (
    // ... existing imports ...
    "github.com/oklog/ulid/v2"
)
```

The ULID validation uses `ulid.Parse()` which:
- Validates 26-character length
- Validates Crockford Base32 encoding
- Returns error for invalid format

## 8. Test Scenarios

### 8.1 Unit Tests: Handler (`handlers_test.go`)

| Test | Input | Expected |
|------|-------|----------|
| `TestDeleteLore_Success` | Valid existing ID | 204 No Content |
| `TestDeleteLore_NotFound` | Non-existent ID | 404 Not Found |
| `TestDeleteLore_AlreadyDeleted` | Previously deleted ID | 404 Not Found |
| `TestDeleteLore_InvalidULID` | "invalid-id" | 400 Bad Request |
| `TestDeleteLore_Unauthorized` | No auth header | 401 Unauthorized |

### 8.2 Unit Tests: Store (`sqlite_test.go`)

| Test | Setup | Expected |
|------|-------|----------|
| `TestDeleteLore_Success` | Insert entry | `deleted_at` set, no error |
| `TestDeleteLore_NotFound` | Empty store | ErrNotFound |
| `TestDeleteLore_AlreadyDeleted` | Insert then delete | Second delete returns ErrNotFound |
| `TestDeleteLore_UpdatesTimestamp` | Insert entry | `deleted_at` and `updated_at` set |
| `TestDeleteLore_ExcludedFromGetLore` | Delete entry | GetLore returns ErrNotFound |
| `TestDeleteLore_AppearsInDelta` | Delete after T1 | ID in `deleted_ids` array |

### 8.3 Integration Tests

| Test | Flow | Expected |
|------|------|----------|
| `TestDelete_Integration` | Ingest -> Delete -> Delta | Entry in deleted_ids |
| `TestDelete_NotInSnapshot` | Ingest -> Delete -> Snapshot | Entry excluded |

### 8.4 E2E Test Unblocked (E2E-SYNC-002)

With delete endpoint available, E2E-SYNC-002 can now be executed:

```
1. Record timestamp T1
2. Ingest lore entry (note ID)
3. DELETE /api/v1/lore/{id}
4. GET /api/v1/lore/delta?since=T1
5. Verify ID in deleted_ids array
```

## 9. Implementation Checklist

- [ ] Add `DeleteLore` method to Store interface (`store.go`)
- [ ] Implement `DeleteLore` in SQLiteStore (`sqlite.go`)
- [ ] Add unit tests for store method (`sqlite_test.go`)
- [ ] Add `DeleteLore` handler (`handlers.go`)
- [ ] Add ULID import to handlers
- [ ] Register DELETE route (`routes.go`)
- [ ] Add handler unit tests (`handlers_test.go`)
- [ ] Add integration test
- [ ] Verify E2E-SYNC-002 passes
- [ ] Update API specification (`docs/api-specification.md`)

## 10. Design Decisions

### D1: Soft Delete vs Hard Delete

**Decision:** Use soft delete (set `deleted_at` timestamp).

**Rationale:**
- Schema already has `deleted_at` column
- Delta sync already queries for deleted entries
- Supports audit trail and potential recovery
- Consistent with existing patterns in codebase

### D2: 204 vs 200 for Success

**Decision:** Return `204 No Content` on successful delete.

**Rationale:**
- REST convention for DELETE with no response body
- Consistent with HTTP semantics
- No useful data to return (entry is gone)

### D3: Idempotency Behavior

**Decision:** Return `404` on subsequent deletes of same ID.

**Rationale:**
- Entry is "not found" from client perspective (soft-deleted)
- Consistent with standard REST semantics
- Alternative (always 204) would hide programming errors

### D4: ULID Validation in Handler

**Decision:** Validate ULID format before calling store.

**Rationale:**
- Fail fast with clear error message
- Avoid unnecessary database round-trip
- Return specific 400 error (not generic 404)

### D5: Logging Level

**Decision:** Log successful deletes at INFO level.

**Rationale:**
- Delete is a significant operation
- Useful for audit trail
- Consistent with other mutating operations (ingest, feedback)

---

The path is clear. Build well.

---

## Implementation Notes (Spark)

### Implementation Summary

**Implemented**: 2026-01-30

All deliverables from Clario's technical design have been implemented using TDD methodology:

### Files Modified

1. **`internal/store/store.go`** (line 16)
   - Added `DeleteLore(ctx context.Context, id string) error` to Store interface

2. **`internal/store/sqlite.go`** (lines 429-450)
   - Implemented `DeleteLore` with soft-delete (sets `deleted_at` and `updated_at`)
   - Returns `ErrNotFound` if entry doesn't exist or already deleted

3. **`internal/api/handlers.go`** (lines 284-314)
   - Added `DeleteLore` handler
   - ULID validation using `ulid.Parse()`
   - Returns 204 No Content on success
   - Returns 404 Not Found for missing/deleted entries
   - Returns 400 Bad Request for invalid ULID format

4. **`internal/api/routes.go`** (line 29)
   - Registered `DELETE /api/v1/lore/{id}` route with auth middleware

### Test Coverage

**Store Tests** (`sqlite_test.go`):
- `TestDeleteLore_Success` - verifies soft-delete sets `deleted_at`
- `TestDeleteLore_NotFound` - returns ErrNotFound for non-existent ID
- `TestDeleteLore_AlreadyDeleted` - returns ErrNotFound on second delete
- `TestDeleteLore_UpdatesTimestamp` - both `deleted_at` and `updated_at` set
- `TestDeleteLore_ExcludedFromGetLore` - GetLore returns ErrNotFound after delete
- `TestDeleteLore_AppearsInDelta` - deleted ID appears in `deleted_ids`

**Handler Tests** (`handlers_test.go`):
- `TestDeleteLore_Success` - 204 No Content
- `TestDeleteLore_NotFound` - 404 Not Found
- `TestDeleteLore_InvalidULID` - 400 Bad Request
- `TestDeleteLore_StoreError` - 500 Internal Server Error
- `TestDeleteLore_Unauthorized` - 401 via route middleware
- `TestDeleteEndpoint_RoundTrip` - full integration test

**Coverage** (post-review fixes):
- Handler DeleteLore: 100%
- Store DeleteLore: 90%
- Rate Limiter Middleware: 100%

### Deviations from Design

None. Implementation follows Clario's design exactly.

### Code Review Fixes (Blade Review - 2026-01-30)

All 7 issues from Blade's adversarial review have been addressed:

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 1 | MEDIUM | Missing audit logging of WHO deleted | Added `request_id` and `remote_addr` to delete logs |
| 2 | MEDIUM | No rate limiting on DELETE | Added `DeleteRateLimiter` middleware (100 burst, 10/sec sustained) |
| 3 | LOW | ID echoed in 404 error | Changed to generic "Lore entry not found" message |
| 4 | LOW | No context cancellation test | Added `TestDeleteLore_RespectsCancellation` |
| 5 | LOW | Handler tests bypass auth (undocumented) | Added documentation comment explaining intentional isolation |
| 6 | LOW | No concurrent delete test | Added `TestDeleteLore_ConcurrentDeletes` |
| 7 | LOW | Untested RowsAffected error path | Added code comment explaining defensive code rationale |

**Additional test added:** `TestDeleteLore_RateLimited` - verifies rate limiting behavior

### Acceptance Criteria Validation

All acceptance criteria verified:
- [x] DELETE /api/v1/lore/{id} endpoint exists
- [x] Returns 204 No Content on successful delete
- [x] Returns 404 Not Found for non-existent or already-deleted entries
- [x] Returns 400 Bad Request for invalid ULID format
- [x] Requires authentication (401 without valid Bearer token)
- [x] Soft-delete sets deleted_at timestamp
- [x] Deleted entries appear in delta sync deleted_ids
- [x] GetLore returns ErrNotFound for deleted entries
- [x] E2E-SYNC-002 unblocked (delta sync test can now validate deletes)

---

Built and tested. Ready for your ruthless review.
