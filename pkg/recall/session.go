package recall

import (
	"fmt"
	"strings"
	"sync"
)

// Session tracks lore surfaced during a session
type Session struct {
	mu       sync.RWMutex
	refs     map[string]string // L1 -> lore ID
	contents map[string]string // lore ID -> content snippet
	counter  int
}

// NewSession creates a new Session
func NewSession() *Session {
	return &Session{
		refs:     make(map[string]string),
		contents: make(map[string]string),
	}
}

// Track tracks a lore entry and returns its session reference
func (s *Session) Track(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already tracked
	for ref, existingID := range s.refs {
		if existingID == id {
			return ref
		}
	}

	s.counter++
	ref := fmt.Sprintf("L%d", s.counter)
	s.refs[ref] = id
	return ref
}

// TrackWithContent tracks a lore entry with its content for fuzzy matching
func (s *Session) TrackWithContent(id, content string) string {
	ref := s.Track(id)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store first 100 chars for fuzzy matching
	if len(content) > 100 {
		content = content[:100]
	}
	s.contents[id] = strings.ToLower(content)

	return ref
}

// Resolve resolves a session reference to a lore ID
func (s *Session) Resolve(ref string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.refs[ref]
	return id, ok
}

// FuzzyMatch attempts to find a lore ID by content snippet
func (s *Session) FuzzyMatch(snippet string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snippet = strings.ToLower(snippet)

	for id, content := range s.contents {
		if strings.Contains(content, snippet) {
			return id
		}
	}

	return ""
}

// GetAll returns all session lore entries
func (s *Session) GetAll() []SessionLore {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]SessionLore, 0, len(s.refs))
	for ref, id := range s.refs {
		content := s.contents[id]
		result = append(result, SessionLore{
			SessionRef: ref,
			ID:         id,
			Content:    content,
			Source:     "query",
		})
	}

	return result
}

// Clear clears the session
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refs = make(map[string]string)
	s.contents = make(map[string]string)
	s.counter = 0
}
