package trace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHashChainIntegrityAndTamperDetection(t *testing.T) {
	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := w.Append(ctx, "task_1", "ses_1", Received, map[string]any{"request": "查看 CPU"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.Append(ctx, "task_1", "ses_1", ToolCall, map[string]any{"tool": "system.get_cpu_metrics"})
	if err != nil {
		t.Fatal(err)
	}
	if second.PrevHash != first.EventHash {
		t.Fatalf("chain is not linked: %+v %+v", first, second)
	}
	if err := w.VerifyIntegrity("task_1"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(w.tracePath("task_1"))
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 1
	if err := os.WriteFile(w.tracePath("task_1"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.VerifyIntegrity("task_1"); err == nil {
		t.Fatal("modified trace was accepted")
	}
}

func TestHashChainDetectsLastEventDeletion(t *testing.T) {
	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := w.Append(ctx, "task_2", "ses_2", Received, map[string]string{"request": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(ctx, "task_2", "ses_2", Final, map[string]string{"answer": "y"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(w.tracePath("task_2"))
	if err != nil {
		t.Fatal(err)
	}
	last := len(b) - 2
	for last >= 0 && b[last] != '\n' {
		last--
	}
	if last < 0 {
		t.Fatal("expected two events")
	}
	if err := os.WriteFile(w.tracePath("task_2"), b[:last+1], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.VerifyIntegrity("task_2"); err == nil {
		t.Fatal("deleted final event was accepted")
	}
}

func TestRequiredEventCategoriesUseExactNames(t *testing.T) {
	required := []Type{
		Received, IntentParsed, TaskCreated, TaskResumed, PlanCreated,
		DecisionRecorded, ToolCall, ToolResult, FindingsUpdated,
		KnowledgeRetrieved, RCAResult, ActionProposed, StaticGuardResult,
		IntentGuardResult, RiskEvaluated, ApprovalRequested, ApprovalResult,
		Execution, Verification, Rollback, TaskCompleted, TaskFailed, Final,
	}
	wanted := []string{
		"RECEIVED", "INTENT_PARSED", "TASK_CREATED", "TASK_RESUMED", "PLAN_CREATED",
		"DECISION_RECORDED", "TOOL_CALL", "TOOL_RESULT", "FINDINGS_UPDATED",
		"KNOWLEDGE_RETRIEVED", "RCA_RESULT", "ACTION_PROPOSED", "STATIC_GUARD_RESULT",
		"INTENT_GUARD_RESULT", "RISK_EVALUATED", "APPROVAL_REQUESTED", "APPROVAL_RESULT",
		"EXECUTION", "VERIFICATION", "ROLLBACK", "TASK_COMPLETED", "TASK_FAILED", "FINAL",
	}
	for index := range wanted {
		if string(required[index]) != wanted[index] {
			t.Fatalf("required event %d = %q, want %q", index, required[index], wanted[index])
		}
	}
}

func TestDecisionRecordIsCompleteAndSecretsAreRedacted(t *testing.T) {
	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = w.Append(context.Background(), "task_decision", "session", DecisionRecorded, map[string]any{
		"objective":        "检查服务",
		"selected_tool":    "service.get_status",
		"arguments":        map[string]any{"unit": "safeops-demo-web.service"},
		"api_key":          "sk-raw-secret-value",
		"chain_of_thought": "hidden private reasoning",
		"note":             "Authorization: Bearer raw-token api_key=raw-value",
	})
	if err != nil {
		t.Fatal(err)
	}
	events, err := w.Read("task_decision")
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(events[0].Data, &record); err != nil {
		t.Fatal(err)
	}
	required := []string{"decision_id", "task_id", "plan_version", "objective", "current_step", "decision_summary", "candidate_hypotheses", "selected_hypothesis", "rejected_hypotheses", "evidence_used", "evidence_missing", "selected_tool", "tool_arguments_digest", "expected_observation", "completion_assessment", "replan_reason"}
	for _, key := range required {
		if _, exists := record[key]; !exists {
			t.Fatalf("normalized decision is missing %s: %s", key, events[0].Data)
		}
	}
	if record["api_key"] != "[REDACTED]" || record["chain_of_thought"] != "[REDACTED]" {
		t.Fatalf("sensitive decision fields were not redacted: %s", events[0].Data)
	}
	if _, exists := record["arguments"]; exists || len(record["tool_arguments_digest"].(string)) != 64 {
		t.Fatalf("tool arguments were not replaced by a digest: %s", events[0].Data)
	}
	encoded := string(events[0].Data)
	for _, forbidden := range []string{"raw-secret-value", "hidden private reasoning", "raw-token", "raw-value"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("trace leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestConcurrentAppendProducesOneOrderedChain(t *testing.T) {
	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const total = 48
	var group sync.WaitGroup
	for index := 0; index < total; index++ {
		group.Add(1)
		go func(value int) {
			defer group.Done()
			if _, err := w.Append(context.Background(), "task_concurrent", "session", FindingsUpdated, map[string]any{"value": value}); err != nil {
				t.Errorf("append %d: %v", value, err)
			}
		}(index)
	}
	group.Wait()
	if err := w.VerifyIntegrity("task_concurrent"); err != nil {
		t.Fatal(err)
	}
	events, err := w.Read("task_concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != total || events[total-1].Sequence != total {
		t.Fatalf("concurrent chain has %d events, last sequence %d", len(events), events[len(events)-1].Sequence)
	}
}

func TestNewWriterRecoversAnchoredEventAndPartialCrashTail(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := w.Append(context.Background(), "task_crash", "session", Received, map[string]any{"request": "x"})
	if err != nil {
		t.Fatal(err)
	}
	appendEventWithoutHead(t, w, first, ToolCall, map[string]any{"tool": "system.get_cpu_metrics"})
	f, err := os.OpenFile(w.tracePath("task_crash"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"partial":`); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.VerifyIntegrity("task_crash"); err != nil {
		t.Fatal(err)
	}
	events, err := reopened.Read("task_crash")
	if err != nil || len(events) != 2 {
		t.Fatalf("recovered events=%d error=%v", len(events), err)
	}
	b, err := os.ReadFile(reopened.tracePath("task_crash"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "partial") || b[len(b)-1] != '\n' {
		t.Fatalf("partial crash tail remains: %q", b)
	}
}

func TestNewWriterRejectsTamperedCommittedPrefix(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Append(context.Background(), "task_reject", "session", Received, map[string]any{"request": "original"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(w.tracePath("task_reject"))
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), "original", "tampered", 1))
	if err := os.WriteFile(w.tracePath("task_reject"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWriter(dir); err == nil {
		t.Fatal("writer recovered a tampered committed prefix")
	}
}

func appendEventWithoutHead(t *testing.T, w *Writer, previous Event, typ Type, data any) {
	t.Helper()
	payload, err := marshalPayload(previous.TaskID, previous.Sequence+1, typ, data)
	if err != nil {
		t.Fatal(err)
	}
	canonical := canonicalEvent{Sequence: previous.Sequence + 1, Timestamp: time.Now().UTC(), Type: typ, TaskID: previous.TaskID, SessionID: previous.SessionID, Data: payload}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append([]byte(previous.EventHash), encoded...))
	event := Event{Sequence: canonical.Sequence, Timestamp: canonical.Timestamp, Type: canonical.Type, TaskID: canonical.TaskID, SessionID: canonical.SessionID, Data: payload, PrevHash: previous.EventHash, EventHash: hex.EncodeToString(sum[:])}
	line, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(w.tracePath(previous.TaskID), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
