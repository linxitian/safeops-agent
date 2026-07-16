package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettingsStoreSaves0600AndLoadsConfig(t *testing.T) {
	store := NewSettingsStore(filepath.Join(t.TempDir(), "state", "llm_config.json"))
	stored, err := store.Save(Config{BaseURL: " https://llm.example/v1 ", APIKey: " secret-key ", Model: " ops-model "}, time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if stored.BaseURL != "https://llm.example/v1" || stored.APIKey != "secret-key" || stored.Model != "ops-model" {
		t.Fatalf("settings were not normalized: %+v", stored)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("settings permissions = %o, want 0600", info.Mode().Perm())
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config().APIKey != "secret-key" {
		t.Fatalf("stored key was not loaded: %+v", loaded)
	}
}

func TestSettingsStoreRejectsIncompleteAndUnsupportedSettings(t *testing.T) {
	store := NewSettingsStore(filepath.Join(t.TempDir(), "llm.json"))
	if _, err := store.Save(Config{BaseURL: "https://example.invalid/v1", Model: "m"}, time.Now()); err == nil {
		t.Fatal("incomplete settings were saved")
	}
	if err := os.WriteFile(store.path, []byte(`{"schema_version":2,"base_url":"https://example.invalid","api_key":"k","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported settings were accepted: %v", err)
	}
	if err := store.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("deleted settings load error = %v", err)
	}
}

func TestRuntimeProviderStatusAndDecision(t *testing.T) {
	runtime := NewRuntimeProvider()
	if _, err := runtime.Decide(context.Background(), DecisionRequest{Objective: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured provider error = %v", err)
	}
	server := newDecisionServer(t, `{"kind":"final","decision_summary":"完成","final_answer":"已有证据"}`)
	defer server.Close()
	if err := runtime.Configure(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()}, "test", time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	status := runtime.Status()
	if !status.Configured || !status.APIKeyConfigured || status.BaseURL != server.URL || status.Model != "model" || status.Source != "test" {
		t.Fatalf("unexpected public status: %+v", status)
	}
	decision, err := runtime.Decide(context.Background(), DecisionRequest{Objective: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionFinal {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	runtime.Clear()
	if runtime.Status().Configured {
		t.Fatal("runtime remained configured after clear")
	}
}

func newDecisionServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
}
