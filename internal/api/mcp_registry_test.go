package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/internal/registry"
)

func TestMCPServerAPIProjectsDependencyAndBoundedHistoryState(t *testing.T) {
	root := t.TempDir()
	reg := registry.New(registry.Config{Servers: []registry.ServerManifest{{
		ID:           "failed",
		Name:         "failed",
		Version:      "manifest-only",
		Transport:    "stdio",
		Command:      filepath.Join(root, "missing-mcp-server"),
		Enabled:      true,
		Dependencies: []string{root},
	}}})
	defer reg.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := reg.Start(ctx); err == nil {
		t.Fatal("missing MCP command unexpectedly initialized")
	}

	server := New(nil, reg, nil, nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("registry API returned %d: %s", response.Code, response.Body.String())
	}
	var payload struct {
		Servers []registry.ServerState `json:"servers"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Servers) != 1 {
		t.Fatalf("registry API returned %+v", payload.Servers)
	}
	state := payload.Servers[0]
	if state.Status != registry.StatusUnhealthy || !state.DependenciesHealthy || len(state.DependencyChecks) != 1 || state.DependencyChecks[0].Mode == "" {
		t.Fatalf("dependency projection missing from API: %+v", state)
	}
	if len(state.HealthHistory) != 1 || len(state.DiscoveryHistory) != 1 || state.DiscoveryHistory[0].Status != registry.StatusUnhealthy || state.DiscoveryHistory[0].Error == "" {
		t.Fatalf("bounded failure history missing from API: %+v", state)
	}
}
