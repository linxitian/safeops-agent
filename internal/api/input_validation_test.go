package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
)

func TestSessionAPIRejectsOversizedNamesAndSearches(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	tooLong := strings.Repeat("界", 129)
	create := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions", map[string]any{"name": tooLong})
	if create.Code != http.StatusBadRequest {
		t.Fatalf("oversized session name returned %d: %s", create.Code, create.Body.String())
	}
	search := httptest.NewRecorder()
	server.Handler().ServeHTTP(search, httptest.NewRequest(http.MethodGet, "/api/v1/sessions?q="+strings.Repeat("x", 129), nil))
	if search.Code != http.StatusBadRequest {
		t.Fatalf("oversized search returned %d: %s", search.Code, search.Body.String())
	}
}

func TestSessionAndTaskAPIRejectInvalidQueryValues(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	for _, path := range []string{
		"/api/v1/sessions?archived=maybe",
		"/api/v1/tasks?limit=0",
		"/api/v1/tasks?limit=501",
		"/api/v1/tasks?limit=not-a-number",
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
}

func TestDecodeJSONRejectsUnknownFieldsAndTrailingObjects(t *testing.T) {
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil)
	for _, body := range []string{
		`{"name":"ok","extra":true}`,
		`{"name":"ok"} {"name":"second"}`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body %q returned %d: %s", body, response.Code, response.Body.String())
		}
	}
}
