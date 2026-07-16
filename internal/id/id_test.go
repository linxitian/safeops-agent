package id

import (
	"regexp"
	"testing"
)

func TestNewReturnsPrefixedHexIdentifier(t *testing.T) {
	got := New("task")
	if matched := regexp.MustCompile(`^task_[0-9a-f]{24}$`).MatchString(got); !matched {
		t.Fatalf("unexpected id format: %q", got)
	}
}

func TestNewDoesNotRepeatAcrossBatch(t *testing.T) {
	seen := make(map[string]struct{}, 512)
	for range 512 {
		got := New("ses")
		if _, exists := seen[got]; exists {
			t.Fatalf("duplicate id generated: %s", got)
		}
		seen[got] = struct{}{}
	}
}
