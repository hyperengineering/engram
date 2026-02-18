package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	engramsync "github.com/hyperengineering/engram/internal/sync"
	"github.com/hyperengineering/engram/migrations"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// --- Schema Validation Tests ---

func TestMigration002_CreatesChangeLog(t *testing.T) {
	// Given: A fresh database with migrations applied
	db := newTestDB(t)

	// Then: change_log table exists with correct columns
	_, err := db.Exec(`
		SELECT sequence, table_name, entity_id, operation, payload, source_id, created_at, received_at
		FROM change_log LIMIT 0
	`)
	if err != nil {
		t.Fatalf("change_log table missing or has wrong columns: %v", err)
	}
}

func TestMigration002_CreatesIdempotency(t *testing.T) {
	// Given: A fresh database with migrations applied
	db := newTestDB(t)

	// Then: push_idempotency table exists with correct columns
	_, err := db.Exec(`
		SELECT push_id, store_id, response, created_at, expires_at
		FROM push_idempotency LIMIT 0
	`)
	if err != nil {
		t.Fatalf("push_idempotency table missing or has wrong columns: %v", err)
	}
}

func TestMigration002_CreatesSyncMeta(t *testing.T) {
	// Given: A fresh database with migrations applied
	db := newTestDB(t)

	// Then: sync_meta table exists with default values
	var schemaVersion, compactionSeq, compactionAt string
	err := db.QueryRow(`SELECT value FROM sync_meta WHERE key = 'schema_version'`).Scan(&schemaVersion)
	if err != nil {
		t.Fatalf("sync_meta schema_version not found: %v", err)
	}
	if schemaVersion != "2" {
		t.Errorf("expected schema_version '2', got %q", schemaVersion)
	}

	err = db.QueryRow(`SELECT value FROM sync_meta WHERE key = 'last_compaction_seq'`).Scan(&compactionSeq)
	if err != nil {
		t.Fatalf("sync_meta last_compaction_seq not found: %v", err)
	}
	if compactionSeq != "0" {
		t.Errorf("expected last_compaction_seq '0', got %q", compactionSeq)
	}

	err = db.QueryRow(`SELECT value FROM sync_meta WHERE key = 'last_compaction_at'`).Scan(&compactionAt)
	if err != nil {
		t.Fatalf("sync_meta last_compaction_at not found: %v", err)
	}
	if compactionAt != "" {
		t.Errorf("expected last_compaction_at '', got %q", compactionAt)
	}
}

func TestMigration002_PreservesExistingData(t *testing.T) {
	// Given: A database with existing lore_entries
	db := newTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO lore_entries (id, content, category, confidence, source_id, sources, created_at, updated_at)
		VALUES ('preserve-test', 'preserved content', 'pattern_outcome', 0.5, 'src-1', '[]', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Then: lore_entries data is still accessible
	var content string
	err = db.QueryRow(`SELECT content FROM lore_entries WHERE id = 'preserve-test'`).Scan(&content)
	if err != nil {
		t.Fatalf("lore_entries data not preserved: %v", err)
	}
	if content != "preserved content" {
		t.Errorf("expected 'preserved content', got %q", content)
	}
}

func TestMigration002_Indexes(t *testing.T) {
	// Given: A migrated database
	db := newTestDB(t)

	// Then: All new indexes exist
	expectedIndexes := []string{
		"idx_change_log_sequence",
		"idx_change_log_entity",
		"idx_push_idempotency_expires",
	}

	for _, idx := range expectedIndexes {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name)
		if err != nil {
			t.Errorf("index %s not found: %v", idx, err)
		}
	}
}

func TestMigration002_OperationConstraint(t *testing.T) {
	// Given: A migrated database
	db := newTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// When: Inserting with invalid operation
	_, err := db.Exec(`
		INSERT INTO change_log (table_name, entity_id, operation, source_id, created_at)
		VALUES ('lore_entries', 'entity-1', 'invalid', 'src-1', ?)
	`, now)

	// Then: Constraint violation
	if err == nil {
		t.Fatal("expected constraint violation for invalid operation, got nil")
	}
}

func TestMigration002_SequenceAutoIncrement(t *testing.T) {
	// Given: A migrated database
	db := newTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// When: Inserting entries
	result1, err := db.Exec(`
		INSERT INTO change_log (table_name, entity_id, operation, source_id, created_at)
		VALUES ('lore_entries', 'e-1', 'upsert', 'src-1', ?)
	`, now)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	seq1, _ := result1.LastInsertId()

	result2, err := db.Exec(`
		INSERT INTO change_log (table_name, entity_id, operation, source_id, created_at)
		VALUES ('lore_entries', 'e-2', 'upsert', 'src-1', ?)
	`, now)
	if err != nil {
		t.Fatalf("second insert failed: %v", err)
	}
	seq2, _ := result2.LastInsertId()

	// Then: Sequences are monotonically increasing
	if seq1 != 1 {
		t.Errorf("expected first sequence 1, got %d", seq1)
	}
	if seq2 != 2 {
		t.Errorf("expected second sequence 2, got %d", seq2)
	}
}

// --- Change Log Operation Tests ---

func TestAppendChangeLog_AssignsSequence(t *testing.T) {
	// Given: Empty change_log
	store := newTestStore(t)
	ctx := context.Background()

	entry := &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-1",
		Operation: engramsync.OperationUpsert,
		Payload:   json.RawMessage(`{"content":"test"}`),
		SourceID:  "source-1",
		CreatedAt: time.Now().UTC(),
	}

	// When: Appending entry
	seq, err := store.AppendChangeLog(ctx, entry)

	// Then: Sequence is 1
	if err != nil {
		t.Fatalf("AppendChangeLog failed: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected sequence 1, got %d", seq)
	}
}

func TestAppendChangeLog_IncrementsSequence(t *testing.T) {
	// Given: change_log with entries
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-1",
			Operation: engramsync.OperationUpsert,
			SourceID:  "source-1",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append %d failed: %v", i, err)
		}
	}

	// When: Appending another entry
	seq, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-6",
		Operation: engramsync.OperationUpsert,
		SourceID:  "source-1",
		CreatedAt: time.Now().UTC(),
	})

	// Then: Sequence is 6
	if err != nil {
		t.Fatalf("AppendChangeLog failed: %v", err)
	}
	if seq != 6 {
		t.Errorf("expected sequence 6, got %d", seq)
	}
}

func TestAppendChangeLog_DeleteOperation(t *testing.T) {
	// Given: A store
	store := newTestStore(t)
	ctx := context.Background()

	// When: Appending a delete entry (NULL payload)
	seq, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-1",
		Operation: engramsync.OperationDelete,
		Payload:   nil,
		SourceID:  "source-1",
		CreatedAt: time.Now().UTC(),
	})

	// Then: Entry is stored with NULL payload
	if err != nil {
		t.Fatalf("AppendChangeLog failed: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected sequence 1, got %d", seq)
	}

	// Verify the entry was stored correctly
	entries, err := store.GetChangeLogAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Operation != engramsync.OperationDelete {
		t.Errorf("expected operation 'delete', got %q", entries[0].Operation)
	}
	if entries[0].Payload != nil {
		t.Errorf("expected nil payload for delete, got %v", entries[0].Payload)
	}
}

func TestAppendChangeLogBatch_Multiple(t *testing.T) {
	// Given: Empty change_log
	store := newTestStore(t)
	ctx := context.Background()

	entries := []engramsync.ChangeLogEntry{
		{TableName: "lore_entries", EntityID: "e-1", Operation: engramsync.OperationUpsert, Payload: json.RawMessage(`{"a":1}`), SourceID: "src-1", CreatedAt: time.Now().UTC()},
		{TableName: "lore_entries", EntityID: "e-2", Operation: engramsync.OperationUpsert, Payload: json.RawMessage(`{"a":2}`), SourceID: "src-1", CreatedAt: time.Now().UTC()},
		{TableName: "lore_entries", EntityID: "e-3", Operation: engramsync.OperationDelete, SourceID: "src-1", CreatedAt: time.Now().UTC()},
	}

	// When: Appending batch
	highSeq, err := store.AppendChangeLogBatch(ctx, entries)

	// Then: Returns highest sequence
	if err != nil {
		t.Fatalf("AppendChangeLogBatch failed: %v", err)
	}
	if highSeq != 3 {
		t.Errorf("expected highest sequence 3, got %d", highSeq)
	}

	// Verify all entries exist
	result, err := store.GetChangeLogAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
}

func TestAppendChangeLogBatch_Empty(t *testing.T) {
	// Given: Empty entries
	store := newTestStore(t)
	ctx := context.Background()

	// When: Appending empty batch
	highSeq, err := store.AppendChangeLogBatch(ctx, nil)

	// Then: Returns 0 and no error
	if err != nil {
		t.Fatalf("AppendChangeLogBatch failed: %v", err)
	}
	if highSeq != 0 {
		t.Errorf("expected highest sequence 0, got %d", highSeq)
	}
}

func TestGetChangeLogAfter_ReturnsOrdered(t *testing.T) {
	// Given: Entries at seq 1,2,3
	store := newTestStore(t)
	ctx := context.Background()
	appendEntries(t, store, ctx, 3)

	// When: Query after=0, limit=10
	entries, err := store.GetChangeLogAfter(ctx, 0, 10)

	// Then: Returns [1,2,3] in order
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	for i, e := range entries {
		expectedSeq := int64(i + 1)
		if e.Sequence != expectedSeq {
			t.Errorf("entry %d: expected sequence %d, got %d", i, expectedSeq, e.Sequence)
		}
	}
}

func TestGetChangeLogAfter_RespectsLimit(t *testing.T) {
	// Given: Entries at seq 1-10
	store := newTestStore(t)
	ctx := context.Background()
	appendEntries(t, store, ctx, 10)

	// When: Query after=0, limit=3
	entries, err := store.GetChangeLogAfter(ctx, 0, 3)

	// Then: Returns [1,2,3] only
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[2].Sequence != 3 {
		t.Errorf("expected last sequence 3, got %d", entries[2].Sequence)
	}
}

func TestGetChangeLogAfter_FiltersCorrectly(t *testing.T) {
	// Given: Entries at seq 1-10
	store := newTestStore(t)
	ctx := context.Background()
	appendEntries(t, store, ctx, 10)

	// When: Query after=5, limit=10
	entries, err := store.GetChangeLogAfter(ctx, 5, 10)

	// Then: Returns [6,7,8,9,10]
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
	if entries[0].Sequence != 6 {
		t.Errorf("expected first sequence 6, got %d", entries[0].Sequence)
	}
	if entries[4].Sequence != 10 {
		t.Errorf("expected last sequence 10, got %d", entries[4].Sequence)
	}
}

func TestGetChangeLogAfter_EmptyResult(t *testing.T) {
	// Given: Entries at seq 1-5
	store := newTestStore(t)
	ctx := context.Background()
	appendEntries(t, store, ctx, 5)

	// When: Query after=5, limit=10
	entries, err := store.GetChangeLogAfter(ctx, 5, 10)

	// Then: Returns empty slice (not nil)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}
	if entries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestGetChangeLogAfter_PayloadRoundTrip(t *testing.T) {
	// Given: An entry with JSON payload
	store := newTestStore(t)
	ctx := context.Background()

	payload := json.RawMessage(`{"content":"hello world","confidence":0.8}`)
	_, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-1",
		Operation: engramsync.OperationUpsert,
		Payload:   payload,
		SourceID:  "source-1",
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("AppendChangeLog failed: %v", err)
	}

	// When: Reading back
	entries, err := store.GetChangeLogAfter(ctx, 0, 1)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}

	// Then: Payload round-trips correctly
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if string(entries[0].Payload) != string(payload) {
		t.Errorf("payload mismatch: expected %s, got %s", payload, entries[0].Payload)
	}
}

func TestGetChangeLogAfter_TimestampPrecision(t *testing.T) {
	// Given: An entry with sub-second precision timestamp
	store := newTestStore(t)
	ctx := context.Background()

	createdAt := time.Date(2026, 2, 17, 12, 30, 45, 123456000, time.UTC)
	_, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
		TableName: "lore_entries",
		EntityID:  "entity-1",
		Operation: engramsync.OperationUpsert,
		Payload:   json.RawMessage(`{}`),
		SourceID:  "source-1",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("AppendChangeLog failed: %v", err)
	}

	// When: Reading back
	entries, err := store.GetChangeLogAfter(ctx, 0, 1)
	if err != nil {
		t.Fatalf("GetChangeLogAfter failed: %v", err)
	}

	// Then: Timestamp round-trips with at least millisecond precision
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	diff := entries[0].CreatedAt.Sub(createdAt)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("timestamp precision lost: expected %v, got %v (diff: %v)", createdAt, entries[0].CreatedAt, diff)
	}
}

func TestGetLatestSequence_Empty(t *testing.T) {
	// Given: Empty change_log
	store := newTestStore(t)
	ctx := context.Background()

	// When: Getting latest sequence
	seq, err := store.GetLatestSequence(ctx)

	// Then: Returns 0
	if err != nil {
		t.Fatalf("GetLatestSequence failed: %v", err)
	}
	if seq != 0 {
		t.Errorf("expected sequence 0 for empty log, got %d", seq)
	}
}

func TestGetLatestSequence_WithEntries(t *testing.T) {
	// Given: Entries at seq 1-5
	store := newTestStore(t)
	ctx := context.Background()
	appendEntries(t, store, ctx, 5)

	// When: Getting latest sequence
	seq, err := store.GetLatestSequence(ctx)

	// Then: Returns 5
	if err != nil {
		t.Fatalf("GetLatestSequence failed: %v", err)
	}
	if seq != 5 {
		t.Errorf("expected sequence 5, got %d", seq)
	}
}

// --- Idempotency Operation Tests ---

func TestCheckPushIdempotency_NotFound(t *testing.T) {
	// Given: Empty cache
	store := newTestStore(t)
	ctx := context.Background()

	// When: Checking unknown ID
	response, found, err := store.CheckPushIdempotency(ctx, "unknown-id")

	// Then: Not found
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if found {
		t.Error("expected found=false for unknown ID")
	}
	if response != nil {
		t.Errorf("expected nil response, got %v", response)
	}
}

func TestCheckPushIdempotency_Found(t *testing.T) {
	// Given: Cache has a push entry
	store := newTestStore(t)
	ctx := context.Background()

	responseData := []byte(`{"accepted":3}`)
	err := store.RecordPushIdempotency(ctx, "push-123", "store-1", responseData, 1*time.Hour)
	if err != nil {
		t.Fatalf("RecordPushIdempotency failed: %v", err)
	}

	// When: Checking the cached push
	response, found, err := store.CheckPushIdempotency(ctx, "push-123")

	// Then: Returns cached response
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if string(response) != string(responseData) {
		t.Errorf("response mismatch: expected %s, got %s", responseData, response)
	}
}

func TestCheckPushIdempotency_Expired(t *testing.T) {
	// Given: Cache has an expired entry (TTL in the past)
	store := newTestStore(t)
	ctx := context.Background()

	// Record with a very short TTL that's already expired
	err := store.RecordPushIdempotency(ctx, "push-expired", "store-1", []byte(`{"ok":true}`), -1*time.Hour)
	if err != nil {
		t.Fatalf("RecordPushIdempotency failed: %v", err)
	}

	// When: Checking expired entry
	response, found, err := store.CheckPushIdempotency(ctx, "push-expired")

	// Then: Not found (expired)
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if found {
		t.Error("expected found=false for expired entry")
	}
	if response != nil {
		t.Errorf("expected nil response for expired entry, got %v", response)
	}
}

func TestRecordPushIdempotency_New(t *testing.T) {
	// Given: Empty cache
	store := newTestStore(t)
	ctx := context.Background()

	// When: Recording a push
	err := store.RecordPushIdempotency(ctx, "push-456", "store-1", []byte(`{"accepted":1}`), 1*time.Hour)

	// Then: Entry exists
	if err != nil {
		t.Fatalf("RecordPushIdempotency failed: %v", err)
	}

	// Verify entry exists
	response, found, err := store.CheckPushIdempotency(ctx, "push-456")
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if !found {
		t.Fatal("expected entry to exist after recording")
	}
	if string(response) != `{"accepted":1}` {
		t.Errorf("unexpected response: %s", response)
	}
}

func TestRecordPushIdempotency_Replace(t *testing.T) {
	// Given: Cache has "push-456"
	store := newTestStore(t)
	ctx := context.Background()

	err := store.RecordPushIdempotency(ctx, "push-456", "store-1", []byte(`{"v":1}`), 1*time.Hour)
	if err != nil {
		t.Fatalf("first record failed: %v", err)
	}

	// When: Recording "push-456" again with new response
	err = store.RecordPushIdempotency(ctx, "push-456", "store-1", []byte(`{"v":2}`), 1*time.Hour)
	if err != nil {
		t.Fatalf("replace record failed: %v", err)
	}

	// Then: Entry has the updated response
	response, found, err := store.CheckPushIdempotency(ctx, "push-456")
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if !found {
		t.Fatal("expected entry to exist")
	}
	if string(response) != `{"v":2}` {
		t.Errorf("expected updated response, got %s", response)
	}
}

func TestCleanExpiredIdempotency_RemovesExpired(t *testing.T) {
	// Given: Cache has 3 expired and 2 valid entries
	store := newTestStore(t)
	ctx := context.Background()

	// Record expired entries (TTL in past)
	for i := 0; i < 3; i++ {
		err := store.RecordPushIdempotency(ctx, "expired-"+string(rune('a'+i)), "store-1", []byte(`{}`), -1*time.Hour)
		if err != nil {
			t.Fatalf("record expired %d failed: %v", i, err)
		}
	}

	// Record valid entries (TTL in future)
	for i := 0; i < 2; i++ {
		err := store.RecordPushIdempotency(ctx, "valid-"+string(rune('a'+i)), "store-1", []byte(`{}`), 1*time.Hour)
		if err != nil {
			t.Fatalf("record valid %d failed: %v", i, err)
		}
	}

	// When: Cleaning expired entries
	removed, err := store.CleanExpiredIdempotency(ctx)

	// Then: Removes 3 expired entries
	if err != nil {
		t.Fatalf("CleanExpiredIdempotency failed: %v", err)
	}
	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}

	// Verify valid entries still exist
	for i := 0; i < 2; i++ {
		_, found, err := store.CheckPushIdempotency(ctx, "valid-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("check valid %d failed: %v", i, err)
		}
		if !found {
			t.Errorf("valid entry %d should still exist", i)
		}
	}
}

func TestCleanExpiredIdempotency_NoneExpired(t *testing.T) {
	// Given: Cache has 5 valid entries
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := store.RecordPushIdempotency(ctx, "valid-"+string(rune('a'+i)), "store-1", []byte(`{}`), 1*time.Hour)
		if err != nil {
			t.Fatalf("record %d failed: %v", i, err)
		}
	}

	// When: Cleaning
	removed, err := store.CleanExpiredIdempotency(ctx)

	// Then: 0 removed
	if err != nil {
		t.Fatalf("CleanExpiredIdempotency failed: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

// --- Sync Meta Operation Tests ---

func TestGetSyncMeta_DefaultValues(t *testing.T) {
	// Given: Fresh migration
	store := newTestStore(t)
	ctx := context.Background()

	// When: Getting schema_version
	val, err := store.GetSyncMeta(ctx, engramsync.SyncMetaSchemaVersion)

	// Then: Returns "2"
	if err != nil {
		t.Fatalf("GetSyncMeta failed: %v", err)
	}
	if val != "2" {
		t.Errorf("expected '2', got %q", val)
	}
}

func TestGetSyncMeta_NotFound(t *testing.T) {
	// Given: Fresh migration
	store := newTestStore(t)
	ctx := context.Background()

	// When: Getting unknown key
	_, err := store.GetSyncMeta(ctx, "unknown_key")

	// Then: Returns error
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
}

func TestSetSyncMeta_Update(t *testing.T) {
	// Given: Key exists
	store := newTestStore(t)
	ctx := context.Background()

	// When: Setting new value
	err := store.SetSyncMeta(ctx, engramsync.SyncMetaSchemaVersion, "3")
	if err != nil {
		t.Fatalf("SetSyncMeta failed: %v", err)
	}

	// Then: Value is updated
	val, err := store.GetSyncMeta(ctx, engramsync.SyncMetaSchemaVersion)
	if err != nil {
		t.Fatalf("GetSyncMeta failed: %v", err)
	}
	if val != "3" {
		t.Errorf("expected '3', got %q", val)
	}
}

func TestSetSyncMeta_Insert(t *testing.T) {
	// Given: Key doesn't exist
	store := newTestStore(t)
	ctx := context.Background()

	// When: Setting new key
	err := store.SetSyncMeta(ctx, "new_key", "new_value")
	if err != nil {
		t.Fatalf("SetSyncMeta failed: %v", err)
	}

	// Then: Key is created
	val, err := store.GetSyncMeta(ctx, "new_key")
	if err != nil {
		t.Fatalf("GetSyncMeta failed: %v", err)
	}
	if val != "new_value" {
		t.Errorf("expected 'new_value', got %q", val)
	}
}

// --- Integration Tests ---

func TestChangeLogIntegration_FullCycle(t *testing.T) {
	// Given: A store
	store := newTestStore(t)
	ctx := context.Background()

	// When: Append 100 entries
	for i := 0; i < 100; i++ {
		_, err := store.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-" + string(rune('a'+i%26)),
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{"i":` + string(rune('0'+i%10)) + `}`),
			SourceID:  "source-1",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append %d failed: %v", i, err)
		}
	}

	// Then: Paginate through with limit=25 and verify order and completeness
	var allEntries []engramsync.ChangeLogEntry
	afterSeq := int64(0)
	for {
		page, err := store.GetChangeLogAfter(ctx, afterSeq, 25)
		if err != nil {
			t.Fatalf("GetChangeLogAfter(after=%d) failed: %v", afterSeq, err)
		}
		if len(page) == 0 {
			break
		}
		allEntries = append(allEntries, page...)
		afterSeq = page[len(page)-1].Sequence
	}

	if len(allEntries) != 100 {
		t.Errorf("expected 100 entries, got %d", len(allEntries))
	}

	// Verify ordering
	for i := 1; i < len(allEntries); i++ {
		if allEntries[i].Sequence <= allEntries[i-1].Sequence {
			t.Errorf("entries not ordered: seq[%d]=%d, seq[%d]=%d",
				i-1, allEntries[i-1].Sequence, i, allEntries[i].Sequence)
		}
	}

	// Verify latest sequence
	latest, err := store.GetLatestSequence(ctx)
	if err != nil {
		t.Fatalf("GetLatestSequence failed: %v", err)
	}
	if latest != 100 {
		t.Errorf("expected latest sequence 100, got %d", latest)
	}
}

func TestIdempotencyIntegration_ConcurrentAccess(t *testing.T) {
	// Given: A store
	s := newTestStore(t)
	ctx := context.Background()

	// When: 10 goroutines check/record same push_id
	var wg sync.WaitGroup
	pushID := "concurrent-push"
	errCh := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			// Try to record
			response := []byte(`{"goroutine":` + string(rune('0'+n)) + `}`)
			err := s.RecordPushIdempotency(ctx, pushID, "store-1", response, 1*time.Hour)
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent record error: %v", err)
	}

	// Then: Exactly one record exists
	response, found, err := s.CheckPushIdempotency(ctx, pushID)
	if err != nil {
		t.Fatalf("CheckPushIdempotency failed: %v", err)
	}
	if !found {
		t.Fatal("expected entry to exist after concurrent writes")
	}
	if response == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestMigrationIntegration_UpgradeFromV1(t *testing.T) {
	// Given: A V1 database with data (created via NewSQLiteStore which runs migrations)
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Verify lore_entries table works
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`
		INSERT INTO lore_entries (id, content, category, confidence, source_id, sources, created_at, updated_at)
		VALUES ('upgrade-test', 'test content', 'pattern_outcome', 0.5, 'src-1', '[]', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert lore: %v", err)
	}

	// Verify new tables exist (migration 002 should have run)
	var tableName string
	for _, tbl := range []string{"change_log", "push_idempotency", "sync_meta"} {
		err = s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&tableName)
		if err != nil {
			t.Errorf("table %s not found after upgrade: %v", tbl, err)
		}
	}

	// Verify original data preserved
	var content string
	err = s.db.QueryRow(`SELECT content FROM lore_entries WHERE id = 'upgrade-test'`).Scan(&content)
	if err != nil {
		t.Fatalf("data not preserved: %v", err)
	}
	if content != "test content" {
		t.Errorf("expected 'test content', got %q", content)
	}

	s.Close()
}

func TestMigrationIntegration_IncrementalV1ToV2(t *testing.T) {
	// Given: A database with ONLY migration 001 applied and existing data
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Configure goose and apply only V1 migration
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	if err := goose.UpTo(db, ".", 1); err != nil {
		t.Fatalf("migration V1 failed: %v", err)
	}

	// Insert V1 data
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO lore_entries (id, content, category, confidence, source_id, sources, created_at, updated_at)
		VALUES ('v1-data', 'existing v1 content', 'pattern_outcome', 0.5, 'src-1', '[]', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert V1 data: %v", err)
	}

	// Verify V2 tables do NOT exist yet
	for _, tbl := range []string{"change_log", "push_idempotency", "sync_meta"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err == nil {
			t.Fatalf("table %s should not exist before V2 migration", tbl)
		}
	}

	// When: Apply V2 migration incrementally
	if err := goose.UpTo(db, ".", 2); err != nil {
		t.Fatalf("incremental migration to V2 failed: %v", err)
	}

	// Then: V2 tables exist
	for _, tbl := range []string{"change_log", "push_idempotency", "sync_meta"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found after V2 migration: %v", tbl, err)
		}
	}

	// And: V1 data is preserved
	var content string
	err = db.QueryRow(`SELECT content FROM lore_entries WHERE id = 'v1-data'`).Scan(&content)
	if err != nil {
		t.Fatalf("V1 data not preserved after V2 migration: %v", err)
	}
	if content != "existing v1 content" {
		t.Errorf("expected 'existing v1 content', got %q", content)
	}

	// And: sync_meta defaults are set
	var schemaVersion string
	err = db.QueryRow(`SELECT value FROM sync_meta WHERE key = 'schema_version'`).Scan(&schemaVersion)
	if err != nil {
		t.Fatalf("sync_meta schema_version not found: %v", err)
	}
	if schemaVersion != "2" {
		t.Errorf("expected schema_version '2', got %q", schemaVersion)
	}
}

func TestMigration002_Rollback(t *testing.T) {
	// Given: A database with migration 002 applied
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Insert lore data to verify preservation
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO lore_entries (id, content, category, confidence, source_id, sources, created_at, updated_at)
		VALUES ('rollback-test', 'preserved content', 'pattern_outcome', 0.5, 'src-1', '[]', ?, ?)
	`, now, now)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// When: Rolling back migration 002
	gooseDown(t, db)

	// Then: New tables are removed
	for _, tbl := range []string{"change_log", "push_idempotency", "sync_meta"} {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err == nil {
			t.Errorf("table %s should have been dropped by rollback", tbl)
		}
	}

	// And: lore_entries data is preserved
	var content string
	err = db.QueryRow(`SELECT content FROM lore_entries WHERE id = 'rollback-test'`).Scan(&content)
	if err != nil {
		t.Fatalf("lore_entries data not preserved after rollback: %v", err)
	}
	if content != "preserved content" {
		t.Errorf("expected 'preserved content', got %q", content)
	}
}

// --- Helper Functions ---

// newTestDB creates a fresh in-memory database with all migrations applied.
// Returns the raw *sql.DB for schema-level testing.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	return db
}

// newTestStore creates a fresh SQLiteStore with in-memory database for testing.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// appendEntries appends n generic change log entries.
func appendEntries(t *testing.T, s *SQLiteStore, ctx context.Context, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := s.AppendChangeLog(ctx, &engramsync.ChangeLogEntry{
			TableName: "lore_entries",
			EntityID:  "entity-" + string(rune('a'+i%26)),
			Operation: engramsync.OperationUpsert,
			Payload:   json.RawMessage(`{"i":` + string(rune('0'+i%10)) + `}`),
			SourceID:  "source-1",
			CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("append entry %d failed: %v", i, err)
		}
	}
}

// gooseDown rolls back one migration using goose.
func gooseDown(t *testing.T, db *sql.DB) {
	t.Helper()

	// goose is already configured (dialect, base FS) from RunMigrations
	// Just call Down to roll back one migration
	if err := goose.Down(db, "."); err != nil {
		t.Fatalf("goose down failed: %v", err)
	}
}
