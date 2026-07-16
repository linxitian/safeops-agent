package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleDecisionContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatalf("unexpected request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "test-model" || request["response_format"].(map[string]any)["type"] != "json_object" {
			t.Fatalf("unexpected payload: %#v", request)
		}
		content := `{"kind":"tool","decision_summary":"读取负载确认现状","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observation":"结构化负载"}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
	defer server.Close()
	provider, err := NewOpenAICompatible(Config{BaseURL: server.URL + "/v1", APIKey: "test-secret", Model: "test-model", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := provider.Decide(context.Background(), DecisionRequest{Objective: "查看负载", OriginalRequest: "查看负载", Tools: []ToolCapability{{ServerID: "system", Name: "system.get_load_average", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionTool || decision.Tool != "system.get_load_average" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestOpenAICompatibleRejectsMalformedOrUnsafeDecision(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field": `{"kind":"tool","decision_summary":"x","server_id":"x","tool":"x","arguments":{},"shell":"rm -rf /"}`,
		"unknown kind":  `{"kind":"shell","decision_summary":"x","final_answer":"x"}`,
		"mixed final":   `{"kind":"final","decision_summary":"x","tool":"system.get_overview","final_answer":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
			}))
			defer server.Close()
			provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
			if _, err := provider.Decide(context.Background(), DecisionRequest{Objective: "test"}); err == nil {
				t.Fatal("unsafe decision accepted")
			}
		})
	}
}

func TestOpenAICompatibleDoesNotReflectErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("api_key=super-secret"))
	}))
	defer server.Close()
	provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
	_, err := provider.Decide(context.Background(), DecisionRequest{Objective: "test"})
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("unsafe provider error: %v", err)
	}
}

func TestConfigFromEnvRequiresAllVariables(t *testing.T) {
	t.Setenv("SAFEOPS_LLM_BASE_URL", "https://example.invalid/v1")
	t.Setenv("SAFEOPS_LLM_API_KEY", "")
	t.Setenv("SAFEOPS_LLM_MODEL", "model")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("partial environment accepted")
	}
}

func TestConfigFromEnvReturnsNotConfiguredWhenMissing(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("SAFEOPS_LLM_BASE_URL", "")
	t.Setenv("SAFEOPS_LLM_API_KEY", "")
	t.Setenv("SAFEOPS_LLM_MODEL", "")
	t.Setenv("SAFEOPS_LLM_TIMEOUT_SECONDS", "")
	if _, err := ConfigFromEnv(); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

func TestConfigFromDotEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	content := strings.Join([]string{
		"SAFEOPS_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4",
		"SAFEOPS_LLM_API_KEY=placeholder-key",
		"SAFEOPS_LLM_MODEL=glm-4.5-air",
		"SAFEOPS_LLM_TIMEOUT_SECONDS=120",
		"",
	}, "\n")
	if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://open.bigmodel.cn/api/paas/v4" || config.APIKey != "placeholder-key" || config.Model != "glm-4.5-air" {
		t.Fatalf("unexpected config: %+v", config)
	}
	if config.Timeout != 120*time.Second {
		t.Fatalf("unexpected timeout: %s", config.Timeout)
	}
}

func TestConfigFromDotEnvFileSyntax(t *testing.T) {
	t.Chdir(t.TempDir())
	content := strings.Join([]string{
		"# comments and non-SafeOps keys are ignored",
		"UNRELATED=value",
		"export SAFEOPS_LLM_BASE_URL=\"https://quoted.example/v1\"",
		"SAFEOPS_LLM_API_KEY='quoted-key'",
		"SAFEOPS_LLM_MODEL=model-from-file # inline comment",
		"SAFEOPS_LLM_TIMEOUT_SECONDS=45",
		"",
	}, "\n")
	if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://quoted.example/v1" || config.APIKey != "quoted-key" || config.Model != "model-from-file" {
		t.Fatalf("unexpected parsed config: %+v", config)
	}
	if config.Timeout != 45*time.Second {
		t.Fatalf("unexpected timeout: %s", config.Timeout)
	}
}

func TestEnvironmentOverridesDotEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".env", []byte("SAFEOPS_LLM_BASE_URL=https://file.example/v1\nSAFEOPS_LLM_API_KEY=file-key\nSAFEOPS_LLM_MODEL=file-model\nSAFEOPS_LLM_TIMEOUT_SECONDS=120\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SAFEOPS_LLM_BASE_URL", "https://env.example/v1")
	t.Setenv("SAFEOPS_LLM_API_KEY", "env-key")
	t.Setenv("SAFEOPS_LLM_MODEL", "env-model")
	t.Setenv("SAFEOPS_LLM_TIMEOUT_SECONDS", "90")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.BaseURL != "https://env.example/v1" || config.APIKey != "env-key" || config.Model != "env-model" {
		t.Fatalf("environment did not override .env: %+v", config)
	}
	if config.Timeout != 90*time.Second {
		t.Fatalf("unexpected timeout: %s", config.Timeout)
	}
}

func TestConfigFromEnvRejectsInvalidDotEnv(t *testing.T) {
	for name, content := range map[string]string{
		"bad line":       "SAFEOPS_LLM_BASE_URL\n",
		"unclosed quote": "SAFEOPS_LLM_BASE_URL=\"https://example.invalid/v1\nSAFEOPS_LLM_API_KEY=key\nSAFEOPS_LLM_MODEL=model\n",
		"timeout zero":   "SAFEOPS_LLM_BASE_URL=https://example.invalid/v1\nSAFEOPS_LLM_API_KEY=key\nSAFEOPS_LLM_MODEL=model\nSAFEOPS_LLM_TIMEOUT_SECONDS=0\n",
		"timeout high":   "SAFEOPS_LLM_BASE_URL=https://example.invalid/v1\nSAFEOPS_LLM_API_KEY=key\nSAFEOPS_LLM_MODEL=model\nSAFEOPS_LLM_TIMEOUT_SECONDS=601\n",
		"timeout text":   "SAFEOPS_LLM_BASE_URL=https://example.invalid/v1\nSAFEOPS_LLM_API_KEY=key\nSAFEOPS_LLM_MODEL=model\nSAFEOPS_LLM_TIMEOUT_SECONDS=slow\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("invalid .env was accepted")
			}
		})
	}
}
