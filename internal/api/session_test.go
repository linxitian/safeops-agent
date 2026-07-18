package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
)

func TestSessionSearchRenameArchiveAndRestore(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	value := session.Session{ID: "ses_manage", Name: "端口调查", Summary: "服务恢复", Messages: []session.Message{{ID: "msg", Role: session.RoleUser, Content: "检查 18081", CreatedAt: now}}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, value); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)

	rename := requestJSON(t, server.Handler(), http.MethodPatch, "/api/v1/sessions/ses_manage", map[string]any{"name": "Web 端口恢复"})
	if rename.Code != http.StatusOK {
		t.Fatalf("rename returned %d %s", rename.Code, rename.Body.String())
	}
	var renamed session.Session
	if err := json.Unmarshal(rename.Body.Bytes(), &renamed); err != nil || renamed.Name != "Web 端口恢复" {
		t.Fatalf("rename response: %+v %v", renamed, err)
	}

	search := httptest.NewRecorder()
	server.Handler().ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?q=18081", nil))
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte("ses_manage")) {
		t.Fatalf("message search returned %d %s", search.Code, search.Body.String())
	}

	activeTask := task.Task{ID: "task_active", SessionID: value.ID, State: task.WaitingApproval, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, activeTask); err != nil {
		t.Fatal(err)
	}
	blocked := requestJSON(t, server.Handler(), http.MethodPatch, "/api/v1/sessions/ses_manage", map[string]any{"archived": true})
	if blocked.Code != http.StatusConflict {
		t.Fatalf("active session archive returned %d %s", blocked.Code, blocked.Body.String())
	}
	activeTask.State = task.Completed
	if err := store.SaveTask(ctx, activeTask); err != nil {
		t.Fatal(err)
	}
	archived := requestJSON(t, server.Handler(), http.MethodPatch, "/api/v1/sessions/ses_manage", map[string]any{"archived": true})
	if archived.Code != http.StatusOK {
		t.Fatalf("archive returned %d %s", archived.Code, archived.Body.String())
	}

	activeList := httptest.NewRecorder()
	server.Handler().ServeHTTP(activeList, httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil))
	if activeList.Code != http.StatusOK || bytes.Contains(activeList.Body.Bytes(), []byte("ses_manage")) {
		t.Fatalf("default list exposed archive: %d %s", activeList.Code, activeList.Body.String())
	}
	archiveList := httptest.NewRecorder()
	server.Handler().ServeHTTP(archiveList, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?archived=true", nil))
	if archiveList.Code != http.StatusOK || !bytes.Contains(archiveList.Body.Bytes(), []byte("ses_manage")) {
		t.Fatalf("archive list missing session: %d %s", archiveList.Code, archiveList.Body.String())
	}

	message := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions/ses_manage/messages", map[string]any{"content": "继续"})
	if message.Code != http.StatusConflict {
		t.Fatalf("archived session accepted message: %d %s", message.Code, message.Body.String())
	}
	restored := requestJSON(t, server.Handler(), http.MethodPatch, "/api/v1/sessions/ses_manage", map[string]any{"archived": false})
	if restored.Code != http.StatusOK {
		t.Fatalf("restore returned %d %s", restored.Code, restored.Body.String())
	}
}

func TestOverviewAndTaskListUseDurableState(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_overview", Name: "overview", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, task.Task{ID: "task_overview", SessionID: "ses_overview", State: task.Completed, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	overview := httptest.NewRecorder()
	server.Handler().ServeHTTP(overview, httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil))
	if overview.Code != http.StatusOK || !bytes.Contains(overview.Body.Bytes(), []byte(`"COMPLETED":1`)) || !bytes.Contains(overview.Body.Bytes(), []byte(`"active":1`)) {
		t.Fatalf("overview returned %d %s", overview.Code, overview.Body.String())
	}
	tasks := httptest.NewRecorder()
	server.Handler().ServeHTTP(tasks, httptest.NewRequest(http.MethodGet, "/api/v1/tasks?session_id=ses_overview&state=completed&limit=1", nil))
	if tasks.Code != http.StatusOK || !bytes.Contains(tasks.Body.Bytes(), []byte("task_overview")) {
		t.Fatalf("task list returned %d %s", tasks.Code, tasks.Body.String())
	}
}

func TestMessageAPIRejectsSessionWithActiveTask(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_busy", Name: "busy", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, task.Task{ID: "task_waiting", SessionID: "ses_busy", State: task.WaitingApproval, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), &agent.Orchestrator{}, nil)
	response := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions/ses_busy/messages", map[string]any{"content": "再执行一个任务"})
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte("task_waiting")) {
		t.Fatalf("active session task returned %d %s", response.Code, response.Body.String())
	}
	value, err := store.GetSession(ctx, "ses_busy")
	if err != nil || len(value.Messages) != 0 {
		t.Fatalf("rejected message changed durable session: %+v %v", value, err)
	}
}

func TestSessionArchiveRejectsAdmittedRun(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_running", Name: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	server.runningSessions["ses_running"] = struct{}{}
	response := requestJSON(t, server.Handler(), http.MethodPatch, "/api/v1/sessions/ses_running", map[string]any{"archived": true})
	if response.Code != http.StatusConflict {
		t.Fatalf("running session archive returned %d %s", response.Code, response.Body.String())
	}
	value, err := store.GetSession(ctx, "ses_running")
	if err != nil || value.Archived {
		t.Fatalf("rejected archive changed durable session: %+v %v", value, err)
	}
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
