package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const registryHistoryLimit = 32

var registrySecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|secret|password|passwd|authorization)\s*[:=]\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`),
}

// CheckAll performs one non-overlapping, bounded health cycle over enabled
// developer-authored manifests. A disconnected server is rediscovered; an
// existing session is pinged and its dependencies are checked without running
// dependency strings.
func (r *Registry) CheckAll(ctx context.Context, perServerTimeout time.Duration) error {
	if perServerTimeout <= 0 {
		return errors.New("MCP per-server health timeout must be positive")
	}
	r.mu.RLock()
	ids := make([]string, 0, len(r.manifests))
	for id, manifest := range r.manifests {
		if manifest.Enabled {
			ids = append(ids, id)
		}
	}
	r.mu.RUnlock()
	sort.Strings(ids)
	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		checkCtx, cancel := context.WithTimeout(ctx, perServerTimeout)
		r.mu.RLock()
		session := r.sessions[id]
		r.mu.RUnlock()
		var err error
		if session == nil {
			err = r.Discover(checkCtx, id)
		} else {
			err = r.Health(checkCtx, id)
		}
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// RunHealthLoop waits one interval after startup and then runs bounded cycles
// until ctx ends. The single loop body prevents overlapping cycles.
func (r *Registry) RunHealthLoop(ctx context.Context, interval, perServerTimeout time.Duration) error {
	if interval <= 0 {
		return errors.New("MCP health interval must be positive")
	}
	if perServerTimeout <= 0 {
		return errors.New("MCP per-server health timeout must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = r.CheckAll(ctx, perServerTimeout)
		}
	}
}

func inspectDependencies(values []string, now time.Time) ([]DependencyState, bool) {
	checks := make([]DependencyState, 0, len(values))
	healthy := true
	for _, raw := range values {
		name := strings.TrimSpace(raw)
		check := DependencyState{Name: name, CheckedAt: now}
		switch {
		case filepath.IsAbs(name):
			check.Kind = "path"
			if _, err := os.Stat(name); err != nil {
				check.Error = boundedRegistryDetail(err.Error())
			} else {
				check.Available = true
				check.Resolved = filepath.Clean(name)
			}
		case validDependencyCommand(name):
			check.Kind = "command"
			if resolved, err := exec.LookPath(name); err != nil {
				check.Error = boundedRegistryDetail(err.Error())
			} else {
				check.Available = true
				check.Resolved = resolved
			}
		default:
			check.Kind = "invalid"
			check.Error = "dependency must be an absolute path or a bare command name"
		}
		if !check.Available {
			healthy = false
		}
		checks = append(checks, check)
	}
	return checks, healthy
}

func validDependencyCommand(value string) bool {
	if value == "" || strings.HasPrefix(value, "-") || strings.ContainsRune(value, filepath.Separator) {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._+-", char) {
			continue
		}
		return false
	}
	return true
}

func dependencyFailure(checks []DependencyState) error {
	missing := make([]string, 0, len(checks))
	for _, check := range checks {
		if !check.Available {
			missing = append(missing, firstNonEmptyRegistry(check.Name, "<empty>"))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("MCP dependencies unavailable: %s", strings.Join(missing, ", "))
}

func boundedRegistryDetail(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	for index, pattern := range registrySecretPatterns {
		replacement := `${1}[REDACTED]`
		if index == 1 {
			replacement = `Bearer [REDACTED]`
		}
		value = pattern.ReplaceAllString(value, replacement)
	}
	runes := []rune(value)
	if len(runes) > 512 {
		value = string(runes[:512]) + "..."
	}
	return value
}

func appendBounded[T any](values []T, value T) []T {
	values = append(values, value)
	if len(values) > registryHistoryLimit {
		values = append([]T(nil), values[len(values)-registryHistoryLimit:]...)
	}
	return values
}

func firstNonEmptyRegistry(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
