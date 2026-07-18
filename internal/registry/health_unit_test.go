package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDependencyInspectionNeverAcceptsCommandArguments(t *testing.T) {
	checks, healthy := inspectDependencies([]string{"systemctl --version", "definitely-not-a-safeops-command"}, time.Unix(1, 0).UTC())
	if healthy || len(checks) != 2 {
		t.Fatalf("unexpected dependency result: healthy=%v checks=%+v", healthy, checks)
	}
	if checks[0].Kind != "invalid" || checks[0].Available {
		t.Fatalf("command with arguments was accepted: %+v", checks[0])
	}
	if checks[1].Kind != "command" || checks[1].Available {
		t.Fatalf("bare missing command was not lookup-only: %+v", checks[1])
	}
}

func TestDependencyInspectionReturnsFilesystemMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	checks, healthy := inspectDependencies([]string{path}, time.Unix(1, 0).UTC())
	if !healthy || len(checks) != 1 || !checks[0].Available || checks[0].Resolved != path || checks[0].Mode == "" || checks[0].IsDir || checks[0].SizeBytes != 2 || checks[0].ModifiedAt.IsZero() {
		t.Fatalf("filesystem metadata was not retained: %+v", checks)
	}
}

func TestRegistryFailureDetailsAreRedactedAndBounded(t *testing.T) {
	detail := boundedRegistryDetail("api_key=must-not-leak " + strings.Repeat("x", 600))
	if strings.Contains(detail, "must-not-leak") || !strings.Contains(detail, "[REDACTED]") || len([]rune(detail)) > 515 {
		t.Fatalf("failure detail was not safely bounded: %q", detail)
	}
}

func TestRegistryHistoryLimit(t *testing.T) {
	var health []HealthRecord
	var discovery []DiscoveryRecord
	for index := 0; index < 40; index++ {
		health = appendBounded(health, HealthRecord{DurationMillis: int64(index)})
		discovery = appendBounded(discovery, DiscoveryRecord{ToolCount: index})
	}
	if len(health) != registryHistoryLimit || health[0].DurationMillis != 8 || health[len(health)-1].DurationMillis != 39 {
		t.Fatalf("unexpected bounded health history: %+v", health)
	}
	if len(discovery) != registryHistoryLimit || discovery[0].ToolCount != 8 || discovery[len(discovery)-1].ToolCount != 39 {
		t.Fatalf("unexpected bounded discovery history: %+v", discovery)
	}
}
