package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/registry"
	"safeops-agent/internal/safefs"
)

const (
	targetLabRoot       = "/var/lib/safeops/lab"
	targetLabConfigRoot = "/var/lib/safeops/lab/config"
	targetConfigPath    = "/etc/safeops/mcp_servers.yaml"
	targetServiceUnit   = "safeops-server.service"
	targetServicePort   = 8080
	targetCallTimeout   = 5 * time.Second
)

var targetErrorSecretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|secret|password|passwd|authorization)\s*[:=]\s*)[^\s,;]+`), `${1}[REDACTED]`},
	{regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`), `Bearer [REDACTED]`},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), `[REDACTED]`},
}

type targetToolCaller interface {
	CallTool(context.Context, string, string, any) (*mcp.CallToolResult, error)
}

type targetCallState struct {
	pid                 int
	filePath            string
	configBaseline      []safefs.SnapshotEntry
	configSnapshotReady bool
}

type targetToolCall struct {
	serverID  string
	name      string
	arguments func(*targetCallState) (map[string]any, error)
	capture   func(any, *targetCallState) error
}

func targetToolCalls(pid int) []targetToolCall {
	empty := func(*targetCallState) (map[string]any, error) { return map[string]any{}, nil }
	fixed := func(arguments map[string]any) func(*targetCallState) (map[string]any, error) {
		return func(*targetCallState) (map[string]any, error) { return arguments, nil }
	}
	return []targetToolCall{
		{serverID: "config", name: "config.list_roots", arguments: empty},
		{serverID: "config", name: "config.get_metadata", arguments: fixed(map[string]any{"path": targetConfigPath})},
		{serverID: "config", name: "config.snapshot", arguments: fixed(map[string]any{"path": targetLabConfigRoot, "limit": 100, "max_file_bytes": 1 << 20, "max_total_bytes": 4 << 20}), capture: captureConfigSnapshot},
		{serverID: "config", name: "config.diff_snapshot", arguments: configDiffArguments},
		{serverID: "diagnostic", name: "diagnostic.build_snapshot", arguments: fixed(map[string]any{"path": "/", "unit": targetServiceUnit, "port": targetServicePort})},
		{serverID: "diagnostic", name: "diagnostic.disk_pressure", arguments: fixed(map[string]any{"path": "/", "warning_ratio": 0.85})},
		{serverID: "diagnostic", name: "diagnostic.high_cpu", arguments: fixed(map[string]any{"limit": 10})},
		{serverID: "diagnostic", name: "diagnostic.port_conflict", arguments: fixed(map[string]any{"unit": targetServiceUnit, "port": targetServicePort})},
		{serverID: "file", name: "file.list_roots", arguments: empty},
		{serverID: "file", name: "file.stat", arguments: fixed(map[string]any{"path": targetLabRoot})},
		{serverID: "file", name: "file.list_directory", arguments: fixed(map[string]any{"path": targetLabRoot, "limit": 100})},
		{serverID: "file", name: "file.find_large", arguments: fixed(map[string]any{"path": targetLabRoot, "minimum_bytes": 0, "max_depth": 16, "limit": 200}), capture: captureLabFile},
		{serverID: "file", name: "file.sha256", arguments: fileHashArguments},
		{serverID: "journal", name: "journal.get_recent", arguments: fixed(map[string]any{"lines": 20})},
		{serverID: "journal", name: "journal.query_unit", arguments: fixed(map[string]any{"unit": targetServiceUnit, "lines": 20})},
		{serverID: "journal", name: "journal.search_errors", arguments: fixed(map[string]any{"unit": targetServiceUnit, "lines": 50})},
		{serverID: "journal", name: "journal.get_priority_events", arguments: fixed(map[string]any{"lines": 20, "priority": 3})},
		{serverID: "network", name: "network.list_listeners", arguments: fixed(map[string]any{"limit": 200})},
		{serverID: "network", name: "network.list_connections", arguments: fixed(map[string]any{"limit": 200})},
		{serverID: "network", name: "network.check_port", arguments: fixed(map[string]any{"port": targetServicePort, "protocol": "tcp"})},
		{serverID: "network", name: "network.get_interfaces", arguments: empty},
		{serverID: "network", name: "network.get_interface_stats", arguments: empty},
		{serverID: "process", name: "process.list_top", arguments: fixed(map[string]any{"limit": 20, "sort_by": "cpu"})},
		{serverID: "process", name: "process.search", arguments: fixed(map[string]any{"query": "targetctl", "limit": 20})},
		{serverID: "process", name: "process.get_details", arguments: fixed(map[string]any{"pid": pid})},
		{serverID: "process", name: "process.get_resource_usage", arguments: fixed(map[string]any{"pid": pid})},
		{serverID: "process", name: "process.find_by_port", arguments: fixed(map[string]any{"port": targetServicePort})},
		{serverID: "service", name: "service.get_status", arguments: fixed(map[string]any{"unit": targetServiceUnit})},
		{serverID: "service", name: "service.list_failed", arguments: empty},
		{serverID: "service", name: "service.get_dependencies", arguments: fixed(map[string]any{"unit": targetServiceUnit})},
		{serverID: "service", name: "service.get_restart_count", arguments: fixed(map[string]any{"unit": targetServiceUnit})},
		{serverID: "system", name: "system.get_cpu_metrics", arguments: empty},
		{serverID: "system", name: "system.get_memory_metrics", arguments: empty},
		{serverID: "system", name: "system.get_load_average", arguments: empty},
		{serverID: "system", name: "system.get_disk_usage", arguments: fixed(map[string]any{"path": "/"})},
		{serverID: "system", name: "system.get_mounts", arguments: empty},
		{serverID: "system", name: "system.get_kernel_info", arguments: empty},
		{serverID: "system", name: "system.get_uptime", arguments: empty},
		{serverID: "system", name: "system.get_overview", arguments: empty},
	}
}

func configDiffArguments(state *targetCallState) (map[string]any, error) {
	if !state.configSnapshotReady {
		return nil, errors.New("config.snapshot did not provide a baseline")
	}
	return map[string]any{
		"path":            targetLabConfigRoot,
		"baseline":        state.configBaseline,
		"limit":           100,
		"max_file_bytes":  1 << 20,
		"max_total_bytes": 4 << 20,
	}, nil
}

func fileHashArguments(state *targetCallState) (map[string]any, error) {
	if state.filePath == "" {
		return nil, fmt.Errorf("no regular file at or below %d bytes was found under %s", safefs.DefaultHashLimit, targetLabRoot)
	}
	return map[string]any{"path": state.filePath, "max_bytes": safefs.DefaultHashLimit}, nil
}

func captureConfigSnapshot(value any, state *targetCallState) error {
	var output struct {
		Snapshot safefs.Snapshot `json:"snapshot"`
	}
	if err := decodeStructured(value, &output); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	if filepath.Clean(output.Snapshot.Root) != targetLabConfigRoot {
		return fmt.Errorf("snapshot root is %q, expected %q", output.Snapshot.Root, targetLabConfigRoot)
	}
	state.configBaseline = append([]safefs.SnapshotEntry(nil), output.Snapshot.Entries...)
	state.configSnapshotReady = true
	return nil
}

func captureLabFile(value any, state *targetCallState) error {
	var output struct {
		Files []safefs.Metadata `json:"files"`
	}
	if err := decodeStructured(value, &output); err != nil {
		return fmt.Errorf("decode file candidates: %w", err)
	}
	for _, file := range output.Files {
		if !file.IsRegular || file.SizeBytes < 0 || file.SizeBytes > safefs.DefaultHashLimit {
			continue
		}
		cleaned := filepath.Clean(file.Path)
		relative, err := filepath.Rel(targetLabRoot, cleaned)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		state.filePath = cleaned
		return nil
	}
	return fmt.Errorf("no regular file at or below %d bytes was returned from %s", safefs.DefaultHashLimit, targetLabRoot)
}

func decodeStructured(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func validateTargetCallCoverage(states []registry.ServerState, calls []targetToolCall) error {
	discovered := map[string]bool{}
	for _, state := range states {
		for _, tool := range state.Tools {
			discovered[state.Manifest.ID+"\x00"+tool.Name] = true
		}
	}
	planned := map[string]int{}
	for _, call := range calls {
		planned[call.serverID+"\x00"+call.name]++
	}
	var problems []string
	for key := range discovered {
		if planned[key] == 0 {
			problems = append(problems, "missing "+displayToolKey(key))
		}
	}
	for key, count := range planned {
		if count > 1 {
			problems = append(problems, fmt.Sprintf("duplicate %s (%d)", displayToolKey(key), count))
		}
		if !discovered[key] {
			problems = append(problems, "not discovered "+displayToolKey(key))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func displayToolKey(key string) string {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return key
	}
	return parts[0] + "/" + parts[1]
}

func appendTargetToolCallChecks(ctx context.Context, report *Report, caller targetToolCaller, calls []targetToolCall) {
	state := &targetCallState{pid: os.Getpid()}
	for _, call := range calls {
		status, details := invokeTargetTool(ctx, caller, call, state)
		report.Checks = append(report.Checks, Check{Name: "mcp_call_" + strings.ReplaceAll(call.name, ".", "_"), Status: status, Details: details})
	}
}

func invokeTargetTool(ctx context.Context, caller targetToolCaller, call targetToolCall, state *targetCallState) (Status, string) {
	arguments, err := call.arguments(state)
	if err != nil {
		return Fail, boundedDetail("arguments: " + err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, targetCallTimeout)
	defer cancel()
	result, err := caller.CallTool(callCtx, call.serverID, call.name, arguments)
	if err != nil {
		return Fail, boundedDetail(err.Error())
	}
	if result == nil {
		return Fail, "MCP tool returned no result"
	}
	if result.IsError {
		return Fail, mcpErrorDetail(result)
	}
	if result.StructuredContent == nil {
		return Fail, "MCP tool returned no structured content"
	}
	if call.capture != nil {
		if err := call.capture(result.StructuredContent, state); err != nil {
			return Fail, boundedDetail(err.Error())
		}
	}
	return Pass, "structured MCP call succeeded"
}

func boundedDetail(value string) string {
	const maximum = 400
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
	for _, secret := range targetErrorSecretPatterns {
		value = secret.pattern.ReplaceAllString(value, secret.replacement)
	}
	if len(value) > maximum {
		return value[:maximum] + "..."
	}
	return value
}

func mcpErrorDetail(result *mcp.CallToolResult) string {
	var details []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && strings.TrimSpace(text.Text) != "" {
			details = append(details, text.Text)
		}
	}
	if len(details) == 0 {
		return "MCP tool returned IsError"
	}
	return boundedDetail("MCP tool error: " + strings.Join(details, "; "))
}
