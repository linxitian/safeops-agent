package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"safeops-agent/internal/registry"
)

func TestWebRootServesAssetsAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>SafeOps</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, registry.New(registry.Config{}), nil, nil, WithWebRoot(root))

	for _, requestPath := range []string{"/", "/sessions/ses-demo"} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "SafeOps") {
			t.Fatalf("SPA request %s returned %d %q", requestPath, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("SPA request %s missing no-cache", requestPath)
		}
		for header, want := range map[string]string{
			"Content-Security-Policy": "default-src 'self'",
			"Permissions-Policy":      "camera=()",
			"Referrer-Policy":         "no-referrer",
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
		} {
			if got := response.Header().Get(header); !strings.Contains(got, want) {
				t.Fatalf("SPA request %s header %s = %q, want it to contain %q", requestPath, header, got, want)
			}
		}
	}

	asset := httptest.NewRecorder()
	server.Handler().ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "export {}" {
		t.Fatalf("asset returned %d %q", asset.Code, asset.Body.String())
	}

	missing := httptest.NewRecorder()
	server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset returned %d", missing.Code)
	}

	unknownAPI := httptest.NewRecorder()
	server.Handler().ServeHTTP(unknownAPI, httptest.NewRequest(http.MethodGet, "/api/v1/missing", nil))
	if unknownAPI.Code != http.StatusNotFound || !strings.Contains(unknownAPI.Body.String(), "API route not found") {
		t.Fatalf("unknown API returned %d %q", unknownAPI.Code, unknownAPI.Body.String())
	}
}
