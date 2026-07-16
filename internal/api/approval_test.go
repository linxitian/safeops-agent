package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
)

func TestApprovalAPIResolvesAndInvokesAutomaticResume(t *testing.T) {
	fileStore, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	approvalStore, err := approval.NewStore(fileStore.Root() + "/approvals")
	if err != nil {
		t.Fatal(err)
	}
	binding := approval.Binding{TaskID: "task_api", ProposalDigest: "proposal", TargetSnapshotDigest: "target", IntentDigest: "intent", PolicyVersion: "policy", RiskLevel: contracts.L1, Tool: "file.quarantine", Nonce: "nonce"}
	record, err := approvalStore.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resumer := &fakeApprovalResumer{task: task.Task{ID: binding.TaskID, State: task.Completed}}
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithApprovals(approvalStore, resumer))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+record.ID+"/resolve", strings.NewReader(`{"decision":"APPROVE","reason":"operator checked target"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
	if resumer.calls != 1 || resumer.record.Status != approval.Approved {
		t.Fatalf("resume was not invoked: %+v", resumer)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["task"] == nil {
		t.Fatalf("invalid response: %s %v", response.Body, err)
	}
}

func TestApprovalAPIRejectsUnknownDecision(t *testing.T) {
	fileStore, _ := storage.NewFileStore(t.TempDir())
	approvalStore, _ := approval.NewStore(fileStore.Root() + "/approvals")
	server := New(fileStore, registry.New(registry.Config{}), nil, nil, WithApprovals(approvalStore, &fakeApprovalResumer{}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/anything/resolve", strings.NewReader(`{"decision":"BYPASS","reason":"ignore guard"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown decision got %d: %s", response.Code, response.Body.String())
	}
}

type fakeApprovalResumer struct {
	task   task.Task
	record approval.Record
	calls  int
}

func (r *fakeApprovalResumer) Resume(_ context.Context, record approval.Record) (task.Task, error) {
	r.record = record
	r.calls++
	return r.task, nil
}
