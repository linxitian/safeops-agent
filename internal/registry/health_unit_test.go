package registry

import (
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

func TestRegistryFailureDetailsAreRedactedAndBounded(t *testing.T) {
	detail := boundedRegistryDetail("api_key=must-not-leak " + strings.Repeat("x", 600))
	if strings.Contains(detail, "must-not-leak") || !strings.Contains(detail, "[REDACTED]") || len([]rune(detail)) > 515 {
		t.Fatalf("failure detail was not safely bounded: %q", detail)
	}
}

func TestRegistryHistoryLimit(t *testing.T) {
	var history []HealthRecord
	for index := 0; index < 40; index++ {
		history = appendBounded(history, HealthRecord{DurationMillis: int64(index)})
	}
	if len(history) != registryHistoryLimit || history[0].DurationMillis != 8 || history[len(history)-1].DurationMillis != 39 {
		t.Fatalf("unexpected bounded history: %+v", history)
	}
}
