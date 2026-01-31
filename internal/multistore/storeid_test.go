package multistore

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateStoreID_Valid_Simple(t *testing.T) {
	err := ValidateStoreID("default")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Valid_TwoLevel(t *testing.T) {
	err := ValidateStoreID("org/project")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Valid_MaxLevels(t *testing.T) {
	err := ValidateStoreID("a/b/c/d")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Valid_WithHyphens(t *testing.T) {
	err := ValidateStoreID("my-org/my-project")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Valid_SingleChar(t *testing.T) {
	err := ValidateStoreID("a")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Valid_Numeric(t *testing.T) {
	err := ValidateStoreID("project1/v2")
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateStoreID_Invalid_Empty(t *testing.T) {
	err := ValidateStoreID("")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error message to contain 'empty', got %q", err.Error())
	}
}

func TestValidateStoreID_Invalid_Uppercase(t *testing.T) {
	err := ValidateStoreID("MyProject")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_Underscore(t *testing.T) {
	err := ValidateStoreID("my_project")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_LeadingHyphen(t *testing.T) {
	err := ValidateStoreID("-project")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_TrailingHyphen(t *testing.T) {
	err := ValidateStoreID("project-")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_TooManyLevels(t *testing.T) {
	err := ValidateStoreID("a/b/c/d/e")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "4 path segments") {
		t.Errorf("expected error message to contain '4 path segments', got %q", err.Error())
	}
}

func TestValidateStoreID_Invalid_TooLong(t *testing.T) {
	longID := strings.Repeat("a", 129)
	err := ValidateStoreID(longID)
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "128 characters") {
		t.Errorf("expected error message to contain '128 characters', got %q", err.Error())
	}
}

func TestValidateStoreID_Invalid_EmptySegment(t *testing.T) {
	err := ValidateStoreID("org//project")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "empty segment") {
		t.Errorf("expected error message to contain 'empty segment', got %q", err.Error())
	}
}

func TestValidateStoreID_Invalid_SpecialChars(t *testing.T) {
	testCases := []string{
		"org/project@1",
		"org/project.v1",
		"org/project!",
		"org/project#1",
		"org/project$1",
		"org/project%1",
		"org/project^1",
		"org/project&1",
		"org/project*1",
		"org/project 1",
	}
	for _, tc := range testCases {
		err := ValidateStoreID(tc)
		if !errors.Is(err, ErrInvalidStoreID) {
			t.Errorf("expected ErrInvalidStoreID for %q, got %v", tc, err)
		}
	}
}

func TestValidateStoreID_Valid_ExactlyMaxLength(t *testing.T) {
	// Exactly 128 characters should be valid
	longID := strings.Repeat("a", 128)
	err := ValidateStoreID(longID)
	if err != nil {
		t.Errorf("expected nil for 128 char ID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_LeadingSlash(t *testing.T) {
	err := ValidateStoreID("/project")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestValidateStoreID_Invalid_TrailingSlash(t *testing.T) {
	err := ValidateStoreID("project/")
	if !errors.Is(err, ErrInvalidStoreID) {
		t.Errorf("expected ErrInvalidStoreID, got %v", err)
	}
}

func TestIsDefaultStore(t *testing.T) {
	if !IsDefaultStore("default") {
		t.Error("expected IsDefaultStore('default') to return true")
	}
	if IsDefaultStore("Default") {
		t.Error("expected IsDefaultStore('Default') to return false")
	}
	if IsDefaultStore("other") {
		t.Error("expected IsDefaultStore('other') to return false")
	}
	if IsDefaultStore("") {
		t.Error("expected IsDefaultStore('') to return false")
	}
}

func TestConstants(t *testing.T) {
	if MaxStoreIDLength != 128 {
		t.Errorf("expected MaxStoreIDLength = 128, got %d", MaxStoreIDLength)
	}
	if MaxStoreIDSegments != 4 {
		t.Errorf("expected MaxStoreIDSegments = 4, got %d", MaxStoreIDSegments)
	}
	if DefaultStoreID != "default" {
		t.Errorf("expected DefaultStoreID = 'default', got %q", DefaultStoreID)
	}
}
