package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"safeops-agent/internal/llm"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
)

func TestLLMConfigAPISavesWithoutReturningSecret(t *testing.T) {
	fileStore, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := llm.NewRuntimeProvider()
	settings := llm.NewSettingsStore(fileStore.Root() + "/state/llm_config.json")
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithLLM(runtime, settings))

	save := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/llm/config", map[string]any{"base_url": "https://llm.example/v1", "api_key": "secret-key", "model": "ops-model"})
	if save.Code != http.StatusOK {
		t.Fatalf("save returned %d %s", save.Code, save.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(save.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["configured"] != true || saved["api_key_configured"] != true || saved["api_key"] != nil {
		t.Fatalf("unsafe or unexpected save response: %s", save.Body.String())
	}

	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/llm/config", nil))
	if get.Code != http.StatusOK || json.Valid(get.Body.Bytes()) == false {
		t.Fatalf("get returned %d %s", get.Code, get.Body.String())
	}
	if body := get.Body.String(); strings.Contains(body, "secret-key") || strings.Contains(body, `"api_key"`) {
		t.Fatalf("GET leaked API key material: %s", body)
	}
}

func TestLLMConfigAPIKeepsExistingSecretWhenKeyOmitted(t *testing.T) {
	fileStore, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := llm.NewRuntimeProvider()
	settings := llm.NewSettingsStore(fileStore.Root() + "/state/llm_config.json")
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithLLM(runtime, settings))

	first := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/llm/config", map[string]any{"base_url": "https://llm.example/v1", "api_key": "first-secret", "model": "old-model"})
	if first.Code != http.StatusOK {
		t.Fatalf("initial save returned %d %s", first.Code, first.Body.String())
	}
	second := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/llm/config", map[string]any{"base_url": "https://llm.example/v2", "api_key": "", "model": "new-model"})
	if second.Code != http.StatusOK {
		t.Fatalf("second save returned %d %s", second.Code, second.Body.String())
	}
	stored, err := settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "first-secret" || stored.Model != "new-model" || stored.BaseURL != "https://llm.example/v2" {
		t.Fatalf("existing secret was not preserved: %+v", stored)
	}

	clear := httptest.NewRecorder()
	server.Handler().ServeHTTP(clear, httptest.NewRequest(http.MethodDelete, "/api/v1/llm/config", nil))
	if clear.Code != http.StatusOK {
		t.Fatalf("clear returned %d %s", clear.Code, clear.Body.String())
	}
	if _, err := runtime.Decide(context.Background(), llm.DecisionRequest{Objective: "test"}); err == nil {
		t.Fatal("runtime provider remained configured after clear")
	}
}

func TestLLMConfigAPIRejectsMissingSecretWithoutExistingConfig(t *testing.T) {
	fileStore, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithLLM(llm.NewRuntimeProvider(), llm.NewSettingsStore(fileStore.Root()+"/state/llm_config.json")))
	response := requestJSON(t, server.Handler(), http.MethodPut, "/api/v1/llm/config", map[string]any{"base_url": "https://llm.example/v1", "model": "model"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing secret returned %d %s", response.Code, response.Body.String())
	}
}
