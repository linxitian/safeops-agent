package id

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestNewUsesPrefixAndHexEntropy(t *testing.T) {
	first := New("task")
	second := New("task")
	if first == second {
		t.Fatal("generated two identical IDs")
	}
	for _, value := range []string{first, second} {
		if !strings.HasPrefix(value, "task_") {
			t.Fatalf("ID %q is missing requested prefix", value)
		}
		payload := strings.TrimPrefix(value, "task_")
		if len(payload) != 24 {
			t.Fatalf("ID payload length = %d, want 24 hex chars", len(payload))
		}
		if _, err := hex.DecodeString(payload); err != nil {
			t.Fatalf("ID payload is not hex: %q: %v", payload, err)
		}
	}
}
