package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/trace"
)

func TestPrepareNamesDefaultSessionFromFirstRequest(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(filepath.Join(root, "traces"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC)
	value := session.Session{ID: "ses_default_title", Name: "新会话", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	orchestrator := Orchestrator{Store: store, Trace: traceWriter}
	if _, err := orchestrator.Prepare(context.Background(), "task_title", value.ID, "帮我查看 /var/lib/safeops/lab/1.txt 文件状态"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetSession(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "帮我查看 /var/lib/safeops/lab/1.txt 文件状态" {
		t.Fatalf("default session name was not updated from request: %q", updated.Name)
	}
}

func TestPrepareDoesNotOverwriteCustomSessionName(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewFileStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(filepath.Join(root, "traces"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC)
	value := session.Session{ID: "ses_custom_title", Name: "生产巡检", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	orchestrator := Orchestrator{Store: store, Trace: traceWriter}
	if _, err := orchestrator.Prepare(context.Background(), "task_custom_title", value.ID, "帮我查看 CPU"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetSession(context.Background(), value.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "生产巡检" {
		t.Fatalf("custom session name was overwritten: %q", updated.Name)
	}
}
