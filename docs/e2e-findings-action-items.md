# E2E Test Findings - Action Items

**Created:** 2026-01-29
**Source:** E2E Test Execution (E2E-SYNC-002, E2E-SHUT-001, E2E-ERR-001, BUG-001)

---

## Summary

| # | Issue | Status | Priority | Complexity | Action |
|---|-------|--------|----------|------------|--------|
| 1 | Deduplication Response Bug | RESOLVED | - | - | Close BUG-001 |
| 2 | Soft Delete Delta Sync | FEATURE GAP | P2 | M | Add DELETE endpoint |
| 3 | Graceful Shutdown In-Flight | INVESTIGATE | P1 | S | Verify test methodology |
| 4 | Validation Error Response Code | AS DESIGNED | - | - | Document behavior |

---

## Issue 1: Deduplication Response Bug (BUG-001)

### Symptom
`POST /api/v1/lore` returns `{"accepted":0,"merged":0,"rejected":0}` when a semantic merge actually occurred.

### Root Cause Analysis
The bug report (`_bmad-output/bugs/BUG-001-deduplication-merged-count.md`) documented that the handler hardcoded `Merged: 0` instead of using `result.Merged`.

### Current State: RESOLVED
The code has been fixed. Current implementation in `/workspaces/engram/internal/api/handlers.go` (lines 92-108):

```go
var accepted, merged int
if len(validEntries) > 0 {
    result, err := h.store.IngestLore(r.Context(), validEntries)
    // ...
    accepted = result.Accepted
    merged = result.Merged  // Correctly uses store result
}
```

The unit test `TestIngestLore_MergedCountFromStore` passes:
```
=== RUN   TestIngestLore_MergedCountFromStore
--- PASS: TestIngestLore_MergedCountFromStore (0.00s)
```

### Action Required
- [x] Code fix applied
- [ ] Close BUG-001 report
- [ ] Verify in next E2E run

---

## Issue 2: Soft Delete Delta Sync (E2E-SYNC-002)

### Symptom
E2E-SYNC-002 was skipped because soft delete could not be tested. The delta endpoint returns `deleted_ids` but there's no way to trigger soft deletes.

### Root Cause Analysis

**Schema supports soft delete:**
- Column `deleted_at TEXT` exists in `lore_entries` table
- Index `idx_lore_entries_deleted_at` enables efficient queries

**Delta query is implemented correctly** (`/workspaces/engram/internal/store/sqlite.go` lines 759-778):
```go
// Query 2: Deleted entry IDs
deletedRows, err := s.db.QueryContext(ctx, `
    SELECT id FROM lore_entries
    WHERE deleted_at IS NOT NULL AND deleted_at > ?
    ORDER BY deleted_at ASC
`, sinceStr)
```

**Missing: Delete endpoint**
- No `DELETE /api/v1/lore/{id}` route in `/workspaces/engram/internal/api/routes.go`
- No `DeleteLore` method in Store interface (`/workspaces/engram/internal/store/store.go`)

### Action Required

**Priority:** P2 (Medium)
**Complexity:** M (Medium) - Requires new endpoint, store method, and validation

1. Add to Store interface:
   ```go
   SoftDelete(ctx context.Context, id string) error
   ```

2. Implement in SQLiteStore:
   ```go
   func (s *SQLiteStore) SoftDelete(ctx context.Context, id string) error {
       now := time.Now().UTC().Format(time.RFC3339)
       result, err := s.db.ExecContext(ctx, `
           UPDATE lore_entries
           SET deleted_at = ?, updated_at = ?
           WHERE id = ? AND deleted_at IS NULL
       `, now, now, id)
       // Handle ErrNotFound if rowsAffected == 0
   }
   ```

3. Add API endpoint:
   - Route: `DELETE /api/v1/lore/{id}`
   - Auth: Required (same as other protected routes)
   - Response: 204 No Content (success), 404 Not Found (already deleted or doesn't exist)

4. Re-run E2E-SYNC-002

### Decision Point
Consider whether delete functionality is needed for MVP. If not, document as out-of-scope and mark E2E-SYNC-002 as N/A.

---

## Issue 3: Graceful Shutdown In-Flight Requests (E2E-SHUT-001)

### Symptom
E2E-SHUT-001 showed only 1/20 batch entries completed when SIGTERM was sent 0.1s after request started.

### Root Cause Analysis

**Shutdown configuration exists:**
- Default: 15 seconds (`/workspaces/engram/internal/config/config.go` line 159)
- Environment override: `ENGRAM_SHUTDOWN_TIMEOUT` (line 223-226)

**Graceful shutdown is properly implemented** (`/workspaces/engram/cmd/engram/root.go` lines 125-144):
```go
// 12. Graceful shutdown sequence
shutdownCtx, shutdownCancel := context.WithTimeout(
    context.Background(),
    time.Duration(cfg.Server.ShutdownTimeout))  // Default: 15s

// 12a. Stop HTTP server (drains in-flight requests)
if err := srv.Shutdown(shutdownCtx); err != nil {
    slog.Error("server shutdown error", "error", err)
}
```

**Potential issues:**

1. **Test timing is extremely tight** (0.1s = 100ms)
   - Request processing includes: JSON parsing, embedding generation (HTTP to OpenAI), DB transaction
   - 100ms may not be enough for embedding API call to complete

2. **Batch processing is atomic within transaction**
   - If embedding call is interrupted, entire batch may fail
   - Store uses single transaction for batch (lines 349-391 in sqlite.go)

3. **Context propagation**
   - HTTP request context is separate from signal context
   - `http.Server.Shutdown()` should drain properly but doesn't wait indefinitely

### Action Required

**Priority:** P1 (High) - Graceful shutdown is critical for production
**Complexity:** S (Small) - Primarily test methodology verification

1. **Verify test methodology:**
   - Increase delay between request start and SIGTERM to 2-5 seconds
   - Use smaller batch size (5 entries vs 50)
   - Add detailed timing logs to understand actual processing time

2. **Add instrumentation:**
   ```go
   slog.Info("ingest started", "entries", len(entries))
   // ... processing ...
   slog.Info("ingest completed", "duration_ms", time.Since(start).Milliseconds())
   ```

3. **Consider adding context check in batch loop:**
   ```go
   for i, entry := range entries {
       select {
       case <-ctx.Done():
           return nil, ctx.Err() // Or commit partial results
       default:
       }
       // ... process entry ...
   }
   ```

4. **Document shutdown guarantees:**
   - In-flight requests have up to `ENGRAM_SHUTDOWN_TIMEOUT` (default 15s) to complete
   - Long-running embedding operations may be interrupted if they exceed timeout
   - Recommend timeout >= max expected request processing time

---

## Issue 4: Validation Error Response Code (E2E-ERR-001)

### Symptom
Validation errors return HTTP 200 with `rejected` count instead of 422.

### Root Cause Analysis

This behavior is **intentional and correct** for batch endpoints with partial success.

**Two-tier validation in `/workspaces/engram/internal/api/handlers.go`:**

1. **Request-level validation** (lines 65-69):
   - Missing `source_id`, empty `lore` array, batch size exceeded
   - Returns **422 Unprocessable Entity** via `WriteProblemWithErrors()`

2. **Entry-level validation** (lines 71-90):
   - Content too long, invalid category, confidence out of range
   - Counts as `rejected`, included in `errors` array
   - Returns **200 OK** with partial success response

**Example responses:**

Request-level error (422):
```json
{
  "type": "https://engram.dev/errors/validation-error",
  "title": "Validation Error",
  "status": 422,
  "detail": "Request contains invalid fields",
  "errors": [{"field": "source_id", "message": "is required"}]
}
```

Entry-level error (200):
```json
{
  "accepted": 2,
  "merged": 0,
  "rejected": 1,
  "errors": ["lore[2].content: exceeds maximum length of 4000 characters"]
}
```

### Action Required

**Priority:** None (documentation only)
**Complexity:** None

- [x] Behavior is correct
- [ ] Update API documentation to clarify:
  - 422 for request-level validation failures
  - 200 with `rejected > 0` for entry-level validation failures
  - Both cases include error details

**Documentation update for `/workspaces/engram/docs/api-specification.md`:**

```markdown
### Validation Behavior

The ingest endpoint uses two-tier validation:

1. **Request-level validation** - Rejects entire request with 422:
   - Missing `source_id`
   - Empty `lore` array
   - Batch size exceeds 50

2. **Entry-level validation** - Partial success with 200:
   - Individual entries that fail validation are counted in `rejected`
   - Valid entries are still processed
   - Error details included in `errors` array
```

---

## Next Steps

1. **Immediate:**
   - Close BUG-001 (deduplication response bug)
   - Re-test E2E-SHUT-001 with adjusted timing

2. **Sprint Planning:**
   - Add Delete endpoint story (P2)
   - Update API documentation

3. **E2E Re-run Checklist:**
   - [ ] E2E-DEDUP-001: Verify merged count in response
   - [ ] E2E-SHUT-001: Verify with 2s delay and 5-entry batch
   - [ ] E2E-SYNC-002: Skip until Delete endpoint implemented (or implement)

---

## File References

| File | Relevance |
|------|-----------|
| `/workspaces/engram/internal/api/handlers.go` | Ingest handler, validation flow |
| `/workspaces/engram/internal/store/sqlite.go` | Store implementation, transactions |
| `/workspaces/engram/internal/store/store.go` | Store interface (missing Delete) |
| `/workspaces/engram/internal/api/routes.go` | API routes (missing DELETE) |
| `/workspaces/engram/internal/config/config.go` | Shutdown timeout config |
| `/workspaces/engram/cmd/engram/root.go` | Graceful shutdown implementation |
| `/workspaces/engram/_bmad-output/bugs/BUG-001-deduplication-merged-count.md` | Stale bug report |
| `/workspaces/engram/docs/e2e-test-plan.md` | E2E test results |
