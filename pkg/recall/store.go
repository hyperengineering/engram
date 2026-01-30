package recall

import (
	"database/sql"
	"time"

	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// Store handles local lore persistence
type Store struct {
	db       *sql.DB
	sourceID string
}

// NewStore creates a new Store instance
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	s := &Store{
		db:       db,
		sourceID: ulid.Make().String(),
	}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS lore (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		context TEXT,
		category TEXT NOT NULL,
		confidence REAL NOT NULL DEFAULT 0.7,
		embedding BLOB,
		source_id TEXT NOT NULL,
		validation_count INTEGER NOT NULL DEFAULT 0,
		last_validated TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		synced_at TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_lore_category ON lore(category);
	CREATE INDEX IF NOT EXISTS idx_lore_confidence ON lore(confidence);
	CREATE INDEX IF NOT EXISTS idx_lore_synced_at ON lore(synced_at);

	CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS sync_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		lore_id TEXT NOT NULL,
		operation TEXT NOT NULL,
		payload TEXT,
		queued_at TEXT NOT NULL,
		attempts INTEGER DEFAULT 0,
		last_error TEXT
	);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Record stores a new lore entry
func (s *Store) Record(params RecordParams) (*Lore, error) {
	now := time.Now().UTC()
	id := ulid.Make().String()

	_, err := s.db.Exec(`
		INSERT INTO lore (id, content, context, category, confidence, source_id, validation_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, params.Content, params.Context, params.Category, params.Confidence, s.sourceID, 0, now.Format(time.RFC3339), now.Format(time.RFC3339))

	if err != nil {
		return nil, err
	}

	// Queue for sync
	_, err = s.db.Exec(`
		INSERT INTO sync_queue (lore_id, operation, queued_at)
		VALUES (?, 'INSERT', ?)
	`, id, now.Format(time.RFC3339))

	return &Lore{
		ID:         id,
		Content:    params.Content,
		Context:    params.Context,
		Category:   params.Category,
		Confidence: params.Confidence,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, err
}

// Query retrieves lore matching the query parameters
func (s *Store) Query(params QueryParams) ([]Lore, error) {
	// TODO: Implement semantic search with embeddings
	// For now, return recent lore matching filters

	query := `
		SELECT id, content, context, category, confidence, validation_count, last_validated, created_at, updated_at
		FROM lore
		WHERE confidence >= ?
	`
	args := []interface{}{params.MinConfidence}

	if len(params.Categories) > 0 {
		query += " AND category IN ("
		for i, cat := range params.Categories {
			if i > 0 {
				query += ","
			}
			query += "?"
			args = append(args, cat)
		}
		query += ")"
	}

	query += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, params.K)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lore []Lore
	for rows.Next() {
		var l Lore
		var lastValidated, createdAt, updatedAt sql.NullString

		if err := rows.Scan(&l.ID, &l.Content, &l.Context, &l.Category, &l.Confidence, &l.ValidationCount, &lastValidated, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			t, _ := time.Parse(time.RFC3339, createdAt.String)
			l.CreatedAt = t
		}
		if updatedAt.Valid {
			t, _ := time.Parse(time.RFC3339, updatedAt.String)
			l.UpdatedAt = t
		}
		if lastValidated.Valid {
			t, _ := time.Parse(time.RFC3339, lastValidated.String)
			l.LastValidated = &t
		}

		lore = append(lore, l)
	}

	return lore, nil
}

// UpdateConfidence updates the confidence of a lore entry
func (s *Store) UpdateConfidence(id string, delta float64) (*FeedbackUpdate, error) {
	var previous float64
	err := s.db.QueryRow("SELECT confidence FROM lore WHERE id = ?", id).Scan(&previous)
	if err != nil {
		return nil, err
	}

	current := previous + delta
	if current > 1.0 {
		current = 1.0
	}
	if current < 0.0 {
		current = 0.0
	}

	now := time.Now().UTC()
	var validationCount int

	if delta > 0 {
		// Increment validation count for positive feedback
		_, err = s.db.Exec(`
			UPDATE lore SET confidence = ?, validation_count = validation_count + 1, last_validated = ?, updated_at = ?
			WHERE id = ?
		`, current, now.Format(time.RFC3339), now.Format(time.RFC3339), id)
		s.db.QueryRow("SELECT validation_count FROM lore WHERE id = ?", id).Scan(&validationCount)
	} else {
		_, err = s.db.Exec(`
			UPDATE lore SET confidence = ?, updated_at = ?
			WHERE id = ?
		`, current, now.Format(time.RFC3339), id)
		s.db.QueryRow("SELECT validation_count FROM lore WHERE id = ?", id).Scan(&validationCount)
	}

	if err != nil {
		return nil, err
	}

	// Queue for sync
	_, _ = s.db.Exec(`
		INSERT INTO sync_queue (lore_id, operation, queued_at)
		VALUES (?, 'FEEDBACK', ?)
	`, id, now.Format(time.RFC3339))

	return &FeedbackUpdate{
		ID:              id,
		Previous:        previous,
		Current:         current,
		ValidationCount: validationCount,
	}, nil
}

// GetPending returns lore entries pending sync
func (s *Store) GetPending() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT lore_id FROM sync_queue")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	return ids, nil
}

// ClearPending clears the sync queue
func (s *Store) ClearPending() error {
	_, err := s.db.Exec("DELETE FROM sync_queue")
	return err
}

// Stats returns store statistics
func (s *Store) Stats() StoreStats {
	var count, pending int
	s.db.QueryRow("SELECT COUNT(*) FROM lore").Scan(&count)
	s.db.QueryRow("SELECT COUNT(*) FROM sync_queue").Scan(&pending)

	return StoreStats{
		LoreCount:   count,
		PendingSync: pending,
	}
}

// SourceID returns the source ID for this store
func (s *Store) SourceID() string {
	return s.sourceID
}
