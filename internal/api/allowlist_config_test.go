package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
)

func TestExecutorAllowlistAPIUpdatesConfigAndServerTargets(t *testing.T) {
	root := t.TempDir()
	fileStore, err := storage.NewFileStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	lab := filepath.Join(root, "lab")
	labA := filepath.Join(lab, "a")
	labB := filepath.Join(lab, "b")
	quarantine := filepath.Join(root, "quarantine")
	for _, dir := range []string{labA, labB, quarantine} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "executor.yaml")
	statePath := filepath.Join(root, "state", "executor_allowlist.yaml")
	initial := executor.Config{SchemaVersion: 1, AllowedFileRoots: []string{lab, quarantine}, LabFileRoots: []string{lab}, QuarantineRoot: quarantine}
	if err := executor.SaveConfig(configPath, initial); err != nil {
		t.Fatal(err)
	}
	staticConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := executor.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	targets := executor.NewMutableTargets(platform.NewLinux(), platform.NewCommandPlatform(), loaded.AllowedFileRoots)
	manager, err := executor.NewConfigManager(statePath, loaded, targets)
	if err != nil {
		t.Fatal(err)
	}
	server := New(fileStore, registry.New(registry.Config{}), &agent.Orchestrator{Actions: &agent.ActionPreparer{}, FileTargets: targets}, nil, WithExecutorAllowlist(manager))

	get := requestJSON(t, server.Handler(), http.MethodGet, "/api/v1/executor/allowlist", map[string]any{})
	if get.Code != http.StatusOK {
		t.Fatalf("GET returned %d %s", get.Code, get.Body.String())
	}
	var status executor.AllowlistStatus
	if err := json.Unmarshal(get.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.WriteActionsEnabled || len(status.ManagedRoots) != 1 || status.ManagedRoots[0] != lab || status.RequiresExecutorRestart {
		t.Fatalf("unexpected initial status: %+v", status)
	}
	if len(status.CandidateRoots) != 3 || status.CandidateRoots[0] != lab || status.CandidateRoots[1] != labA || status.CandidateRoots[2] != labB {
		t.Fatalf("unexpected candidate roots: %+v", status.CandidateRoots)
	}

	put := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/executor/allowlist", map[string]any{"managed_roots": []string{labB}})
	if put.Code != http.StatusOK {
		t.Fatalf("PUT returned %d %s", put.Code, put.Body.String())
	}
	unchangedConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchangedConfig) != string(staticConfig) {
		t.Fatal("root-owned executor config was modified by the API")
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("mutable allowlist state was not persisted: %v", err)
	}
	if _, err := targets.SnapshotNewFile(context.Background(), filepath.Join(labB, "created.txt"), filepath.Join(labB, "created.txt")); err != nil {
		t.Fatalf("server targets did not pick up updated allowlist: %v", err)
	}
}

func TestExecutorAllowlistAPIRejectsUnsafeRoots(t *testing.T) {
	root := t.TempDir()
	fileStore, err := storage.NewFileStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	lab := filepath.Join(root, "lab")
	quarantine := filepath.Join(root, "quarantine")
	for _, dir := range []string{lab, quarantine} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "executor.yaml")
	initial := executor.Config{SchemaVersion: 1, AllowedFileRoots: []string{lab, quarantine}, LabFileRoots: []string{lab}, QuarantineRoot: quarantine}
	if err := executor.SaveConfig(configPath, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := executor.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := executor.NewConfigManager(filepath.Join(root, "state", "executor_allowlist.yaml"), loaded, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithExecutorAllowlist(manager))

	for _, bad := range [][]string{{"relative/path"}, {"/"}, {quarantine}, {root}, {filepath.Join(root, "outside")}} {
		response := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/executor/allowlist", map[string]any{"managed_roots": bad})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsafe roots %v returned %d %s", bad, response.Code, response.Body.String())
		}
	}
}

func TestExecutorPathBrowserSeparatesReadAndWriteRoots(t *testing.T) {
	root := t.TempDir()
	fileStore, err := storage.NewFileStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	readRoot := filepath.Join(root, "read-root")
	lab := filepath.Join(root, "lab")
	quarantine := filepath.Join(root, "quarantine")
	for _, dir := range []string{filepath.Join(readRoot, "logs"), lab, quarantine} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "executor.yaml")
	initial := executor.Config{SchemaVersion: 1, ReadFileRoots: []string{readRoot}, AllowedFileRoots: []string{lab, quarantine}, LabFileRoots: []string{lab}, QuarantineRoot: quarantine}
	if err := executor.SaveConfig(configPath, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := executor.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := executor.NewConfigManager(filepath.Join(root, "state", "executor_allowlist.yaml"), loaded, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithExecutorAllowlist(manager))

	readResponse := requestJSON(t, server.Handler(), http.MethodGet, "/api/v1/executor/path-browser?mode=read&path="+readRoot, map[string]any{})
	if readResponse.Code != http.StatusOK {
		t.Fatalf("read browser returned %d %s", readResponse.Code, readResponse.Body.String())
	}
	var readBrowser executor.PathBrowser
	if err := json.Unmarshal(readResponse.Body.Bytes(), &readBrowser); err != nil {
		t.Fatal(err)
	}
	if !readBrowser.CanSelectRead || readBrowser.CanSelectWrite || len(readBrowser.Entries) != 1 || readBrowser.Entries[0].Name != "logs" {
		t.Fatalf("unexpected read browser: %+v", readBrowser)
	}

	writeResponse := requestJSON(t, server.Handler(), http.MethodGet, "/api/v1/executor/path-browser?mode=write&path="+lab, map[string]any{})
	if writeResponse.Code != http.StatusOK {
		t.Fatalf("write browser returned %d %s", writeResponse.Code, writeResponse.Body.String())
	}
	var writeBrowser executor.PathBrowser
	if err := json.Unmarshal(writeResponse.Body.Bytes(), &writeBrowser); err != nil {
		t.Fatal(err)
	}
	if !writeBrowser.CanSelectWrite || !writeBrowser.CanCreateChild {
		t.Fatalf("unexpected write browser: %+v", writeBrowser)
	}

	missingRoot := filepath.Join(lab, "missing")
	missingResponse := requestJSON(t, server.Handler(), http.MethodGet, "/api/v1/executor/path-browser?mode=write&path="+missingRoot, map[string]any{})
	if missingResponse.Code != http.StatusOK {
		t.Fatalf("missing write root browser returned %d %s", missingResponse.Code, missingResponse.Body.String())
	}
	var missingBrowser executor.PathBrowser
	if err := json.Unmarshal(missingResponse.Body.Bytes(), &missingBrowser); err != nil {
		t.Fatal(err)
	}
	if !missingBrowser.WriteRootMissing || missingBrowser.CanCreateChild {
		t.Fatalf("unexpected missing write root browser: %+v", missingBrowser)
	}

	createResponse := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/executor/path-browser/directories", map[string]any{"parent": lab, "name": "created"})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create returned %d %s", createResponse.Code, createResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(lab, "created")); err != nil {
		t.Fatalf("directory was not created: %v", err)
	}

	denied := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/executor/path-browser/directories", map[string]any{"parent": readRoot, "name": "blocked"})
	if denied.Code != http.StatusBadRequest {
		t.Fatalf("create outside write roots returned %d %s", denied.Code, denied.Body.String())
	}
}
