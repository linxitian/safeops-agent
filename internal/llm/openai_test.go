package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestDecodeStructuredDecisionAcceptsAnswerAlias(t *testing.T) {
	decision, err := decodeStructuredDecision(`{"kind":"final","decision_summary":"基于证据完成","answer":"已引用 trace://task/1 的 MCP 证据。"}`)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionFinal || decision.FinalAnswer == "" {
		t.Fatalf("answer alias was not normalized: %+v", decision)
	}
	if err := validateDecision(decision); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeStructuredDecisionRejectsAmbiguousAnswerAlias(t *testing.T) {
	_, err := decodeStructuredDecision(`{"kind":"final","decision_summary":"x","answer":"a","final_answer":"b"}`)
	if err == nil {
		t.Fatal("conflicting answer aliases were accepted")
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
