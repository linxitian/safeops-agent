package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionJSONUsesStablePublicFieldNames(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	value := Session{
		ID:   "ses_json",
		Name: "系统巡检",
		Messages: []Message{{
			ID:        "msg_json",
			Role:      RoleAssistant,
			Content:   "完成",
			TaskID:    "task_json",
			CreatedAt: now,
		}},
		PinnedContext:     map[string]string{"service": "safeops-demo-web.service"},
		SelectedResources: []string{"file:/var/lib/safeops/lab/a.log"},
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"session_id", "name", "messages", "pinned_context", "selected_resources", "created_at", "updated_at"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("missing public JSON field %q in %s", key, b)
		}
	}
	if _, ok := body["ID"]; ok {
		t.Fatalf("internal Go field leaked into JSON: %s", b)
	}
	messages := body["messages"].([]any)
	message := messages[0].(map[string]any)
	if message["message_id"] != "msg_json" || message["role"] != string(RoleAssistant) || message["task_id"] != "task_json" {
		t.Fatalf("unexpected message JSON: %+v", message)
	}
}
