package recall

import (
	"testing"
)

func TestSession_Track(t *testing.T) {
	s := NewSession()

	ref1 := s.Track("id-1")
	if ref1 != "L1" {
		t.Errorf("Expected L1, got %s", ref1)
	}

	ref2 := s.Track("id-2")
	if ref2 != "L2" {
		t.Errorf("Expected L2, got %s", ref2)
	}

	// Same ID should return same ref
	ref1Again := s.Track("id-1")
	if ref1Again != "L1" {
		t.Errorf("Expected L1 for same ID, got %s", ref1Again)
	}
}

func TestSession_Resolve(t *testing.T) {
	s := NewSession()

	s.Track("id-1")
	s.Track("id-2")

	id, ok := s.Resolve("L1")
	if !ok || id != "id-1" {
		t.Errorf("Expected id-1, got %s (ok=%v)", id, ok)
	}

	id, ok = s.Resolve("L2")
	if !ok || id != "id-2" {
		t.Errorf("Expected id-2, got %s (ok=%v)", id, ok)
	}

	_, ok = s.Resolve("L99")
	if ok {
		t.Error("Expected not found for L99")
	}
}

func TestSession_FuzzyMatch(t *testing.T) {
	s := NewSession()

	s.TrackWithContent("id-1", "Queue consumer idempotency is important")
	s.TrackWithContent("id-2", "Message broker confirmation patterns")

	id := s.FuzzyMatch("idempotency")
	if id != "id-1" {
		t.Errorf("Expected id-1, got %s", id)
	}

	id = s.FuzzyMatch("message broker")
	if id != "id-2" {
		t.Errorf("Expected id-2, got %s", id)
	}

	id = s.FuzzyMatch("nonexistent")
	if id != "" {
		t.Errorf("Expected empty, got %s", id)
	}
}

func TestSession_Clear(t *testing.T) {
	s := NewSession()

	s.Track("id-1")
	s.Track("id-2")

	s.Clear()

	_, ok := s.Resolve("L1")
	if ok {
		t.Error("Expected session to be cleared")
	}

	ref := s.Track("id-3")
	if ref != "L1" {
		t.Errorf("Expected counter reset, got %s", ref)
	}
}
