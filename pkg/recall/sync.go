package recall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Syncer handles synchronization with Engram
type Syncer struct {
	engramURL string
	apiKey    string
	store     *Store
	client    *http.Client
}

// NewSyncer creates a new Syncer
func NewSyncer(engramURL, apiKey string, store *Store) *Syncer {
	return &Syncer{
		engramURL: engramURL,
		apiKey:    apiKey,
		store:     store,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Ping checks connectivity to Engram
func (s *Syncer) Ping() error {
	if s.engramURL == "" {
		return fmt.Errorf("engram URL not configured")
	}

	req, err := http.NewRequest("GET", s.engramURL+"/api/v1/health", nil)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %d", resp.StatusCode)
	}

	return nil
}

// Bootstrap performs initial sync from Engram
func (s *Syncer) Bootstrap() error {
	if s.engramURL == "" {
		return fmt.Errorf("engram URL not configured")
	}

	// Check health first
	if err := s.Ping(); err != nil {
		return err
	}

	// TODO: Download and replace local database with snapshot
	// For now, just verify connectivity

	return nil
}

// Pull pulls updates from Engram
func (s *Syncer) Pull() (*SyncStats, error) {
	if s.engramURL == "" {
		return &SyncStats{}, nil
	}

	start := time.Now()
	stats := &SyncStats{}

	// TODO: Implement delta sync
	// GET /api/v1/lore/delta?since={last_sync}

	stats.Duration = time.Since(start)
	return stats, nil
}

// Push pushes local changes to Engram
func (s *Syncer) Push(flush bool) (*SyncStats, error) {
	if s.engramURL == "" {
		return &SyncStats{}, nil
	}

	start := time.Now()
	stats := &SyncStats{}

	pending, err := s.store.GetPending()
	if err != nil {
		stats.Errors++
		return stats, err
	}

	if len(pending) == 0 {
		stats.Duration = time.Since(start)
		return stats, nil
	}

	// TODO: Batch and push lore
	// POST /api/v1/lore

	// For now, clear the queue
	if err := s.store.ClearPending(); err != nil {
		stats.Errors++
		return stats, err
	}

	stats.Pushed = len(pending)
	stats.Duration = time.Since(start)
	return stats, nil
}

// sendRequest sends an authenticated request to Engram
func (s *Syncer) sendRequest(method, path string, body interface{}) (*http.Response, error) {
	var reqBody *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(data)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req, err := http.NewRequest(method, s.engramURL+path, reqBody)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return s.client.Do(req)
}
