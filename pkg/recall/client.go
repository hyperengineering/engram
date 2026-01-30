package recall

import (
	"errors"
	"sync"
	"time"
)

// Client is the Recall client for interacting with lore
type Client struct {
	config  Config
	store   *Store
	session *Session
	syncer  *Syncer

	mu       sync.RWMutex
	closed   bool
	syncDone chan struct{}
}

// New creates a new Recall client
func New(config Config) (*Client, error) {
	if config.LocalPath == "" {
		return nil, errors.New("LocalPath is required")
	}

	// Set defaults
	if config.SyncInterval == 0 {
		config.SyncInterval = 5 * time.Minute
	}

	store, err := NewStore(config.LocalPath)
	if err != nil {
		return nil, err
	}

	c := &Client{
		config:   config,
		store:    store,
		session:  NewSession(),
		syncer:   NewSyncer(config.EngramURL, config.APIKey, store),
		syncDone: make(chan struct{}),
	}

	return c, nil
}

// Initialize initializes the client and performs initial sync
func (c *Client) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("client is closed")
	}

	if !c.config.OfflineMode && c.config.EngramURL != "" {
		if err := c.syncer.Bootstrap(); err != nil {
			// Log but don't fail - offline mode
			_ = err
		}
	}

	// Start background sync if enabled
	if c.config.AutoSync && !c.config.OfflineMode {
		go c.syncLoop()
	}

	return nil
}

// Shutdown gracefully shuts down the client
func (c *Client) Shutdown() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	close(c.syncDone)

	// Final sync
	if !c.config.OfflineMode && c.config.EngramURL != "" {
		_, _ = c.syncer.Push(true)
	}

	return c.store.Close()
}

// Record captures lore from current experience
func (c *Client) Record(params RecordParams) (*Lore, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	if params.Confidence == 0 {
		params.Confidence = 0.7
	}

	return c.store.Record(params)
}

// Query retrieves relevant lore based on semantic similarity
func (c *Client) Query(params QueryParams) (*QueryResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	if params.K == 0 {
		params.K = 5
	}
	if params.MinConfidence == 0 {
		params.MinConfidence = 0.5
	}

	lore, err := c.store.Query(params)
	if err != nil {
		return nil, err
	}

	// Track in session
	sessionRefs := make(map[string]string)
	for _, l := range lore {
		ref := c.session.Track(l.ID)
		sessionRefs[ref] = l.ID
	}

	return &QueryResult{
		Lore:        lore,
		SessionRefs: sessionRefs,
	}, nil
}

// Feedback provides feedback on lore recalled this session
func (c *Client) Feedback(params FeedbackParams) (*FeedbackResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	result := &FeedbackResult{
		Updated: []FeedbackUpdate{},
	}

	// Process helpful feedback
	for _, ref := range params.Helpful {
		id, ok := c.session.Resolve(ref)
		if !ok {
			// Try fuzzy match by content
			id = c.session.FuzzyMatch(ref)
		}
		if id != "" {
			update, err := c.store.UpdateConfidence(id, 0.08)
			if err == nil {
				result.Updated = append(result.Updated, *update)
			}
		}
	}

	// Process incorrect feedback
	for _, ref := range params.Incorrect {
		id, ok := c.session.Resolve(ref)
		if !ok {
			id = c.session.FuzzyMatch(ref)
		}
		if id != "" {
			update, err := c.store.UpdateConfidence(id, -0.15)
			if err == nil {
				result.Updated = append(result.Updated, *update)
			}
		}
	}

	// not_relevant doesn't affect confidence

	return result, nil
}

// GetSessionLore returns all lore surfaced this session
func (c *Client) GetSessionLore() []SessionLore {
	return c.session.GetAll()
}

// SyncFromEngram pulls updates from Engram
func (c *Client) SyncFromEngram() (*SyncStats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	return c.syncer.Pull()
}

// SyncToEngram pushes local changes to Engram
func (c *Client) SyncToEngram() (*SyncStats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, errors.New("client is closed")
	}

	return c.syncer.Push(false)
}

// GetStats returns store statistics
func (c *Client) GetStats() StoreStats {
	return c.store.Stats()
}

// HealthCheck returns the health status
func (c *Client) HealthCheck() HealthStatus {
	status := HealthStatus{
		LocalStore:  true,
		CentralSync: false,
	}

	if c.config.OfflineMode {
		return status
	}

	if err := c.syncer.Ping(); err != nil {
		status.LastError = err.Error()
	} else {
		status.CentralSync = true
	}

	return status
}

func (c *Client) syncLoop() {
	ticker := time.NewTicker(c.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.syncDone:
			return
		case <-ticker.C:
			c.mu.RLock()
			if !c.closed {
				_, _ = c.syncer.Push(false)
				_, _ = c.syncer.Pull()
			}
			c.mu.RUnlock()
		}
	}
}
