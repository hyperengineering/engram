package store

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
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
// It initializes the database with WAL mode, applies pragmas, and runs migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure parent directory exists
	if dir := filepath.Dir(dbPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable pragmas for performance and safety
	if err := enablePragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable pragmas: %w", err)
	}

	// Run goose migrations
	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// enablePragmas sets SQLite pragmas for optimal performance and safety.
func enablePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("execute %s: %w", pragma, err)
		}
	}

	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}


// Record stores a new lore entry
func (s *SQLiteStore) Record(lore types.Lore, embedding []float32) (*types.Lore, error) {
	now := time.Now().UTC()
	lore.ID = ulid.Make().String()
	lore.CreatedAt = now
	lore.UpdatedAt = now
	lore.Embedding = packEmbedding(embedding)

	_, err := s.db.Exec(`
		INSERT INTO lore_entries (id, content, context, category, confidence, embedding, source_id, validation_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, lore.ID, lore.Content, lore.Context, lore.Category, lore.Confidence, lore.Embedding, lore.SourceID, lore.ValidationCount, lore.CreatedAt.Format(time.RFC3339), lore.UpdatedAt.Format(time.RFC3339))

	if err != nil {
		return nil, err
	}

	return &lore, nil
}

// Count returns the number of lore entries (excluding soft-deleted)
func (s *SQLiteStore) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM lore_entries WHERE deleted_at IS NULL").Scan(&count)
	return count, err
}

// FindSimilar finds lore entries similar to the given embedding
func (s *SQLiteStore) FindSimilar(embedding []float32, threshold float32, limit int) ([]types.Lore, error) {
	rows, err := s.db.Query(`SELECT id, content, context, category, confidence, embedding, source_id, validation_count, last_validated_at, created_at, updated_at FROM lore_entries WHERE deleted_at IS NULL`)
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
			t, err := time.Parse(time.RFC3339, createdAt.String)
			if err != nil {
				slog.Warn("invalid timestamp in lore entry", "id", lore.ID, "field", "created_at", "err", err)
			}
			lore.CreatedAt = t
		}
		if updatedAt.Valid {
			t, err := time.Parse(time.RFC3339, updatedAt.String)
			if err != nil {
				slog.Warn("invalid timestamp in lore entry", "id", lore.ID, "field", "updated_at", "err", err)
			}
			lore.UpdatedAt = t
		}
		if lastValidated.Valid {
			t, err := time.Parse(time.RFC3339, lastValidated.String)
			if err != nil {
				slog.Warn("invalid timestamp in lore entry", "id", lore.ID, "field", "last_validated_at", "err", err)
			}
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
