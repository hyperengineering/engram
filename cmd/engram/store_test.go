package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperengineering/engram/internal/plugin"
)

// executeStoreCmd executes a store subcommand with captured output.
// It resets plugins for test isolation and uses --root to isolate filesystem state.
func executeStoreCmd(t *testing.T, rootPath string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	// Reset plugins for test isolation (Register panics on duplicate).
	// resolveStoreManager() calls initPlugins(), so we only need Reset() here.
	plugin.Reset()

	// Reset package-level flag variables to their defaults.
	// Cobra parses into these variables, so stale values from previous tests
	// would leak if not reset.
	storeRootOverride = ""
	storeJSONOutput = false
	createType = ""
	createDescription = ""
	createIfNotExists = false
	deleteForce = false

	// Build full args: "store" + subcommand args + "--root" + rootPath
	fullArgs := append([]string{"store"}, args...)
	fullArgs = append(fullArgs, "--root", rootPath)

	// Capture output
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs(fullArgs)

	err = rootCmd.Execute()

	// Reset output to defaults after execution
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)

	return outBuf.String(), errBuf.String(), err
}

// executeStoreCmdWithStdin executes a store subcommand with piped stdin.
func executeStoreCmdWithStdin(t *testing.T, rootPath string, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	plugin.Reset()

	storeRootOverride = ""
	storeJSONOutput = false
	createType = ""
	createDescription = ""
	createIfNotExists = false
	deleteForce = false

	fullArgs := append([]string{"store"}, args...)
	fullArgs = append(fullArgs, "--root", rootPath)

	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs(fullArgs)
	rootCmd.SetIn(strings.NewReader(stdin))

	err = rootCmd.Execute()

	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	rootCmd.SetArgs(nil)
	rootCmd.SetIn(nil)

	return outBuf.String(), errBuf.String(), err
}

// --- Create Tests ---

func TestStoreCreate_Defaults(t *testing.T) {
	root := t.TempDir()
	stdout, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, `Created store "my-project"`) {
		t.Errorf("stdout = %q, want it to contain 'Created store \"my-project\"'", stdout)
	}
	if !strings.Contains(stdout, "recall") {
		t.Errorf("stdout = %q, want it to contain 'recall'", stdout)
	}

	// Verify store directory was created
	if _, err := os.Stat(filepath.Join(root, "my-project", "meta.yaml")); os.IsNotExist(err) {
		t.Error("store directory with meta.yaml was not created")
	}
}

func TestStoreCreate_WithTypeAndDescription(t *testing.T) {
	root := t.TempDir()
	stdout, _, err := executeStoreCmd(t, root, "create", "my-project", "--type", "tract", "--description", "My project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, "tract") {
		t.Errorf("stdout = %q, want it to contain 'tract'", stdout)
	}
}

func TestStoreCreate_NestedID(t *testing.T) {
	root := t.TempDir()
	stdout, _, err := executeStoreCmd(t, root, "create", "org/team/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, `Created store "org/team/project"`) {
		t.Errorf("stdout = %q, want it to contain 'Created store \"org/team/project\"'", stdout)
	}

	// Verify nested directory structure
	if _, err := os.Stat(filepath.Join(root, "org", "team", "project", "meta.yaml")); os.IsNotExist(err) {
		t.Error("nested store directory was not created")
	}
}

func TestStoreCreate_DuplicateFails(t *testing.T) {
	root := t.TempDir()

	// Create the store first
	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}

	// Try to create again
	_, _, err = executeStoreCmd(t, root, "create", "my-project")
	if err == nil {
		t.Fatal("expected error for duplicate store, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to contain 'already exists'", err.Error())
	}
}

func TestStoreCreate_DuplicateWithIfNotExists(t *testing.T) {
	root := t.TempDir()

	// Create the store first
	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}

	// Create again with --if-not-exists
	_, stderr, err := executeStoreCmd(t, root, "create", "my-project", "--if-not-exists")
	if err != nil {
		t.Fatalf("unexpected error with --if-not-exists: %v", err)
	}
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("stderr = %q, want it to contain 'already exists'", stderr)
	}
}

func TestStoreCreate_InvalidID(t *testing.T) {
	root := t.TempDir()
	_, _, err := executeStoreCmd(t, root, "create", "Invalid/ID")
	if err == nil {
		t.Fatal("expected error for invalid store ID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid store ID") {
		t.Errorf("error = %q, want it to contain 'invalid store ID'", err.Error())
	}
}

func TestStoreCreate_JSONOutput(t *testing.T) {
	root := t.TempDir()
	stdout, _, err := executeStoreCmd(t, root, "create", "my-project", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	if result["id"] != "my-project" {
		t.Errorf("JSON id = %v, want 'my-project'", result["id"])
	}
	if result["type"] != "recall" {
		t.Errorf("JSON type = %v, want 'recall'", result["type"])
	}
	if _, ok := result["created"]; !ok {
		t.Error("JSON missing 'created' field")
	}
}

func TestStoreCreate_JSONOutputIfNotExists(t *testing.T) {
	root := t.TempDir()

	// Create store first
	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: unexpected error: %v", err)
	}

	// Create with --if-not-exists --json
	stdout, _, err := executeStoreCmd(t, root, "create", "my-project", "--if-not-exists", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	if result["already_existed"] != true {
		t.Errorf("JSON already_existed = %v, want true", result["already_existed"])
	}
}

// --- List Tests ---

func TestStoreList_Empty(t *testing.T) {
	root := t.TempDir()
	stdout, _, err := executeStoreCmd(t, root, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, "No stores found.") {
		t.Errorf("stdout = %q, want it to contain 'No stores found.'", stdout)
	}
}

func TestStoreList_MultipleStores(t *testing.T) {
	root := t.TempDir()

	// Create stores
	for _, id := range []string{"default", "project-a", "org/project-b"} {
		_, _, err := executeStoreCmd(t, root, "create", id)
		if err != nil {
			t.Fatalf("setup: create %q: %v", id, err)
		}
	}

	stdout, _, err := executeStoreCmd(t, root, "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check all IDs present
	for _, id := range []string{"default", "project-a", "org/project-b"} {
		if !strings.Contains(stdout, id) {
			t.Errorf("stdout missing store %q:\n%s", id, stdout)
		}
	}

	// Check header
	if !strings.Contains(stdout, "ID") || !strings.Contains(stdout, "TYPE") {
		t.Errorf("stdout missing table header:\n%s", stdout)
	}

	// Check sorted: default should come before org/project-b which comes before project-a
	defaultIdx := strings.Index(stdout, "default")
	orgIdx := strings.Index(stdout, "org/project-b")
	projectIdx := strings.Index(stdout, "project-a")
	if defaultIdx >= orgIdx || orgIdx >= projectIdx {
		t.Errorf("stores not sorted alphabetically:\n%s", stdout)
	}
}

func TestStoreList_JSONOutput(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout, _, err := executeStoreCmd(t, root, "list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	stores, ok := result["stores"].([]any)
	if !ok {
		t.Fatalf("JSON 'stores' field missing or not an array")
	}
	if len(stores) != 1 {
		t.Errorf("JSON stores count = %d, want 1", len(stores))
	}

	total, ok := result["total"].(float64) // JSON numbers are float64
	if !ok {
		t.Fatal("JSON 'total' field missing")
	}
	if int(total) != 1 {
		t.Errorf("JSON total = %v, want 1", total)
	}
}

func TestStoreList_JSONOutputEmpty(t *testing.T) {
	root := t.TempDir()

	stdout, _, err := executeStoreCmd(t, root, "list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	stores, ok := result["stores"].([]any)
	if !ok {
		t.Fatalf("JSON 'stores' field missing or not an array")
	}
	if len(stores) != 0 {
		t.Errorf("JSON stores count = %d, want 0", len(stores))
	}

	total, ok := result["total"].(float64)
	if !ok {
		t.Fatal("JSON 'total' field missing")
	}
	if int(total) != 0 {
		t.Errorf("JSON total = %v, want 0", total)
	}
}

// --- Info Tests ---

func TestStoreInfo_Existing(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project", "--description", "Test store")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout, _, err := executeStoreCmd(t, root, "info", "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"Store:         my-project",
		"Type:          recall",
		"Description:   Test store",
		"Path:",
	}
	for _, check := range checks {
		if !strings.Contains(stdout, check) {
			t.Errorf("stdout missing %q:\n%s", check, stdout)
		}
	}
}

func TestStoreInfo_Nonexistent(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "info", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent store, got nil")
	}
	if !strings.Contains(err.Error(), "store not found") {
		t.Errorf("error = %q, want it to contain 'store not found'", err.Error())
	}
}

func TestStoreInfo_JSONOutput(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project", "--description", "Test store")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout, _, err := executeStoreCmd(t, root, "info", "my-project", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	if result["id"] != "my-project" {
		t.Errorf("JSON id = %v, want 'my-project'", result["id"])
	}
	if result["type"] != "recall" {
		t.Errorf("JSON type = %v, want 'recall'", result["type"])
	}
	if result["description"] != "Test store" {
		t.Errorf("JSON description = %v, want 'Test store'", result["description"])
	}
	if _, ok := result["path"]; !ok {
		t.Error("JSON missing 'path' field")
	}
	if _, ok := result["schema_version"]; !ok {
		t.Error("JSON missing 'schema_version' field")
	}
}

// --- Delete Tests ---

func TestStoreDelete_WithForce(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout, _, err := executeStoreCmd(t, root, "delete", "my-project", "--force")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, `Deleted store "my-project"`) {
		t.Errorf("stdout = %q, want it to contain 'Deleted store \"my-project\"'", stdout)
	}

	// Verify store directory was removed
	if _, err := os.Stat(filepath.Join(root, "my-project")); !os.IsNotExist(err) {
		t.Error("store directory still exists after deletion")
	}
}

func TestStoreDelete_DefaultStoreRejected(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "delete", "default", "--force")
	if err == nil {
		t.Fatal("expected error for deleting default store, got nil")
	}
	if !strings.Contains(err.Error(), "cannot delete the default store") {
		t.Errorf("error = %q, want it to contain 'cannot delete the default store'", err.Error())
	}
}

func TestStoreDelete_Nonexistent(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "delete", "nonexistent", "--force")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent store, got nil")
	}
	if !strings.Contains(err.Error(), "store not found") {
		t.Errorf("error = %q, want it to contain 'store not found'", err.Error())
	}
}

func TestStoreDelete_JSONOutput(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	stdout, _, err := executeStoreCmd(t, root, "delete", "my-project", "--force", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout)
	}

	if result["id"] != "my-project" {
		t.Errorf("JSON id = %v, want 'my-project'", result["id"])
	}
	if result["deleted"] != true {
		t.Errorf("JSON deleted = %v, want true", result["deleted"])
	}
}

func TestStoreDelete_InteractiveConfirmation(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Provide correct confirmation via stdin
	stdout, _, err := executeStoreCmdWithStdin(t, root, "my-project\n", "delete", "my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(stdout, `Deleted store "my-project"`) {
		t.Errorf("stdout = %q, want it to contain 'Deleted store \"my-project\"'", stdout)
	}

	// Verify deletion
	if _, err := os.Stat(filepath.Join(root, "my-project")); !os.IsNotExist(err) {
		t.Error("store directory still exists after confirmed deletion")
	}
}

func TestStoreDelete_InteractiveAbort(t *testing.T) {
	root := t.TempDir()

	_, _, err := executeStoreCmd(t, root, "create", "my-project")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Provide wrong confirmation
	_, stderr, err := executeStoreCmdWithStdin(t, root, "wrong\n", "delete", "my-project")
	if err != nil {
		t.Fatalf("unexpected error (abort should not be an error): %v", err)
	}

	if !strings.Contains(stderr, "Aborted") {
		t.Errorf("stderr = %q, want it to contain 'Aborted'", stderr)
	}

	// Verify store still exists
	if _, err := os.Stat(filepath.Join(root, "my-project", "meta.yaml")); os.IsNotExist(err) {
		t.Error("store directory should still exist after aborted deletion")
	}
}

// --- Config Resolution Tests ---

func TestStoreConfig_RootFlagOverrides(t *testing.T) {
	root := t.TempDir()

	// The --root flag is already being passed by executeStoreCmd.
	// If it works (stores go to root), then --root is working.
	_, _, err := executeStoreCmd(t, root, "create", "test-override")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify store is in the custom root
	if _, err := os.Stat(filepath.Join(root, "test-override", "meta.yaml")); os.IsNotExist(err) {
		t.Error("store was not created in --root path")
	}
}

func TestStoreConfig_NoAPIKeyRequired(t *testing.T) {
	root := t.TempDir()

	// Unset API keys to verify they're not required
	originalOpenAI := os.Getenv("OPENAI_API_KEY")
	originalEngram := os.Getenv("ENGRAM_API_KEY")
	originalDevMode := os.Getenv("ENGRAM_DEV_MODE")
	os.Unsetenv("OPENAI_API_KEY")
	os.Unsetenv("ENGRAM_API_KEY")
	os.Unsetenv("ENGRAM_DEV_MODE")
	defer func() {
		if originalOpenAI != "" {
			os.Setenv("OPENAI_API_KEY", originalOpenAI)
		}
		if originalEngram != "" {
			os.Setenv("ENGRAM_API_KEY", originalEngram)
		}
		if originalDevMode != "" {
			os.Setenv("ENGRAM_DEV_MODE", originalDevMode)
		}
	}()

	stdout, _, err := executeStoreCmd(t, root, "list")
	if err != nil {
		t.Fatalf("store list should work without API keys, got error: %v", err)
	}

	if !strings.Contains(stdout, "No stores found.") {
		t.Errorf("stdout = %q, want 'No stores found.'", stdout)
	}
}

// --- formatSize Tests ---

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{2203648, "2.1 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		got := formatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}
