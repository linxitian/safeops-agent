package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
)

func TestSSEReplaysAfterLastEventIDAndEmitsRestartSnapshot(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	value := task.Task{ID: "task_sse", SessionID: "session", State: task.Completed, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	server.hub.publish(agent.RuntimeEvent{TaskID: value.ID, State: task.Executing, Message: "one", Timestamp: now})
	server.hub.publish(agent.RuntimeEvent{TaskID: value.ID, State: task.Verifying, Message: "two", Timestamp: now})
	server.hub.publish(agent.RuntimeEvent{TaskID: value.ID, State: task.Completed, Message: "three", Timestamp: now})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_sse/events", nil)
	request.Header.Set("Last-Event-ID", "1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || strings.Contains(body, "id: 1\n") || !strings.Contains(body, "id: 2\n") || !strings.Contains(body, "id: 3\n") || strings.Count(body, "event: task.progress") != 2 {
		t.Fatalf("replay response %d: %s", response.Code, body)
	}

	restarted := New(store, registry.New(registry.Config{}), nil, nil)
	restartRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_sse/events", nil)
	restartRequest.Header.Set("Last-Event-ID", "3")
	restartResponse := httptest.NewRecorder()
	restarted.Handler().ServeHTTP(restartResponse, restartRequest)
	restartBody := restartResponse.Body.String()
	if !strings.Contains(restartBody, "event: task.gap") || !strings.Contains(restartBody, "event: task.snapshot") || !strings.Contains(restartBody, "持久化状态恢复") {
		t.Fatalf("restart snapshot response: %s", restartBody)
	}
}
