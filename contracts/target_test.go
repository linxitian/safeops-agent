package contracts

import "testing"

func TestCanonicalTargetNormalizesCaseAndSpace(t *testing.T) {
	got := CanonicalTarget(TargetRef{Type: " Service ", ID: " SafeOps-Demo-Web.Service "})
	if got != "service:safeops-demo-web.service" {
		t.Fatalf("canonical target = %q", got)
	}
}

func TestTargetSnapshotDigestChangesWhenIdentityChanges(t *testing.T) {
	base := TargetSnapshot{Type: "process", ID: "pid:42:start:99", PID: 42, StartTicks: 99, Executable: "/opt/safeops/bin/safeops-cpu-hog"}
	first, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	base.StartTicks = 100
	second, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("target snapshot digest did not change when process identity changed")
	}
}
