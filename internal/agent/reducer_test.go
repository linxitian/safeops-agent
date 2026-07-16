package agent

import (
	"encoding/json"
	"testing"
	"time"

	"safeops-agent/internal/task"
)

func TestRuntimeBudgetsSurvivePersistence(t *testing.T) {
	now := time.Now().UTC()
	checkpoint := task.RuntimeCheckpoint{}
	if err := beginDecision(&checkpoint, now); err != nil {
		t.Fatal(err)
	}
	if err := reserveToolCall(&checkpoint); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var reopened task.RuntimeCheckpoint
	if err := json.Unmarshal(b, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.Iterations != 1 || reopened.ToolCalls != 1 || reopened.MaxIterations != MaxIterations || reopened.DeadlineAt.IsZero() {
		t.Fatalf("runtime budget was not persisted: %+v", reopened)
	}
}

func TestRuntimeStopsNoProgressLoop(t *testing.T) {
	checkpoint := task.RuntimeCheckpoint{}
	observation := task.RuntimeObservation{ServerID: "system", Tool: "system.get_load_average", Arguments: json.RawMessage(`{}`), Result: json.RawMessage(`{"load":1}`)}
	if err := recordObservation(&checkpoint, observation); err != nil {
		t.Fatal(err)
	}
	if err := recordObservation(&checkpoint, observation); err != nil {
		t.Fatal(err)
	}
	if err := recordObservation(&checkpoint, observation); err == nil {
		t.Fatal("three identical observations were accepted")
	}
}

func TestRuntimeLimitsDecisionsAndReplans(t *testing.T) {
	now := time.Now().UTC()
	checkpoint := task.RuntimeCheckpoint{MaxIterations: 1, MaxToolCalls: 1, StartedAt: now, DeadlineAt: now.Add(time.Minute)}
	if err := beginDecision(&checkpoint, now); err != nil {
		t.Fatal(err)
	}
	if err := beginDecision(&checkpoint, now); err == nil {
		t.Fatal("iteration budget exceeded")
	}
	for i := 0; i < MaxReplans; i++ {
		if err := recordReplan(&checkpoint); err != nil {
			t.Fatal(err)
		}
	}
	if err := recordReplan(&checkpoint); err == nil {
		t.Fatal("replan budget exceeded")
	}
}
