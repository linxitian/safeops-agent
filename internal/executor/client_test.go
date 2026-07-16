package executor

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/contracts"
)

func TestUnixClientUsesOnlyUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "executor.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socket)
	}()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/execute" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeExecutorJSON(w, http.StatusOK, Result{Tool: "service.restart", Mode: DryRun, Status: "DRY_RUN_OK", StartedAt: time.Now(), FinishedAt: time.Now()})
	})}
	go server.Serve(listener)
	defer server.Close()
	client, err := NewUnixClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Execute(context.Background(), contracts.ActionEnvelope{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != DryRun {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestUnixClientRejectsRelativeSocket(t *testing.T) {
	if _, err := NewUnixClient("executor.sock"); err == nil {
		t.Fatal("relative socket accepted")
	}
}
