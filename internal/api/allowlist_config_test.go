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
