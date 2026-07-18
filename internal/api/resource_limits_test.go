package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/agent"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestSessionRetentionLimitRejectsBeforeMutation(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSession(ctx, session.Session{ID: "ses_existing", Name: "existing", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	server := New(store, registry.New(registry.Config{}), nil, nil, WithLimits(Limits{MaxSessions: 1}))
	response := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions", map[string]any{"name": "overflow"})
	if response.Code != http.StatusInsufficientStorage || response.Header().Get("Retry-After") != "" {
		t.Fatalf("session limit returned %d %s", response.Code, response.Body.String())
	}
	values, err := store.ListSessions(ctx)
	if err != nil || len(values) != 1 {
		t.Fatalf("session limit mutated storage: %+v %v", values, err)
	}
}

func TestTaskRetentionAndConcurrencyLimitsRejectBeforePrepare(t *testing.T) {
	for _, test := range []struct {
		name     string
		addTask  bool
		fillSlot bool
	}{
		{name: "retention", addTask: true},
		{name: "concurrency", fillSlot: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := storage.NewFileStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			value := session.Session{ID: "ses_limited", Name: "limited", CreatedAt: now, UpdatedAt: now}
			if err := store.SaveSession(ctx, value); err != nil {
				t.Fatal(err)
			}
			if test.addTask {
				if err := store.SaveTask(ctx, task.Task{ID: "task_existing", SessionID: value.ID, State: task.Completed, CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			server := New(store, registry.New(registry.Config{}), &agent.Orchestrator{}, nil, WithLimits(Limits{MaxConcurrentTasks: 1, MaxTasks: 1}))
			if test.fillSlot {
				server.taskSlots <- struct{}{}
			}
			response := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions/"+value.ID+"/messages", map[string]any{"content": "inspect"})
			want := http.StatusInsufficientStorage
			if test.fillSlot {
				want = http.StatusTooManyRequests
			}
			if response.Code != want {
				t.Fatalf("task limit returned %d %s", response.Code, response.Body.String())
			}
			stored, err := store.GetSession(ctx, value.ID)
			if err != nil || len(stored.Messages) != 0 {
				t.Fatalf("task limit mutated session: %+v %v", stored, err)
			}
		})
	}
}

func TestApprovalRemainsPendingWhenRuntimeIsSaturated(t *testing.T) {
	ctx := context.Background()
	fileStore, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	approvalStore, err := approval.NewStore(fileStore.Root() + "/approvals")
	if err != nil {
		t.Fatal(err)
	}
	binding := approval.Binding{TaskID: "task_limited", ProposalDigest: "proposal", TargetSnapshotDigest: "target", IntentDigest: "intent", PolicyVersion: "policy", RiskLevel: contracts.L1, Tool: "file.quarantine", Nonce: "nonce"}
	record, err := approvalStore.Create(ctx, binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithApprovals(approvalStore, &fakeApprovalResumer{}), WithLimits(Limits{MaxConcurrentTasks: 1}))
	server.taskSlots <- struct{}{}
	response := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/approvals/"+record.ID+"/resolve", map[string]any{"decision": "APPROVE"})
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("saturated approval returned %d %s", response.Code, response.Body.String())
	}
	stored, err := approvalStore.Get(ctx, record.ID)
	if err != nil || stored.Status != approval.Pending {
		t.Fatalf("saturated approval was mutated: %+v %v", stored, err)
	}
}

func TestConcurrentAdmissionsCannotExceedTaskRetentionLimit(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sessions := []session.Session{
		{ID: "ses_race_1", Name: "race 1", CreatedAt: now, UpdatedAt: now},
		{ID: "ses_race_2", Name: "race 2", CreatedAt: now, UpdatedAt: now},
	}
	for _, value := range sessions {
		if err := store.SaveSession(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	orchestrator := &agent.Orchestrator{Store: store, Trace: traceWriter}
	server := New(store, registry.New(registry.Config{}), orchestrator, traceWriter, WithLimits(Limits{MaxConcurrentTasks: 2, MaxTasks: 1}))
	responses := make(chan int, 2)
	var group sync.WaitGroup
	for _, value := range sessions {
		group.Add(1)
		go func(sessionID string) {
			defer group.Done()
			response := requestJSON(t, server.Handler(), http.MethodPost, "/api/v1/sessions/"+sessionID+"/messages", map[string]any{"content": "inspect"})
			responses <- response.Code
		}(value.ID)
	}
	group.Wait()
	close(responses)
	counts := map[int]int{}
	for status := range responses {
		counts[status]++
	}
	if counts[http.StatusAccepted] != 1 || counts[http.StatusInsufficientStorage] != 1 {
		t.Fatalf("concurrent admissions crossed quota: %+v", counts)
	}
	tasks, err := store.ListTasks(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("task quota persisted %d tasks: %v", len(tasks), err)
	}
	deadline := time.Now().Add(time.Second)
	for !terminal(tasks[0].State) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		tasks[0], err = store.GetTask(ctx, tasks[0].ID)
		if err != nil {
			t.Fatal(err)
		}
	}
}
