package approval

import (
	"context"
	"os"
	"testing"
	"time"

	"safeops-agent/contracts"
)

func TestApprovalBindingAndSingleConsumption(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{TaskID: "task", ProposalDigest: "proposal", TargetSnapshotDigest: "target", IntentDigest: "intent", PolicyVersion: "policy", RiskLevel: contracts.L2, Tool: "process.terminate", Nonce: "nonce"}
	record, err := store.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(context.Background(), record.ID, true, "approved by operator"); err != nil {
		t.Fatal(err)
	}
	if err := store.Validate(context.Background(), record.ID, binding); err != nil {
		t.Fatal(err)
	}
	tampered := binding
	tampered.Tool = "service.restart"
	if err := store.Validate(context.Background(), record.ID, tampered); err == nil {
		t.Fatal("tampered binding accepted")
	}
	if err := store.Consume(context.Background(), record.ID, binding); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(context.Background(), record.ID, binding); err == nil {
		t.Fatal("approval consumed twice")
	}
}

func TestApprovalRecordUsesBoundaryReadableMode(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{TaskID: "task", ProposalDigest: "proposal", TargetSnapshotDigest: "target", IntentDigest: "intent", PolicyVersion: "policy", RiskLevel: contracts.L2, Tool: "process.terminate", Nonce: "nonce"}
	record, err := store.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("approval mode = %04o, want 0640", got)
	}
}

func TestApprovalRejectIsIdempotent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{TaskID: "t", ProposalDigest: "p", TargetSnapshotDigest: "s", IntentDigest: "i", PolicyVersion: "v", RiskLevel: contracts.L1, Tool: "service.restart", Nonce: "n"}
	record, err := store.Create(context.Background(), binding, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Resolve(context.Background(), record.ID, false, "no")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Resolve(context.Background(), record.ID, false, "no")
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != Rejected || second.Status != Rejected {
		t.Fatal("rejection not persisted")
	}
}

func TestApprovalListExpiresPendingRecords(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	binding := Binding{TaskID: "t", ProposalDigest: "p", TargetSnapshotDigest: "s", IntentDigest: "i", PolicyVersion: "v", RiskLevel: contracts.L1, Tool: "service.restart", Nonce: "n"}
	if _, err := store.Create(context.Background(), binding, time.Second); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	records, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != Expired {
		t.Fatalf("pending record did not expire: %+v", records)
	}
}
