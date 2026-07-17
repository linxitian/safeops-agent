package target

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/registry"
	"safeops-agent/internal/safefs"
)

type recordedTargetCall struct {
	serverID  string
	name      string
	arguments map[string]any
}

type fakeTargetCaller struct {
	calls   []recordedTargetCall
	errors  map[string]error
	results map[string]*mcp.CallToolResult
}

func (f *fakeTargetCaller) CallTool(_ context.Context, serverID, name string, arguments any) (*mcp.CallToolResult, error) {
	values, _ := arguments.(map[string]any)
	f.calls = append(f.calls, recordedTargetCall{serverID: serverID, name: name, arguments: values})
	if err := f.errors[name]; err != nil {
		return nil, err
	}
	if result, exists := f.results[name]; exists {
		return result, nil
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"ok": true}}, nil
}

func TestTargetToolCallPlanCoversDiscoveredToolsExactlyOnce(t *testing.T) {
	calls := targetToolCalls(4242)
	if len(calls) != 39 {
		t.Fatalf("call plan has %d tools, want 39", len(calls))
	}
	states := statesForCalls(calls)
	if err := validateTargetCallCoverage(states, calls); err != nil {
		t.Fatalf("valid call plan rejected: %v", err)
	}
	pidCalls := map[string]bool{"process.get_details": false, "process.get_resource_usage": false}
	for _, call := range calls {
		if _, exists := pidCalls[call.name]; !exists {
			continue
		}
		arguments, err := call.arguments(&targetCallState{})
		if err != nil {
			t.Fatal(err)
		}
		if arguments["pid"] != 4242 {
			t.Fatalf("%s used PID %#v, want 4242", call.name, arguments["pid"])
		}
		pidCalls[call.name] = true
	}
	for name, found := range pidCalls {
		if !found {
			t.Fatalf("dynamic PID call missing: %s", name)
		}
	}
}

func TestTargetToolCallsCaptureSafeDependentArguments(t *testing.T) {
	baseline := safefs.SnapshotEntry{RelativePath: "target.yaml", SizeBytes: 12, SHA256: strings.Repeat("a", 64)}
	caller := &fakeTargetCaller{errors: map[string]error{}, results: map[string]*mcp.CallToolResult{
		"config.snapshot": {
			StructuredContent: map[string]any{"snapshot": safefs.Snapshot{Root: targetLabConfigRoot, Entries: []safefs.SnapshotEntry{baseline}}},
		},
		"file.find_large": {
			StructuredContent: map[string]any{"files": []safefs.Metadata{{Path: targetLabRoot + "/fixture.log", SizeBytes: 1024, IsRegular: true}}},
		},
	}}
	report := newReport("test")
	appendTargetToolCallChecks(context.Background(), &report, caller, targetToolCalls(4242))
	if len(report.Checks) != 39 {
		t.Fatalf("got %d call checks, want 39", len(report.Checks))
	}
	for _, check := range report.Checks {
		if check.Status != Pass {
			t.Fatalf("call check failed: %+v", check)
		}
		if strings.Contains(check.Details, "fixture.log") || strings.Contains(check.Details, baseline.SHA256) {
			t.Fatalf("call detail leaked structured content: %q", check.Details)
		}
	}
	fileHash := recordedCall(t, caller.calls, "file.sha256")
	if fileHash.arguments["path"] != targetLabRoot+"/fixture.log" {
		t.Fatalf("file hash did not use captured bounded fixture: %#v", fileHash.arguments)
	}
	configDiff := recordedCall(t, caller.calls, "config.diff_snapshot")
	entries, ok := configDiff.arguments["baseline"].([]safefs.SnapshotEntry)
	if !ok || len(entries) != 1 || entries[0].SHA256 != baseline.SHA256 {
		t.Fatalf("config diff did not use captured baseline: %#v", configDiff.arguments["baseline"])
	}
}

func TestTargetToolCallsAggregateFailuresAndContinue(t *testing.T) {
	arguments := func(*targetCallState) (map[string]any, error) { return map[string]any{}, nil }
	calls := []targetToolCall{
		{serverID: "system", name: "system.first", arguments: arguments},
		{serverID: "system", name: "system.second", arguments: arguments},
		{serverID: "system", name: "system.third", arguments: arguments},
	}
	caller := &fakeTargetCaller{
		errors: map[string]error{"system.second": errors.New("permission denied\nsecret-looking-detail")},
		results: map[string]*mcp.CallToolResult{
			"system.third": {StructuredContent: nil},
		},
	}
	report := newReport("test")
	appendTargetToolCallChecks(context.Background(), &report, caller, calls)
	if len(caller.calls) != 3 || len(report.Checks) != 3 {
		t.Fatalf("failure stopped collection: calls=%d checks=%d", len(caller.calls), len(report.Checks))
	}
	if report.Checks[0].Status != Pass || report.Checks[1].Status != Fail || report.Checks[2].Status != Fail {
		t.Fatalf("unexpected aggregated statuses: %+v", report.Checks)
	}
	if strings.Contains(report.Checks[1].Details, "\n") || len(report.Checks[1].Details) > 403 {
		t.Fatalf("failure detail was not bounded: %q", report.Checks[1].Details)
	}
}

func TestTargetToolErrorDetailIsBoundedAndRedacted(t *testing.T) {
	result := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "snapshot denied api_key=must-not-leak Bearer token-value sk-abcdefghijk"}}}
	detail := mcpErrorDetail(result)
	for _, forbidden := range []string{"must-not-leak", "token-value", "sk-abcdefghijk"} {
		if strings.Contains(detail, forbidden) {
			t.Fatalf("secret %q leaked in %q", forbidden, detail)
		}
	}
	if !strings.Contains(detail, "snapshot denied") || !strings.Contains(detail, "[REDACTED]") {
		t.Fatalf("error detail lost its actionable text: %q", detail)
	}
}

func TestTargetToolCallCoverageReportsMissingDuplicateAndUnknown(t *testing.T) {
	calls := targetToolCalls(1)
	states := statesForCalls(calls)
	broken := append([]targetToolCall(nil), calls[1:]...)
	broken = append(broken, broken[0], targetToolCall{serverID: "unknown", name: "unknown.tool", arguments: broken[0].arguments})
	err := validateTargetCallCoverage(states, broken)
	if err == nil {
		t.Fatal("broken call plan unexpectedly passed")
	}
	for _, wanted := range []string{"missing ", "duplicate ", "not discovered unknown/unknown.tool"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("coverage error %q does not contain %q", err, wanted)
		}
	}
}

func statesForCalls(calls []targetToolCall) []registry.ServerState {
	byServer := map[string][]registry.ToolRecord{}
	for _, call := range calls {
		byServer[call.serverID] = append(byServer[call.serverID], registry.ToolRecord{ServerID: call.serverID, Name: call.name})
	}
	states := make([]registry.ServerState, 0, len(byServer))
	for serverID, tools := range byServer {
		states = append(states, registry.ServerState{Manifest: registry.ServerManifest{ID: serverID}, Tools: tools})
	}
	return states
}

func recordedCall(t *testing.T, calls []recordedTargetCall, name string) recordedTargetCall {
	t.Helper()
	for _, call := range calls {
		if call.name == name {
			return call
		}
	}
	t.Fatalf("call %s was not recorded", name)
	return recordedTargetCall{}
}
