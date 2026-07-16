package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
)

func TestFileStorePersistsAcrossInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s := session.Session{ID: "ses_1", Name: "测试会话", Messages: []session.Message{{ID: "msg_1", Role: session.RoleUser, Content: "查看 CPU", CreatedAt: now}}, CreatedAt: now, UpdatedAt: now}
	taskValue := task.Task{ID: "task_1", SessionID: s.ID, Objective: "查看 CPU", OriginalRequest: "查看 CPU", State: task.Investigating, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTask(ctx, taskValue); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	gotSession, err := reopened.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSession.Messages[0].Content != "查看 CPU" {
		t.Fatalf("unexpected session: %+v", gotSession)
	}
	gotTask, err := reopened.GetTask(ctx, taskValue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotTask.State != task.Investigating {
		t.Fatalf("unexpected task: %+v", gotTask)
	}
	if _, err := reopened.GetTask(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestAtomicSessionMutationAcrossStoreInstances(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := first.SaveSession(ctx, session.Session{ID: "ses_concurrent", Name: "concurrent", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	const updates = 40
	var group sync.WaitGroup
	for index := 0; index < updates; index++ {
		group.Add(1)
		go func(value int) {
			defer group.Done()
			store := first
			if value%2 == 1 {
				store = second
			}
			_, updateErr := store.UpdateSession(ctx, "ses_concurrent", func(current *session.Session) error {
				current.Messages = append(current.Messages, session.Message{ID: fmt.Sprintf("msg_%02d", value), Role: session.RoleUser, Content: "message", CreatedAt: now})
				current.UpdatedAt = now.Add(time.Duration(value) * time.Millisecond)
				return nil
			})
			if updateErr != nil {
				t.Errorf("update %d: %v", value, updateErr)
			}
		}(index)
	}
	group.Wait()
	value, err := first.GetSession(ctx, "ses_concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Messages) != updates {
		t.Fatalf("atomic mutations retained %d/%d messages", len(value.Messages), updates)
	}
}

func TestTaskLeaseExpiryTakeoverAndFencing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	first.now = func() time.Time { return now }
	second.now = func() time.Time { return now }
	value := task.Task{ID: "task_fenced", SessionID: "session", State: task.New, CreatedAt: now, UpdatedAt: now}
	if err := first.SaveTask(ctx, value); err != nil {
		t.Fatal(err)
	}
	claimed, err := first.ClaimTask(ctx, value.ID, "worker-a", "token-a", time.Minute)
	if err != nil || claimed.WorkerLease.Fence != 1 {
		t.Fatalf("first claim: %+v %v", claimed.WorkerLease, err)
	}
	if _, err := second.ClaimTask(ctx, value.ID, "worker-b", "token-b", time.Minute); !errors.Is(err, ErrTaskLeased) {
		t.Fatalf("live lease was not exclusive: %v", err)
	}
	stale := claimed
	now = now.Add(2 * time.Minute)
	taken, err := second.ClaimTask(ctx, value.ID, "worker-b", "token-b", time.Minute)
	if err != nil || taken.WorkerLease.Fence != 2 {
		t.Fatalf("expired lease takeover: %+v %v", taken.WorkerLease, err)
	}
	stale.State = task.Completed
	if err := first.SaveTask(ctx, stale); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("stale worker crossed fence: %v", err)
	}
	taken.State = task.Completed
	if err := second.SaveTask(ctx, taken); err != nil {
		t.Fatalf("current worker save: %v", err)
	}
	if _, err := second.ReleaseTask(ctx, taken.ID, "token-b", 1); !errors.Is(err, ErrLeaseConflict) {
		t.Fatalf("wrong fence released task: %v", err)
	}
	released, err := second.ReleaseTask(ctx, taken.ID, "token-b", 2)
	if err != nil || released.WorkerLease.Token != "" || released.WorkerLease.Fence != 2 {
		t.Fatalf("release did not preserve fence: %+v %v", released.WorkerLease, err)
	}
}

func TestFileStoreRejectsPathTraversal(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), session.Session{ID: "../outside"}); err == nil {
		t.Fatal("path traversal id accepted")
	}
}
