package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
)

func TestRuntimeLogWritesMetadataWithoutBody(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	server := New(store, registry.New(registry.Config{}), nil, nil, WithRuntimeLog(&log))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"name":"secret-body-token"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create session returned %d %s", response.Code, response.Body.String())
	}
	line := strings.TrimSpace(log.String())
	if line == "" {
		t.Fatal("runtime log was not written")
	}
	if strings.Contains(line, "secret-body-token") {
		t.Fatalf("runtime log leaked request body: %s", line)
	}
	var entry runtimeLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("runtime log is not JSON: %v %q", err, line)
	}
	if entry.Method != http.MethodPost || entry.Path != "/api/v1/sessions" || entry.Status != http.StatusCreated || entry.Bytes == 0 {
		t.Fatalf("unexpected runtime log entry: %+v", entry)
	}
}

func TestRuntimeLogPreservesStreamingFlusher(t *testing.T) {
	var log bytes.Buffer
	wrapped := (&runtimeLog{writer: &log}).handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
		flusher.Flush()
	}))
	response := httptest.NewRecorder()
	wrapped.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task/events", nil))

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "event: ping") {
		t.Fatalf("streaming response failed: %d %q", response.Code, response.Body.String())
	}
}

func TestRuntimeLogKeepsFirstResponseStatus(t *testing.T) {
	var log bytes.Buffer
	wrapped := (&runtimeLog{writer: &log}).handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	response := httptest.NewRecorder()
	wrapped.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))

	var entry runtimeLogEntry
	if err := json.Unmarshal(bytes.TrimSpace(log.Bytes()), &entry); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || entry.Status != http.StatusCreated {
		t.Fatalf("first status was not preserved: response=%d log=%d", response.Code, entry.Status)
	}
}
