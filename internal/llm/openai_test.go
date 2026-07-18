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
		messages := request["messages"].([]any)
		userPrompt := messages[1].(map[string]any)["content"].(string)
		var decisionRequest DecisionRequest
		if err := json.Unmarshal([]byte(userPrompt), &decisionRequest); err != nil {
			t.Fatal(err)
		}
		if decisionRequest.SessionContext == nil || len(decisionRequest.SessionContext.SelectedResources) != 1 || decisionRequest.SessionContext.RecentMessages[0].Content != "之前的问题" {
			t.Fatalf("session context missing from provider prompt: %+v", decisionRequest.SessionContext)
		}
		content := `{"kind":"tool","decision_summary":"读取负载确认现状","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observation":"结构化负载"}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
	defer server.Close()
	provider, err := NewOpenAICompatible(Config{BaseURL: server.URL + "/v1", APIKey: "test-secret", Model: "test-model", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := provider.Decide(context.Background(), DecisionRequest{Objective: "查看负载", OriginalRequest: "查看负载", SessionContext: &SessionContext{RecentMessages: []SessionMessage{{Role: "user", Content: "之前的问题"}}, SelectedResources: []string{"/var/lib/safeops/lab/demo.log"}}, Tools: []ToolCapability{{ServerID: "system", Name: "system.get_load_average", InputSchema: json.RawMessage(`{"type":"object"}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionTool || decision.Tool != "system.get_load_average" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestOpenAICompatibleRepairsMalformedDecisionOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Messages []chatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		content := `{"kind":"tool","decision_summary":"读取磁盘占用","server_id":"system","tool":"system.get_disk_usage","arguments":{"path":"/var/log"},"expected_observaton":"目录占用"}`
		if requests == 2 {
			if len(request.Messages) != 3 || request.Messages[2].Role != "user" || !strings.Contains(request.Messages[2].Content, `unknown field "expected_observaton"`) {
				t.Fatalf("repair feedback missing or unsafe: %+v", request.Messages)
			}
			content = `{"kind":"tool","decision_summary":"读取磁盘占用","server_id":"system","tool":"system.get_disk_usage","arguments":{"path":"/var/log"},"expected_observation":"目录占用"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
	defer server.Close()
	provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
	decision, err := provider.Decide(context.Background(), DecisionRequest{Objective: "修复 /var/log 磁盘占用问题"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || decision.ExpectedObservation != "目录占用" {
		t.Fatalf("repair result = (%d, %+v), want one retry and corrected decision", requests, decision)
	}
}

func TestOpenAICompatibleRepairsUncitedFinalOnce(t *testing.T) {
	requests := 0
	reference := "trace://task_target/11"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request struct {
			Messages []chatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		content := `{"kind":"final","decision_summary":"基于证据总结","final_answer":"磁盘状态正常。"}`
		if requests == 2 {
			if len(request.Messages) != 3 || !strings.Contains(request.Messages[2].Content, "must cite at least one provided evidence_ref exactly") {
				t.Fatalf("citation repair feedback missing: %+v", request.Messages)
			}
			content = `{"kind":"final","decision_summary":"基于证据总结","final_answer":"磁盘状态正常，证据：trace://task_target/11。"}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
	defer server.Close()
	provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
	decision, err := provider.Decide(context.Background(), DecisionRequest{Objective: "检查磁盘", Observations: []Observation{{Tool: "system.get_disk_usage", EvidenceRef: reference}}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !strings.Contains(decision.FinalAnswer, reference) {
		t.Fatalf("citation repair result = (%d, %+v), want one retry with exact reference", requests, decision)
	}
}

func TestValidateDecisionForRequestRequiresExactEvidenceReference(t *testing.T) {
	input := DecisionRequest{Observations: []Observation{{EvidenceRef: "trace://task/11"}}}
	uncited := Decision{Kind: DecisionFinal, FinalAnswer: "已经检查完成。"}
	if err := validateDecisionForRequest(uncited, input); err == nil {
		t.Fatal("uncited final answer was accepted")
	}
	cited := Decision{Kind: DecisionFinal, FinalAnswer: "已经检查完成（trace://task/11）。"}
	if err := validateDecisionForRequest(cited, input); err != nil {
		t.Fatal(err)
	}
	if err := validateDecisionForRequest(Decision{Kind: DecisionTool}, input); err != nil {
		t.Fatalf("non-final decision was incorrectly subjected to citation validation: %v", err)
	}
}

func TestOpenAICompatibleStopsAfterOneFailedRepair(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		content := `{"kind":"tool","decision_summary":"x","server_id":"system","tool":"system.get_disk_usage","arguments":{},"unexpected":true}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
	}))
	defer server.Close()
	provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
	_, err := provider.Decide(context.Background(), DecisionRequest{Objective: "test"})
	if err == nil || requests != 2 || !strings.Contains(err.Error(), "after one repair attempt") {
		t.Fatalf("failed repair result = (%d, %v), want two requests and final rejection", requests, err)
	}
}

func TestOpenAICompatibleDoesNotRepairProviderFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	provider, _ := NewOpenAICompatible(Config{BaseURL: server.URL, APIKey: "key", Model: "model", Client: server.Client()})
	_, err := provider.Decide(context.Background(), DecisionRequest{Objective: "test"})
	if err == nil || requests != 1 {
		t.Fatalf("provider failure result = (%d, %v), want no repair retry", requests, err)
	}
}

func TestDecisionSystemPromptConstrainsAmbiguousFollowupsToSelectedResources(t *testing.T) {
	if !strings.Contains(decisionSystemPrompt, "selected_resources is an ordered durable scope") || !strings.Contains(decisionSystemPrompt, "ambiguous follow-up") {
		t.Fatal("system prompt does not define durable follow-up scope")
	}
	if !strings.Contains(decisionSystemPrompt, "managed_action is not command execution") || !strings.Contains(decisionSystemPrompt, "direct shell, terminal, command, or bash execution") {
		t.Fatal("system prompt does not define managed action command guardrails")
	}
	if !strings.Contains(decisionSystemPrompt, "Simplified Chinese") || !strings.Contains(decisionSystemPrompt, "final_answer") {
		t.Fatal("system prompt does not require Chinese user-facing answers")
	}
	if !strings.Contains(decisionSystemPrompt, "local_read_scope is an authoritative local-policy boundary") || !strings.Contains(decisionSystemPrompt, "excluded_paths") || !strings.Contains(decisionSystemPrompt, "never authorizes expanding to another root") || !strings.Contains(decisionSystemPrompt, "guard_feedback") {
		t.Fatal("system prompt does not enforce local read scope or bounded guard feedback")
	}
	if !strings.Contains(decisionSystemPrompt, "If final_only is true") || !strings.Contains(decisionSystemPrompt, "must not return tool, action_request, or replan") {
		t.Fatal("system prompt does not enforce evidence-backed final-only mode")
	}
	if !strings.Contains(decisionSystemPrompt, "1 MiB = 1048576 bytes") || !strings.Contains(decisionSystemPrompt, "Never describe a millions-of-bytes file as gigabytes") || !strings.Contains(decisionSystemPrompt, "cite at least one provided evidence_ref exactly") {
		t.Fatal("system prompt does not constrain numeric evidence or exact citations")
	}
}

func TestOpenAICompatibleUsesExtendedDefaultTimeout(t *testing.T) {
	provider, err := NewOpenAICompatible(Config{BaseURL: "https://llm.example/v1", APIKey: "key", Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if provider.client.Timeout != defaultClientTimeout {
		t.Fatalf("default provider timeout = %s, want %s", provider.client.Timeout, defaultClientTimeout)
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

func TestDecodeStructuredDecisionAcceptsExpectedObservationsAlias(t *testing.T) {
	decision, err := decodeStructuredDecision(`{"kind":"tool","decision_summary":"读取负载确认现状","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observations":"结构化负载"}`)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ExpectedObservation != "结构化负载" {
		t.Fatalf("expected_observations alias was not normalized: %+v", decision)
	}
}

func TestDecodeStructuredDecisionAcceptsMatchingExpectedObservationAliases(t *testing.T) {
	decision, err := decodeStructuredDecision(`{"kind":"tool","decision_summary":"读取负载确认现状","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observation":"结构化负载","expected_observations":"结构化负载"}`)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ExpectedObservation != "结构化负载" {
		t.Fatalf("matching expected observation aliases were not normalized: %+v", decision)
	}
}

func TestDecodeStructuredDecisionRejectsInvalidExpectedObservationsAlias(t *testing.T) {
	for name, content := range map[string]string{
		"conflict": `{"kind":"tool","decision_summary":"x","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observation":"a","expected_observations":"b"}`,
		"array":    `{"kind":"tool","decision_summary":"x","server_id":"system","tool":"system.get_load_average","arguments":{},"expected_observations":["a"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeStructuredDecision(content); err == nil {
				t.Fatal("invalid expected_observations alias was accepted")
			}
		})
	}
}

func TestDecodeStructuredDecisionAcceptsManagedActionRequest(t *testing.T) {
	decision, err := decodeStructuredDecision(`{"kind":"action_request","decision_summary":"申请重启服务","tool":"service.restart","target":{"type":"service","id":"safeops-demo-web.service"},"arguments":{},"expected_observation":"服务恢复 active"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDecision(decision); err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionActionRequest || decision.ServerID != "" || decision.Target.ID != "safeops-demo-web.service" {
		t.Fatalf("managed action was not decoded: %+v", decision)
	}
}

func TestOpenAICompatibleRejectsMalformedOrUnsafeDecision(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":           `{"kind":"tool","decision_summary":"x","server_id":"x","tool":"x","arguments":{},"shell":"rm -rf /"}`,
		"unknown kind":            `{"kind":"shell","decision_summary":"x","final_answer":"x"}`,
		"mixed final":             `{"kind":"final","decision_summary":"x","tool":"system.get_overview","final_answer":"x"}`,
		"action with server":      `{"kind":"action_request","decision_summary":"x","server_id":"system","tool":"service.restart","target":{"type":"service","id":"safeops-demo-web.service"},"arguments":{}}`,
		"action missing target":   `{"kind":"action_request","decision_summary":"x","tool":"service.restart","arguments":{}}`,
		"action command field":    `{"kind":"action_request","decision_summary":"x","tool":"service.restart","target":{"type":"service","id":"safeops-demo-web.service"},"arguments":{},"command":"systemctl restart safeops-demo-web"}`,
		"tool with action target": `{"kind":"tool","decision_summary":"x","server_id":"system","tool":"system.get_overview","target":{"type":"host","id":"local"},"arguments":{}}`,
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
