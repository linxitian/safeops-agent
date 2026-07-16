package task

import (
	"testing"
	"time"
)

func TestTransitionUpdatesStateCheckpointAndTimestamp(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := Task{ID: "task_transition", State: New, Checkpoint: 7, UpdatedAt: old}
	before := time.Now().UTC().Add(-time.Second)
	value.Transition(Verifying)
	if value.State != Verifying {
		t.Fatalf("state = %s, want %s", value.State, Verifying)
	}
	if value.Checkpoint != 8 {
		t.Fatalf("checkpoint = %d, want 8", value.Checkpoint)
	}
	if !value.UpdatedAt.After(before) || value.UpdatedAt.Equal(old) {
		t.Fatalf("updated_at was not refreshed: %s", value.UpdatedAt)
	}
	firstUpdate := value.UpdatedAt
	value.Transition(Completed)
	if value.State != Completed || value.Checkpoint != 9 {
		t.Fatalf("second transition failed: state=%s checkpoint=%d", value.State, value.Checkpoint)
	}
	if value.UpdatedAt.Before(firstUpdate) {
		t.Fatalf("updated_at moved backwards: first=%s second=%s", firstUpdate, value.UpdatedAt)
	}
}
