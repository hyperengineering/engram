package store

import (
	"database/sql"
	"encoding/binary"
	"math"
	"time"

	"github.com/hyperengineering/engram/internal/types"
	"github.com/oklog/ulid/v2"
	_ "modernc.org/sqlite"
)

// SQLiteStore represents the SQLite-backed lore database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLiteStore instance.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS lore (
		id TEXT PRIMARY KEY,
		content TEXT NOT NULL,
		context TEXT,
		category TEXT NOT NULL,
		confidence REAL NOT NULL DEFAULT 0.7,
		embedding BLOB NOT NULL,
		source_id TEXT NOT NULL,
		sources TEXT,
		validation_count INTEGER NOT NULL DEFAULT 0,
		last_validated TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		synced_at TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_lore_category ON lore(category);
	CREATE INDEX IF NOT EXISTS idx_lore_confidence ON lore(confidence);
	CREATE INDEX IF NOT EXISTS idx_lore_last_validated ON lore(last_validated);
	CREATE INDEX IF NOT EXISTS idx_lore_created_at ON lore(created_at);
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
func (s *SQLiteStore) Record(lore types.Lore, embedding []float32) (*types.Lore, error) {
	now := time.Now().UTC()
	lore.ID = ulid.Make().String()
	lore.CreatedAt = now
	lore.UpdatedAt = now
	lore.Embedding = packEmbedding(embedding)

	_, err := s.db.Exec(`
		INSERT INTO lore (id, content, context, category, confidence, embedding, source_id, validation_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lore.ID, lore.Content, lore.Context, lore.Category, lore.Confidence, lore.Embedding, lore.SourceID, lore.ValidationCount, lore.CreatedAt.Format(time.RFC3339), lore.UpdatedAt.Format(time.RFC3339))

	if err != nil {
		return nil, err
	}

	return &lore, nil
}

// Count returns the number of lore entries
func (s *SQLiteStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM lore").Scan(&count)
	return count, err
}

// FindSimilar finds lore entries similar to the given embedding
func (s *SQLiteStore) FindSimilar(embedding []float32, threshold float32, limit int) ([]types.Lore, error) {
	rows, err := s.db.Query(`SELECT id, content, context, category, confidence, embedding, source_id, validation_count, last_validated, created_at, updated_at FROM lore`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []struct {
		lore       types.Lore
		similarity float32
	}

	for rows.Next() {
		var lore types.Lore
		var lastValidated, createdAt, updatedAt sql.NullString

		if err := rows.Scan(&lore.ID, &lore.Content, &lore.Context, &lore.Category, &lore.Confidence, &lore.Embedding, &lore.SourceID, &lore.ValidationCount, &lastValidated, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			t, _ := time.Parse(time.RFC3339, createdAt.String)
			lore.CreatedAt = t
		}
		if updatedAt.Valid {
			t, _ := time.Parse(time.RFC3339, updatedAt.String)
			lore.UpdatedAt = t
		}
		if lastValidated.Valid {
			t, _ := time.Parse(time.RFC3339, lastValidated.String)
			lore.LastValidated = &t
		}

		storedEmbedding := unpackEmbedding(lore.Embedding)
		similarity := cosineSimilarity(embedding, storedEmbedding)

		if similarity >= threshold {
			results = append(results, struct {
				lore       types.Lore
				similarity float32
			}{lore, similarity})
		}
	}

	// Sort by similarity descending
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].similarity > results[i].similarity {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Return top limit
	loreResults := make([]types.Lore, 0, limit)
	for i := 0; i < len(results) && i < limit; i++ {
		loreResults = append(loreResults, results[i].lore)
	}

	return loreResults, nil
}

func packEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func unpackEmbedding(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}
