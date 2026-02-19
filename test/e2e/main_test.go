package e2e

import (
	"os"
	"os/exec"
	"testing"
)

var (
	engramBin string
	recallBin string
	tractBin  string
)

func TestMain(m *testing.M) {
	engramBin = envOrLookPath("ENGRAM_BIN", "engram")
	recallBin = envOrLookPath("RECALL_BIN", "recall")
	tractBin = envOrLookPath("TRACT_BIN", "tract")
	os.Exit(m.Run())
}

func envOrLookPath(envVar, name string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	return ""
}

func requireEngram(t *testing.T) {
	t.Helper()
	if engramBin == "" {
		t.Skip("engram binary not available (set ENGRAM_BIN or add to PATH)")
	}
}

func requireRecall(t *testing.T) {
	t.Helper()
	requireEngram(t)
	if recallBin == "" {
		t.Skip("recall binary not available (set RECALL_BIN or add to PATH)")
	}
}

func requireTract(t *testing.T) {
	t.Helper()
	requireEngram(t)
	if tractBin == "" {
		t.Skip("tract binary not available (set TRACT_BIN or add to PATH)")
	}
}
