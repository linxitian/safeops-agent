package benchmark

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type intentFixture struct {
	id       string
	category string
	request  string
	labels   []string
	tools    []string
}

var intentFixtures = []intentFixture{
	{id: "intent_standard_cpu", category: "standard expression", request: "查看 CPU 使用率", labels: []string{"cpu"}, tools: []string{"system.get_cpu_metrics"}},
	{id: "intent_standard_memory", category: "standard expression", request: "查看内存使用情况", labels: []string{"memory"}, tools: []string{"system.get_memory_metrics"}},
	{id: "intent_combined", category: "standard expression", request: "查看 CPU 和内存", labels: []string{"cpu", "memory"}, tools: []string{"system.get_cpu_metrics", "system.get_memory_metrics"}},
	{id: "intent_colloquial_cpu", category: "colloquial expression", request: "处理器现在忙不忙", labels: []string{"cpu"}, tools: []string{"system.get_cpu_metrics"}},
	{id: "intent_colloquial_memory", category: "colloquial expression", request: "机器是不是内存不够", labels: []string{"memory"}, tools: []string{"system.get_memory_metrics"}},
	{id: "intent_ellipsis_cpu", category: "ellipsis", request: "cpu 呢", labels: []string{"cpu"}, tools: []string{"system.get_cpu_metrics"}},
	{id: "intent_mixed_language", category: "mixed language", request: "查查处理器和 memory", labels: []string{"cpu", "memory"}, tools: []string{"system.get_cpu_metrics", "system.get_memory_metrics"}},
}

func runIntent(report *Report) {
	started := time.Now()
	suite := Suite{Name: "intent"}
	correct := 0
	for _, fixture := range intentFixtures {
		caseStarted := time.Now()
		actual, err := agent.ClassifyMetricIntent(fixture.request)
		passed := err == nil && slices.Equal(actual, fixture.labels)
		if passed {
			correct++
		}
		details := fmt.Sprintf("expected=%v actual=%v", fixture.labels, actual)
		if err != nil {
			details += " error=" + err.Error()
		}
		suite.Cases = append(suite.Cases, Case{ID: fixture.id, Category: fixture.category, Passed: passed, Details: details, Duration: time.Since(caseStarted)})
	}
	report.setRate("Intent Classification Accuracy", correct, len(intentFixtures), "exact label-set match against the production deterministic metric classifier")
	suite.finish(started)
	report.addSuite(suite)
}

func runToolSelection(ctx context.Context, report *Report, policyPath string) {
	started := time.Now()
	suite := Suite{Name: "tool-selection"}
	catalog, err := guard.LoadCatalog(policyPath)
	if err != nil {
		suite.Error = err.Error()
		suite.finish(started)
		report.addSuite(suite)
		return
	}
	root, err := os.MkdirTemp("", "safeops-bench-tools-*")
	if err != nil {
		suite.Error = err.Error()
		suite.finish(started)
		report.addSuite(suite)
		return
	}
	defer os.RemoveAll(root)
	store, err := storage.NewFileStore(root)
	if err != nil {
		suite.Error = err.Error()
		suite.finish(started)
		report.addSuite(suite)
		return
	}
	traceWriter, err := trace.NewWriter(root + "/traces")
	if err != nil {
		suite.Error = err.Error()
		suite.finish(started)
		report.addSuite(suite)
		return
	}
	orchestrator := agent.Orchestrator{Store: store, Registry: benchmarkMetricTools{}, Safety: guard.NewSafetyPipeline(catalog), Trace: traceWriter, ToolTimeout: time.Second}
	selectedCorrect, completed := 0, 0
	for index, fixture := range intentFixtures {
		caseStarted := time.Now()
		sessionID := fmt.Sprintf("bench_tools_session_%d", index)
		taskID := fmt.Sprintf("bench_tools_task_%d", index)
		now := time.Now().UTC()
		value := session.Session{ID: sessionID, Name: fixture.id, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveSession(ctx, value); err != nil {
			suite.Cases = append(suite.Cases, Case{ID: fixture.id, Category: fixture.category, Passed: false, Details: err.Error(), Duration: time.Since(caseStarted)})
			continue
		}
		_, prepareErr := orchestrator.Prepare(ctx, taskID, sessionID, fixture.request)
		result, runErr := orchestrator.Run(ctx, taskID, sessionID, fixture.request, nil)
		actualTools := make([]string, 0, len(result.Plan))
		for _, step := range result.Plan {
			actualTools = append(actualTools, step.Tool)
		}
		selectionPassed := prepareErr == nil && runErr == nil && slices.Equal(actualTools, fixture.tools)
		completionPassed := runErr == nil && result.State == task.Completed
		if selectionPassed {
			selectedCorrect++
		}
		if completionPassed {
			completed++
		}
		details := fmt.Sprintf("expected_tools=%v actual_tools=%v state=%s", fixture.tools, actualTools, result.State)
		if prepareErr != nil {
			details += " prepare_error=" + prepareErr.Error()
		}
		if runErr != nil {
			details += " run_error=" + runErr.Error()
		}
		suite.Cases = append(suite.Cases, Case{ID: fixture.id, Category: fixture.category, Passed: selectionPassed && completionPassed, Details: details, Duration: time.Since(caseStarted)})
	}
	report.setRate("Tool Selection Accuracy", selectedCorrect, len(intentFixtures), "exact ordered plan-tool match from the production Orchestrator")
	report.setRate("Task Completion Rate", completed, len(intentFixtures), "durable Orchestrator tasks reaching COMPLETED with evidence-backed fake MCP observations")
	suite.finish(started)
	report.addSuite(suite)
}

type benchmarkMetricTools struct{}

func (benchmarkMetricTools) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	if server != "system" {
		return nil, errors.New("unexpected MCP server")
	}
	switch name {
	case "system.get_cpu_metrics":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"usage_percent": 12.5, "cpu": map[string]any{"total_ticks": 100.0}}}, nil
	case "system.get_memory_metrics":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"usage_percent": 50.0, "memory": map[string]any{"used_bytes": float64(2 << 30), "total_bytes": float64(4 << 30), "available_bytes": float64(2 << 30)}}}, nil
	default:
		return nil, errors.New("unexpected MCP tool")
	}
}
