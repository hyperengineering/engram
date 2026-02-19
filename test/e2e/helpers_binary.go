//go:build e2e

package e2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// engramServer manages a running Engram server process.
type engramServer struct {
	cmd     *exec.Cmd
	dataDir string
	address string
	apiKey  string
	logFile string
}

// startEngram launches the Engram binary and waits for it to become healthy.
// Engram is configured entirely via environment variables (no CLI flags).
func startEngram(t *testing.T) *engramServer {
	t.Helper()

	if engramBin == "" {
		t.Skip("engram binary not available")
	}

	dataDir := t.TempDir()
	apiKey := "e2e-test-api-key"
	port := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	logFile := fmt.Sprintf("%s/engram.log", dataDir)
	storesRoot := fmt.Sprintf("%s/stores", dataDir)
	dbPath := fmt.Sprintf("%s/engram.db", dataDir)

	cmd := exec.Command(engramBin)
	cmd.Env = append(os.Environ(),
		"ENGRAM_PORT="+fmt.Sprintf("%d", port),
		"ENGRAM_DB_PATH="+dbPath,
		"ENGRAM_API_KEY="+apiKey,
		"ENGRAM_STORES_ROOT="+storesRoot,
		"ENGRAM_CONFIG_PATH="+fmt.Sprintf("%s/nonexistent.yaml", dataDir), // skip YAML file
		"ENGRAM_DEV_MODE=true", // bypass OPENAI_API_KEY validation
	)

	lf, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create log file: %v", err)
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		t.Fatalf("start engram: %v", err)
	}

	s := &engramServer{
		cmd:     cmd,
		dataDir: dataDir,
		address: address,
		apiKey:  apiKey,
		logFile: logFile,
	}

	t.Cleanup(func() {
		s.stop()
		lf.Close()
	})

	if err := s.waitHealthy(10 * time.Second); err != nil {
		t.Fatalf("engram not healthy: %v", err)
	}

	return s
}

func (s *engramServer) stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(os.Interrupt)
		_ = s.cmd.Wait()
	}
}

func (s *engramServer) baseURL() string {
	return fmt.Sprintf("http://%s", s.address)
}

func (s *engramServer) createStore(t *testing.T, name, storeType string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/stores", s.baseURL())
	body := fmt.Sprintf(`{"store_id":"%s","type":"%s","description":"e2e test store"}`, name, storeType)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create store: status %d", resp.StatusCode)
	}
	return name
}

func (s *engramServer) storeDB(t *testing.T, storeID string) *sql.DB {
	t.Helper()
	dbPath := fmt.Sprintf("%s/stores/%s/engram.db", s.dataDir, storeID)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open store DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func (s *engramServer) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("%s/api/v1/health", s.baseURL())

	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("engram not healthy after %s", timeout)
}

// --- engramServer HTTP + DB helpers for E2E verification ---

// syncDeltaEntry mirrors a single entry in a sync delta response.
type syncDeltaEntry struct {
	Sequence  int64           `json:"sequence"`
	TableName string          `json:"table_name"`
	EntityID  string          `json:"entity_id"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	SourceID  string          `json:"source_id"`
}

type syncDeltaResponse struct {
	Entries        []syncDeltaEntry `json:"entries"`
	LastSequence   int64            `json:"last_sequence"`
	LatestSequence int64            `json:"latest_sequence"`
	HasMore        bool             `json:"has_more"`
}

// getDelta fetches the sync delta from the Engram API.
func (s *engramServer) getDelta(t *testing.T, storeID string, after int64) syncDeltaResponse {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/stores/%s/sync/delta?after=%d", s.baseURL(), storeID, after)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getDelta: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("getDelta: status %d: %s", resp.StatusCode, body)
	}
	var result syncDeltaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("getDelta decode: %v", err)
	}
	return result
}

// pushViaAPI pushes entries directly to Engram's sync push endpoint.
func (s *engramServer) pushViaAPI(t *testing.T, storeID string, entries []syncDeltaEntry, sourceID string) {
	t.Helper()
	type pushReq struct {
		PushID        string           `json:"push_id"`
		SourceID      string           `json:"source_id"`
		SchemaVersion int              `json:"schema_version"`
		Entries       []syncDeltaEntry `json:"entries"`
	}
	body := pushReq{
		PushID:        fmt.Sprintf("api-push-%d", time.Now().UnixNano()),
		SourceID:      sourceID,
		SchemaVersion: 2,
		Entries:       entries,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal push: %v", err)
	}
	url := fmt.Sprintf("%s/api/v1/stores/%s/sync/push", s.baseURL(), storeID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pushViaAPI: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("pushViaAPI: status %d: %s", resp.StatusCode, respBody)
	}
}

// generateSnapshot creates a snapshot of the store's database using VACUUM INTO.
func (s *engramServer) generateSnapshot(t *testing.T, storeID string) {
	t.Helper()
	storePath := filepath.Join(s.dataDir, "stores", storeID)
	dbPath := filepath.Join(storePath, "engram.db")
	snapshotDir := filepath.Join(storePath, "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("create snapshot dir: %v", err)
	}
	snapshotPath := filepath.Join(snapshotDir, "current.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open store for snapshot: %v", err)
	}
	defer db.Close()
	// VACUUM INTO creates a clean, self-contained copy (WAL-safe, non-blocking)
	_, err = db.Exec(fmt.Sprintf("VACUUM INTO '%s'", snapshotPath))
	if err != nil {
		t.Fatalf("generate snapshot: %v", err)
	}
}

// makeLorePayload builds a valid lore_entries payload for sync push.
func makeLorePayload(t *testing.T, id, content, category, sourceID string, confidence float64) json.RawMessage {
	t.Helper()
	payload := map[string]interface{}{
		"id":         id,
		"content":    content,
		"category":   category,
		"confidence": confidence,
		"source_id":  sourceID,
		"sources":    []string{sourceID},
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal lore payload: %v", err)
	}
	return json.RawMessage(b)
}

// recallCLI wraps the Recall CLI binary for E2E testing.
// Recall uses RECALL_HOME as its store root (defaults to ~/.recall/).
// Local DB lives at $RECALL_HOME/stores/<encoded-store-id>/lore.db.
type recallCLI struct {
	bin        string
	recallHome string // RECALL_HOME for test isolation
	engram     *engramServer
	storeID    string
	sourceID   string
}

func newRecallCLI(t *testing.T, engram *engramServer, storeID string) *recallCLI {
	t.Helper()
	if recallBin == "" {
		t.Skip("recall binary not available")
	}
	return &recallCLI{
		bin:        recallBin,
		recallHome: t.TempDir(),
		engram:     engram,
		storeID:    storeID,
		sourceID:   fmt.Sprintf("e2e-recall-%s", storeID),
	}
}

func (r *recallCLI) exec(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(r.bin, args...)
	cmd.Env = append(os.Environ(),
		"RECALL_HOME="+r.recallHome,
		"ENGRAM_URL="+r.engram.baseURL(),
		"ENGRAM_API_KEY="+r.engram.apiKey,
		"ENGRAM_STORE="+r.storeID,
		"RECALL_SOURCE_ID="+r.sourceID,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// record records a lore entry via the Recall CLI. Additional flags (e.g. --confidence, --context)
// can be passed as extra args.
func (r *recallCLI) record(t *testing.T, content, category string, extra ...string) string {
	t.Helper()
	args := []string{"record", "--store", r.storeID, "--content", content, "--category", category}
	args = append(args, extra...)
	out, err := r.exec(t, args...)
	if err != nil {
		t.Fatalf("recall record: %v\noutput: %s", err, out)
	}
	return out
}

// recordID records a lore entry and extracts the returned ULID from stdout.
func (r *recallCLI) recordID(t *testing.T, content, category string, extra ...string) string {
	t.Helper()
	out := r.record(t, content, category, extra...)
	// Parse "✓ Recorded: <ULID>" from output
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Recorded:") {
			parts := strings.SplitN(line, "Recorded:", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	t.Fatalf("could not extract record ID from output: %s", out)
	return ""
}

// syncPush pushes local changes to Engram.
func (r *recallCLI) syncPush(t *testing.T) string {
	t.Helper()
	out, err := r.exec(t, "sync", "push", "--store", r.storeID)
	if err != nil {
		t.Fatalf("recall sync push: %v\noutput: %s", err, out)
	}
	return out
}

// syncDelta pulls remote changes from Engram.
func (r *recallCLI) syncDelta(t *testing.T) string {
	t.Helper()
	out, err := r.exec(t, "sync", "delta", "--store", r.storeID)
	if err != nil {
		t.Fatalf("recall sync delta: %v\noutput: %s", err, out)
	}
	return out
}

// syncBootstrap downloads a full snapshot from Engram.
func (r *recallCLI) syncBootstrap(t *testing.T) string {
	t.Helper()
	out, err := r.exec(t, "sync", "bootstrap", "--store", r.storeID)
	if err != nil {
		t.Fatalf("recall sync bootstrap: %v\noutput: %s", err, out)
	}
	return out
}

// queryJSON runs a semantic query and returns the raw JSON output.
func (r *recallCLI) queryJSON(t *testing.T, query string) string {
	t.Helper()
	out, err := r.exec(t, "query", query, "--store", r.storeID, "--json")
	if err != nil {
		t.Fatalf("recall query: %v\noutput: %s", err, out)
	}
	return out
}

// feedback applies feedback on a lore entry. feedbackType is "helpful", "incorrect", or "not_relevant".
func (r *recallCLI) feedback(t *testing.T, id, feedbackType string) string {
	t.Helper()
	out, err := r.exec(t, "feedback", "--store", r.storeID, "--id", id, "--type", feedbackType)
	if err != nil {
		t.Fatalf("recall feedback: %v\noutput: %s", err, out)
	}
	return out
}

// tractCLI wraps the Tract CLI binary for E2E testing.
// Tract uses TRACT_HOME as its store root (defaults to ~/.tract/).
// Local DB lives at $TRACT_HOME/stores/{store-name}/tract.db.
type tractCLI struct {
	bin       string
	tractHome string // TRACT_HOME for test isolation
	engram    *engramServer
	storeID   string
}

func newTractCLI(t *testing.T, engram *engramServer, storeID string) *tractCLI {
	t.Helper()
	if tractBin == "" {
		t.Skip("tract binary not available")
	}
	return &tractCLI{
		bin:       tractBin,
		tractHome: t.TempDir(),
		engram:    engram,
		storeID:   storeID,
	}
}

func (tr *tractCLI) exec(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(tr.bin, args...)
	cmd.Env = append(os.Environ(),
		"TRACT_HOME="+tr.tractHome,
		"ENGRAM_URL="+tr.engram.baseURL(),
		"ENGRAM_API_KEY="+tr.engram.apiKey,
		"ENGRAM_STORE="+tr.storeID,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// initStore initializes a local Tract store.
func (tr *tractCLI) initStore(t *testing.T) string {
	t.Helper()
	out, err := tr.exec(t, "init", tr.storeID)
	if err != nil {
		t.Fatalf("tract init: %v\noutput: %s", err, out)
	}
	return out
}

// loadGoalTree loads a goal tree JSON file into the local store.
// The file must have {"version": "1", "goals": [...], ...} format.
func (tr *tractCLI) loadGoalTree(t *testing.T, filePath string) string {
	t.Helper()
	out, err := tr.exec(t, "load", "goal-tree", filePath, "--store", tr.storeID)
	if err != nil {
		t.Fatalf("tract load goal-tree: %v\noutput: %s", err, out)
	}
	return out
}

// syncPush pushes local changes to Engram.
func (tr *tractCLI) syncPush(t *testing.T) string {
	t.Helper()
	out, err := tr.exec(t, "sync", "push", "--store", tr.storeID)
	if err != nil {
		t.Fatalf("tract sync push: %v\noutput: %s", err, out)
	}
	return out
}

// syncPull pulls remote changes from Engram.
func (tr *tractCLI) syncPull(t *testing.T) string {
	t.Helper()
	out, err := tr.exec(t, "sync", "pull", "--store", tr.storeID)
	if err != nil {
		t.Fatalf("tract sync pull: %v\noutput: %s", err, out)
	}
	return out
}

// syncBootstrap downloads a full snapshot from Engram, replacing local data.
func (tr *tractCLI) syncBootstrap(t *testing.T) string {
	t.Helper()
	out, err := tr.exec(t, "sync", "bootstrap", "--store", tr.storeID)
	if err != nil {
		t.Fatalf("tract sync bootstrap: %v\noutput: %s", err, out)
	}
	return out
}

// syncStatusJSON returns the sync status as a parsed JSON map.
func (tr *tractCLI) syncStatusJSON(t *testing.T) map[string]interface{} {
	t.Helper()
	out, err := tr.exec(t, "sync", "status", "--store", tr.storeID)
	if err != nil {
		t.Fatalf("tract sync status: %v\noutput: %s", err, out)
	}
	// Extract JSON from output (may contain GORM debug lines before the JSON)
	jsonStart := strings.Index(out, "{")
	if jsonStart < 0 {
		t.Fatalf("no JSON found in sync status output: %s", out)
	}
	jsonEnd := strings.LastIndex(out, "}")
	if jsonEnd < 0 {
		t.Fatalf("no closing brace in sync status output: %s", out)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out[jsonStart:jsonEnd+1]), &result); err != nil {
		t.Fatalf("parse sync status JSON: %v\nraw: %s", err, out[jsonStart:jsonEnd+1])
	}
	return result
}

// writeGoalTreeFile creates a temporary goal tree JSON file and returns its path.
func (tr *tractCLI) writeGoalTreeFile(t *testing.T, goals []map[string]interface{}) string {
	t.Helper()
	tree := map[string]interface{}{
		"version": "1",
		"goals":   goals,
	}
	b, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal goal tree: %v", err)
	}
	path := filepath.Join(t.TempDir(), "goal-tree.json")
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("write goal tree file: %v", err)
	}
	return path
}

// freePort returns a free TCP port.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// --- Additional helpers for Layer 3 & 4 tests ---

// restartOnSameData stops the server and starts a new one using the same data directory.
// Returns a new engramServer with a new port but the same persisted data.
func (s *engramServer) restartOnSameData(t *testing.T) *engramServer {
	t.Helper()

	s.stop()
	time.Sleep(200 * time.Millisecond) // allow port release

	port := freePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	logFile := filepath.Join(s.dataDir, "engram-restart.log")

	cmd := exec.Command(engramBin)
	cmd.Env = append(os.Environ(),
		"ENGRAM_PORT="+fmt.Sprintf("%d", port),
		"ENGRAM_DB_PATH="+filepath.Join(s.dataDir, "engram.db"),
		"ENGRAM_API_KEY="+s.apiKey,
		"ENGRAM_STORES_ROOT="+filepath.Join(s.dataDir, "stores"),
		"ENGRAM_CONFIG_PATH="+filepath.Join(s.dataDir, "nonexistent.yaml"),
		"ENGRAM_DEV_MODE=true",
	)

	lf, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create restart log file: %v", err)
	}
	cmd.Stdout = lf
	cmd.Stderr = lf

	if err := cmd.Start(); err != nil {
		lf.Close()
		t.Fatalf("restart engram: %v", err)
	}

	newSrv := &engramServer{
		cmd:     cmd,
		dataDir: s.dataDir,
		address: address,
		apiKey:  s.apiKey,
		logFile: logFile,
	}

	t.Cleanup(func() {
		newSrv.stop()
		lf.Close()
	})

	if err := newSrv.waitHealthy(10 * time.Second); err != nil {
		t.Fatalf("restarted engram not healthy: %v", err)
	}

	return newSrv
}

// pushViaAPIWithResponse pushes entries and returns the full HTTP response details.
// Useful for testing idempotency headers and error responses.
func (s *engramServer) pushViaAPIWithResponse(t *testing.T, storeID, pushID string, entries []syncDeltaEntry, sourceID string) (int, []byte, http.Header) {
	t.Helper()
	type pushReq struct {
		PushID        string           `json:"push_id"`
		SourceID      string           `json:"source_id"`
		SchemaVersion int              `json:"schema_version"`
		Entries       []syncDeltaEntry `json:"entries"`
	}
	body := pushReq{
		PushID:        pushID,
		SourceID:      sourceID,
		SchemaVersion: 2,
		Entries:       entries,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal push: %v", err)
	}
	url := fmt.Sprintf("%s/api/v1/stores/%s/sync/push", s.baseURL(), storeID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pushViaAPIWithResponse: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, resp.Header
}

// pushViaAPIWithSchemaVersion pushes entries with a specific schema version.
// Used to test schema mismatch rejection.
func (s *engramServer) pushViaAPIWithSchemaVersion(t *testing.T, storeID string, schemaVersion int, entries []syncDeltaEntry, sourceID string) (int, []byte) {
	t.Helper()
	type pushReq struct {
		PushID        string           `json:"push_id"`
		SourceID      string           `json:"source_id"`
		SchemaVersion int              `json:"schema_version"`
		Entries       []syncDeltaEntry `json:"entries"`
	}
	body := pushReq{
		PushID:        fmt.Sprintf("push-%d", time.Now().UnixNano()),
		SourceID:      sourceID,
		SchemaVersion: schemaVersion,
		Entries:       entries,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal push: %v", err)
	}
	url := fmt.Sprintf("%s/api/v1/stores/%s/sync/push", s.baseURL(), storeID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pushViaAPIWithSchemaVersion: %v", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody
}

// legacyIngestLore ingests lore via the legacy POST /stores/{id}/lore endpoint.
func (s *engramServer) legacyIngestLore(t *testing.T, storeID, sourceID string, loreItems []map[string]interface{}) {
	t.Helper()
	body := map[string]interface{}{
		"source_id": sourceID,
		"lore":      loreItems,
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal legacy ingest: %v", err)
	}
	url := fmt.Sprintf("%s/api/v1/stores/%s/lore", s.baseURL(), storeID)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacyIngestLore: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("legacyIngestLore: status %d: %s", resp.StatusCode, respBody)
	}
}

// getDeltaWithLimit fetches delta with a specific limit parameter.
func (s *engramServer) getDeltaWithLimit(t *testing.T, storeID string, after int64, limit int) syncDeltaResponse {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/stores/%s/sync/delta?after=%d&limit=%d", s.baseURL(), storeID, after, limit)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getDeltaWithLimit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("getDeltaWithLimit: status %d: %s", resp.StatusCode, body)
	}
	var result syncDeltaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("getDeltaWithLimit decode: %v", err)
	}
	return result
}
