package api

import (
	"testing"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/task"
)

func TestEventHubSequencesReplayAndReportsGaps(t *testing.T) {
	hub := newEventHub()
	for index := 0; index < 205; index++ {
		hub.publish(agent.RuntimeEvent{TaskID: "task", State: task.Executing, Message: "event"})
	}
	window, _, unsubscribe := hub.subscribe("task", 200)
	if window.Gap || len(window.Events) != 5 || window.Events[0].Sequence != 201 || window.Events[4].Sequence != 205 {
		t.Fatalf("recent replay window: %+v", window)
	}
	unsubscribe()
	window, _, unsubscribe = hub.subscribe("task", 1)
	if !window.Gap || len(window.Events) != 200 || window.Oldest != 6 || window.Latest != 205 {
		t.Fatalf("truncated replay window: oldest=%d latest=%d gap=%t events=%d", window.Oldest, window.Latest, window.Gap, len(window.Events))
	}
	unsubscribe()
	window, _, unsubscribe = hub.subscribe("task", 999)
	if !window.Gap || len(window.Events) != 0 {
		t.Fatalf("future replay id did not report a gap: %+v", window)
	}
	unsubscribe()
}
