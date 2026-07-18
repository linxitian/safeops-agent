package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/contracts"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func TestGeneralRuntimeReentersPlannerWithToolResult(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_general", Name: "general", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取系统负载", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}, ExpectedObservation: "负载"},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于负载证据完成", FinalAnswer: "当前负载已经通过真实 MCP 证据确认。"},
	}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_general", s.ID, "查看当前系统负载"); err != nil {
		t.Fatal(err)
	}
	completed, err := orchestrator.Run(ctx, "task_general", s.ID, "查看当前系统负载", nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.Iterations != 2 || completed.Runtime.ToolCalls != 1 || len(completed.Runtime.Observations) != 1 {
		t.Fatalf("unexpected runtime state: %+v", completed)
	}
	if !planner.sawObservation {
		t.Fatal("tool result did not re-enter the planner")
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeRecordsReadToolFailureAndContinues(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_tool_failure", Name: "general", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := fakeRecoverableToolFailureTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "尝试读取旧 Lab 日志目录", ServerID: "file", Tool: "file.list_directory", Arguments: map[string]any{"path": "/var/lib/safeops/lab"}, ExpectedObservation: "日志目录项"},
		{Kind: llm.DecisionTool, DecisionSummary: "改用系统负载证据", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}, ExpectedObservation: "系统负载"},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于可用证据完成", FinalAnswer: "文件目录不可用，但系统状态已经通过 MCP 证据确认。"},
	}}
	var events []RuntimeEvent
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_tool_failure", s.ID, "查找日志并说明运行情况"); err != nil {
		t.Fatal(err)
	}
	completed, err := orchestrator.Run(ctx, "task_tool_failure", s.ID, "查找日志并说明运行情况", func(event RuntimeEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.ToolCalls != 2 || len(completed.Runtime.Observations) != 2 || len(completed.EvidenceRefs) != 2 {
		t.Fatalf("recoverable tool failure did not continue: %+v", completed)
	}
	if completed.Plan[0].State != "FAILED" || completed.Plan[1].State != "COMPLETED" {
		t.Fatalf("unexpected plan states after recoverable failure: %+v", completed.Plan)
	}
	var failureObservation map[string]any
	if err := json.Unmarshal(completed.Runtime.Observations[0].Result, &failureObservation); err != nil {
		t.Fatal(err)
	}
	if failureObservation["recoverable"] != true || failureObservation["status"] != "error" {
		t.Fatalf("tool failure was not exposed as recoverable observation: %+v", failureObservation)
	}
	if len(planner.requests) < 2 || len(planner.requests[1].Observations) != 1 {
		t.Fatalf("planner did not receive failed tool observation: %+v", planner.requests)
	}
	sawRecoverableEvent := false
	for _, event := range events {
		if strings.Contains(event.Message, "工具返回错误，已记录并继续调查") {
			sawRecoverableEvent = true
			break
		}
	}
	if !sawRecoverableEvent {
		t.Fatal("operator was not told that the tool failure was recoverable")
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeProvidesDurableSessionContextToPlanner(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{
		ID:   "ses_followup",
		Name: "followup",
		Messages: []session.Message{
			{ID: "msg_1", Role: session.RoleUser, Content: "列出 Lab 大文件", TaskID: "task_prior", CreatedAt: now},
			{ID: "msg_2", Role: session.RoleAssistant, Content: "已找到三个文件。", TaskID: "task_prior", CreatedAt: now},
		},
		SelectedResources: []string{"/var/lib/safeops/lab/demo-3.log", "/var/lib/safeops/lab/demo-2.log", "/var/lib/safeops/lab/demo-1.log"},
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取已选文件范围", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于证据完成", FinalAnswer: "已完成有界调查。"},
	}}
	tools := fakeGeneralTools{}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_followup", s.ID, "哪些建议处理？"); err != nil {
		t.Fatal(err)
	}
	if _, err := orchestrator.Run(ctx, "task_followup", s.ID, "哪些建议处理？", nil); err != nil {
		t.Fatal(err)
	}
	if len(planner.requests) == 0 || planner.requests[0].SessionContext == nil {
		t.Fatal("planner did not receive durable session context")
	}
	context := planner.requests[0].SessionContext
	if len(context.RecentMessages) != 2 || context.RecentMessages[0].Content != "列出 Lab 大文件" {
		t.Fatalf("unexpected recent context: %+v", context.RecentMessages)
	}
	if len(context.SelectedResources) != 3 || context.SelectedResources[2] != "/var/lib/safeops/lab/demo-1.log" {
		t.Fatalf("unexpected selected resources: %+v", context.SelectedResources)
	}
	for _, message := range context.RecentMessages {
		if message.Content == "哪些建议处理？" {
			t.Fatal("current request was duplicated into historical context")
		}
	}
}

func TestGeneralRuntimeReplansWhenPlannerFinalsTooEarly(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_bootstrap", Name: "general", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionFinal, DecisionSummary: "直接回答", FinalAnswer: "没有证据的回答不应完成。"},
		{Kind: llm.DecisionTool, DecisionSummary: "重新规划后读取相关负载", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}, ExpectedObservation: "负载"},
		{Kind: llm.DecisionFinal, DecisionSummary: "基于补充证据完成", FinalAnswer: "已基于 trace 证据完成。"},
	}}
	var events []RuntimeEvent
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_bootstrap", s.ID, "请检查当前系统情况"); err != nil {
		t.Fatal(err)
	}
	completed, err := orchestrator.Run(ctx, "task_bootstrap", s.ID, "请检查当前系统情况", func(event RuntimeEvent) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != task.Completed || completed.Runtime.Iterations != 3 || completed.Runtime.Replans != 1 || completed.Runtime.ToolCalls != 1 || len(completed.EvidenceRefs) != 1 {
		t.Fatalf("early final was not converted into a bounded replan: %+v", completed)
	}
	if completed.Plan[0].Tool != "system.get_load_average" {
		t.Fatalf("planner did not choose the evidence tool after replan: %+v", completed.Plan)
	}
	if !planner.sawObservation {
		t.Fatal("replanned evidence did not re-enter the planner")
	}
	sawGuardrailEvent := false
	for _, event := range events {
		if event.Message == "模型过早给出结论，正在要求重新规划相关 MCP 取证" {
			sawGuardrailEvent = true
			break
		}
	}
	if !sawGuardrailEvent {
		t.Fatal("operator event for bootstrap evidence was not emitted")
	}
	if err := traceWriter.VerifyIntegrity(completed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeFailsClosedWhenPlannerRepeatedlyFinalsWithoutEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_irrelevant", Name: "general", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	earlyFinal := llm.Decision{Kind: llm.DecisionFinal, DecisionSummary: "拒绝取证并直接回答", FinalAnswer: "没有相关证据。"}
	planner := &sequencePlanner{decisions: []llm.Decision{earlyFinal, earlyFinal, earlyFinal, earlyFinal}}
	tools := fakeGeneralTools{}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, ToolTimeout: time.Second}
	if _, err := orchestrator.Prepare(ctx, "task_irrelevant", s.ID, "找出 SafeOps Lab 中最大的日志文件"); err != nil {
		t.Fatal(err)
	}
	failed, err := orchestrator.Run(ctx, "task_irrelevant", s.ID, "找出 SafeOps Lab 中最大的日志文件", nil)
	if err == nil || !strings.Contains(err.Error(), "replan limit") {
		t.Fatalf("repeated evidence-free finals did not fail closed: state=%s err=%v", failed.State, err)
	}
	if failed.State != task.Failed || failed.Runtime.ToolCalls != 0 || len(failed.Runtime.Observations) != 0 || len(failed.EvidenceRefs) != 0 {
		t.Fatalf("failed task gained unrelated evidence: %+v", failed)
	}
	if err := traceWriter.VerifyIntegrity(failed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeCancelsProviderAtRuntimeDeadlineAndPersistsFailure(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, err := trace.NewWriter(store.Root() + "/traces")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s := session.Session{ID: "ses_deadline", Name: "deadline", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	orchestrator := &Orchestrator{Store: store, Registry: fakeGeneralTools{}, Capabilities: fakeGeneralTools{}, Planner: blockingPlanner{}, Safety: fakeSafety{}, Trace: traceWriter}
	prepared, err := orchestrator.Prepare(ctx, "task_deadline", s.ID, "检查 Lab 大文件")
	if err != nil {
		t.Fatal(err)
	}
	prepared.Runtime.DeadlineAt = time.Now().UTC().Add(100 * time.Millisecond)
	if err := store.SaveTask(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	result, err := orchestrator.Run(ctx, prepared.ID, s.ID, prepared.OriginalRequest, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocking provider error = %v, want context deadline exceeded", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("provider was not bounded by the runtime deadline: %s", time.Since(started))
	}
	if result.State != task.Failed || result.WorkerLease.Token != "" {
		t.Fatalf("returned task did not persist terminal state and release its lease: %+v", result)
	}
	persisted, err := store.GetTask(ctx, prepared.ID)
	if err != nil || persisted.State != task.Failed || !strings.Contains(persisted.FailureReason, context.DeadlineExceeded.Error()) {
		t.Fatalf("durable task is inconsistent with failure: %+v err=%v", persisted, err)
	}
	events, err := traceWriter.Read(prepared.ID)
	if err != nil || len(events) == 0 || events[len(events)-1].Type != trace.TaskFailed {
		t.Fatalf("terminal trace is inconsistent: events=%+v err=%v", events, err)
	}
	if err := traceWriter.VerifyIntegrity(prepared.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeRejectsUnavailableTool(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	now := time.Now().UTC()
	s := session.Session{ID: "ses_unavailable", Name: "test", CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{{Kind: llm.DecisionTool, DecisionSummary: "尝试任意命令", ServerID: "system", Tool: "shell.execute", Arguments: map[string]any{}}}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter}
	_, _ = orchestrator.Prepare(ctx, "task_unavailable", s.ID, "执行 uname")
	result, err := orchestrator.Run(ctx, "task_unavailable", s.ID, "执行 uname", nil)
	if err == nil || result.State != task.Failed {
		t.Fatalf("unavailable tool was not rejected: %+v %v", result, err)
	}
}

func TestGeneralRuntimePreparesManagedActionRequestForApproval(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	approvalStore, _ := approval.NewStore(store.Root() + "/approvals")
	catalog, err := guard.LoadCatalog(filepath.Join("..", "..", "policies", "tools.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	pipeline := guard.NewSafetyPipeline(catalog)
	now := time.Now().UTC()
	s := session.Session{ID: "ses_managed_action", Name: "managed action", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	tools := fakeManagedActionTools{service: "safeops-demo-web.service"}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取服务状态作为动作证据", ServerID: "service", Tool: "service.get_status", Arguments: map[string]any{}, ExpectedObservation: "服务状态"},
		{Kind: llm.DecisionActionRequest, DecisionSummary: "申请重启证据绑定的 allowlist 服务", Tool: "service.restart", Target: llm.ActionTarget{Type: "service", ID: "safeops-demo-web.service"}, Arguments: map[string]any{}, ExpectedObservation: "服务重启并验证 active"},
	}}
	orchestrator := &Orchestrator{
		Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: pipeline, Trace: traceWriter, ToolTimeout: time.Second,
		Actions:       &ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Scope: allowAllScope{}, Trace: traceWriter, Secret: []byte("0123456789abcdef0123456789abcdef")},
		ActionTargets: fakeManagedActionTargets{service: "safeops-demo-web.service"},
	}
	if _, err := orchestrator.Prepare(ctx, "task_managed_action", s.ID, "检查并在安全时重启 demo 服务"); err != nil {
		t.Fatal(err)
	}
	waiting, err := orchestrator.Run(ctx, "task_managed_action", s.ID, "检查并在安全时重启 demo 服务", nil)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.State != task.WaitingApproval || waiting.PendingApprovalID == "" || len(waiting.PendingAction) == 0 {
		t.Fatalf("managed action did not become approval-bound: %+v", waiting)
	}
	if waiting.Runtime.ToolCalls != 1 || len(waiting.EvidenceRefs) != 1 {
		t.Fatalf("managed action skipped required MCP evidence: %+v", waiting.Runtime)
	}
	var envelope contracts.ActionEnvelope
	if err := json.Unmarshal(waiting.PendingAction, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Proposal.Tool != "service.restart" || envelope.TargetSnapshot.ServiceName != "safeops-demo-web.service" {
		t.Fatalf("unexpected pending envelope: %+v", envelope)
	}
	records, err := approvalStore.List(ctx)
	if err != nil || len(records) != 1 || records[0].Status != approval.Pending {
		t.Fatalf("approval was not created exactly once: %+v err=%v", records, err)
	}
	if err := traceWriter.VerifyIntegrity(waiting.ID); err != nil {
		t.Fatal(err)
	}
}

func TestGeneralRuntimeManagedActionRequiresApprovalScopeBeforeCreatingApproval(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	approvalStore, _ := approval.NewStore(store.Root() + "/approvals")
	catalog, _ := guard.LoadCatalog(filepath.Join("..", "..", "policies", "tools.yaml"))
	pipeline := guard.NewSafetyPipeline(catalog)
	now := time.Now().UTC()
	s := session.Session{ID: "ses_scope_denied", Name: "scope denied", CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	tools := fakeManagedActionTools{service: "nginx.service"}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "读取非 allowlist 服务状态", ServerID: "service", Tool: "service.get_status", Arguments: map[string]any{}},
		{Kind: llm.DecisionActionRequest, DecisionSummary: "尝试申请重启非 allowlist 服务", Tool: "service.restart", Target: llm.ActionTarget{Type: "service", ID: "nginx.service"}, Arguments: map[string]any{}},
	}}
	orchestrator := &Orchestrator{
		Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: pipeline, Trace: traceWriter, ToolTimeout: time.Second,
		Actions:       &ActionPreparer{Store: store, Approvals: approvalStore, Safety: pipeline, Scope: denyScope{err: errors.New("service is not allowlisted")}, Trace: traceWriter, Secret: []byte("0123456789abcdef0123456789abcdef")},
		ActionTargets: fakeManagedActionTargets{service: "nginx.service"},
	}
	_, _ = orchestrator.Prepare(ctx, "task_scope_denied", s.ID, "检查并重启 nginx")
	failed, err := orchestrator.Run(ctx, "task_scope_denied", s.ID, "检查并重启 nginx", nil)
	if err == nil || !strings.Contains(err.Error(), "target scope denied before approval") {
		t.Fatalf("scope denial did not fail closed: state=%s err=%v", failed.State, err)
	}
	records, listErr := approvalStore.List(ctx)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(records) != 0 {
		t.Fatalf("approval was created for denied target: %+v", records)
	}
}

func TestGeneralRuntimeRejectsUnavailableManagedAction(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewFileStore(t.TempDir())
	traceWriter, _ := trace.NewWriter(store.Root() + "/traces")
	now := time.Now().UTC()
	s := session.Session{ID: "ses_shell_action", Name: "shell action", CreatedAt: now, UpdatedAt: now}
	_ = store.SaveSession(ctx, s)
	tools := fakeGeneralTools{}
	planner := &sequencePlanner{decisions: []llm.Decision{
		{Kind: llm.DecisionTool, DecisionSummary: "先读取证据", ServerID: "system", Tool: "system.get_load_average", Arguments: map[string]any{}},
		{Kind: llm.DecisionActionRequest, DecisionSummary: "尝试任意 shell", Tool: "shell.execute", Target: llm.ActionTarget{Type: "host", ID: "local"}, Arguments: map[string]any{}},
	}}
	orchestrator := &Orchestrator{Store: store, Registry: tools, Capabilities: tools, Planner: planner, Safety: fakeSafety{}, Trace: traceWriter, Actions: &ActionPreparer{}, ActionTargets: fakeManagedActionTargets{}}
	_, _ = orchestrator.Prepare(ctx, "task_shell_action", s.ID, "执行 id")
	result, err := orchestrator.Run(ctx, "task_shell_action", s.ID, "执行 id", nil)
	if err == nil || result.State != task.Failed || !strings.Contains(err.Error(), "unavailable managed action shell.execute") {
		t.Fatalf("unsafe managed action was not rejected: %+v err=%v", result, err)
	}
}

func TestManagedActionRequiresExplicitOperatorIntent(t *testing.T) {
	if managedActionIntentAllows("检查 demo 服务状态", "service.restart") {
		t.Fatal("read-only service request authorized a restart")
	}
	if !managedActionIntentAllows("检查后重启 demo 服务", "service.restart") {
		t.Fatal("explicit service restart request was rejected")
	}
	for _, request := range []string{"restart nginx", "please restart safeops-demo-web.service", "check the unit, then restart nginx"} {
		if !managedActionIntentAllows(request, "service.restart") {
			t.Fatalf("direct English service restart request was rejected: %q", request)
		}
	}
	for _, request := range []string{"check the service restart count", "查看服务重启历史", "重启服务的风险", "how to restart nginx", "restart nginx impact", "should I restart nginx?"} {
		if managedActionIntentAllows(request, "service.restart") {
			t.Fatalf("read-only service restart wording authorized an action: %q", request)
		}
	}
	for _, request := range []string{
		"不要重启 demo 服务，只检查状态",
		"别再重新启动 demo 服务",
		"请说明是否需要重启，但不要执行",
		"check only; do not restart service",
		"never restart the service",
		"without restarting the service",
		"don't, under any circumstances, restart nginx",
		"不要在任何情况下，重启 demo 服务",
	} {
		if managedActionIntentAllows(request, "service.restart") {
			t.Fatalf("negated service request authorized a restart: %q", request)
		}
	}
	if managedActionIntentAllows("查看进程详情", "process.terminate") {
		t.Fatal("read-only process request authorized termination")
	}
	if !managedActionIntentAllows("确认后终止进程", "process.terminate") {
		t.Fatal("explicit process termination request was rejected")
	}
	for _, request := range []string{"terminate process 123", "please stop the process", "check it, then kill worker process"} {
		if !managedActionIntentAllows(request, "process.terminate") {
			t.Fatalf("direct English process action was rejected: %q", request)
		}
	}
	for _, request := range []string{"explain how to kill process 123", "告诉我如何终止进程", "终止进程的风险", "stop process impact", "should I stop the process?"} {
		if managedActionIntentAllows(request, "process.terminate") {
			t.Fatalf("read-only process wording authorized an action: %q", request)
		}
	}
	for _, request := range []string{
		"不要终止进程，只查看",
		"禁止停止这个进程",
		"请给建议，不要执行",
		"do not kill process; only inspect",
		"don't stop process",
		"without terminating the process",
		"don't, under any circumstances, stop the process",
		"禁止在任何情况下，终止这个进程",
	} {
		if managedActionIntentAllows(request, "process.terminate") {
			t.Fatalf("negated process request authorized termination: %q", request)
		}
	}
}

func TestManagedActionEvidenceRequiresExactStructuredIdentity(t *testing.T) {
	serviceTarget := contracts.TargetRef{Type: "service", ID: "safeops-demo-web.service"}
	serviceSnapshot := contracts.TargetSnapshot{Type: "service", ServiceName: serviceTarget.ID}
	for name, observation := range map[string]task.RuntimeObservation{
		"unrelated tool text": {Tool: "journal.query_unit", Result: json.RawMessage(`{"events":[{"message":"safeops-demo-web.service"}]}`), EvidenceRef: "trace://task/1"},
		"recoverable failure": {Tool: "service.get_status", Result: json.RawMessage(`{"status":"error","error":"safeops-demo-web.service failed"}`), EvidenceRef: "trace://task/2"},
	} {
		t.Run(name, func(t *testing.T) {
			value := task.Task{EvidenceRefs: []string{observation.EvidenceRef}, Runtime: task.RuntimeCheckpoint{Observations: []task.RuntimeObservation{observation}}}
			if managedActionEvidenceSupports(value, serviceTarget, serviceSnapshot) {
				t.Fatal("unstructured or unrelated text authorized a service action")
			}
		})
	}
	serviceObservation := task.RuntimeObservation{Tool: "service.get_status", Result: json.RawMessage(`{"service":{"name":"safeops-demo-web.service","active_state":"failed"}}`), EvidenceRef: "trace://task/3"}
	serviceValue := task.Task{EvidenceRefs: []string{serviceObservation.EvidenceRef}, Runtime: task.RuntimeCheckpoint{Observations: []task.RuntimeObservation{serviceObservation}}}
	if !managedActionEvidenceSupports(serviceValue, serviceTarget, serviceSnapshot) {
		t.Fatal("exact structured service identity was rejected")
	}

	processTarget := contracts.TargetRef{Type: "process", ID: "pid:42:start:99"}
	processSnapshot := contracts.TargetSnapshot{Type: "process", PID: 42, StartTicks: 99}
	separateObjects := task.RuntimeObservation{Tool: "process.list_top", Result: json.RawMessage(`{"processes":[{"pid":42,"start_ticks":98},{"pid":41,"start_ticks":99}]}`), EvidenceRef: "trace://task/4"}
	processValue := task.Task{EvidenceRefs: []string{separateObjects.EvidenceRef}, Runtime: task.RuntimeCheckpoint{Observations: []task.RuntimeObservation{separateObjects}}}
	if managedActionEvidenceSupports(processValue, processTarget, processSnapshot) {
		t.Fatal("PID and start_ticks from different objects authorized process termination")
	}
	exactProcess := task.RuntimeObservation{Tool: "process.get_details", Result: json.RawMessage(`{"process":{"pid":42,"start_ticks":99}}`), EvidenceRef: "trace://task/5"}
	processValue = task.Task{EvidenceRefs: []string{exactProcess.EvidenceRef}, Runtime: task.RuntimeCheckpoint{Observations: []task.RuntimeObservation{exactProcess}}}
	if !managedActionEvidenceSupports(processValue, processTarget, processSnapshot) {
		t.Fatal("exact structured process identity was rejected")
	}
}

func TestDiscoveredSchemaValidationFailsClosed(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["limit"],"additionalProperties":false}`)
	if err := validateToolArguments(schema, map[string]any{"limit": float64(10)}); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []map[string]any{{}, {"limit": 100.0}, {"limit": "ten"}, {"limit": 1.0, "command": "sh -c id"}} {
		if err := validateToolArguments(schema, arguments); err == nil {
			t.Fatalf("invalid arguments accepted: %#v", arguments)
		}
	}
}

func TestCaptureSelectedLargeFilesPreservesOrder(t *testing.T) {
	value := task.Task{}
	captureSelectedResources(&value, "file.find_large", map[string]any{"files": []any{map[string]any{"path": "/lab/a"}, map[string]any{"path": "/lab/b"}, map[string]any{"path": "/lab/a"}}})
	if len(value.SelectedResources) != 2 || value.SelectedResources[1] != "/lab/b" {
		t.Fatalf("unexpected resources: %+v", value.SelectedResources)
	}
	captureSelectedResources(&value, "service.get_status", map[string]any{"files": []any{map[string]any{"path": "/etc/shadow"}}})
	if len(value.SelectedResources) != 2 {
		t.Fatal("non-file tool changed selected resources")
	}
}

type fakeGeneralTools struct{}

func (fakeGeneralTools) AvailableTools() []llm.ToolCapability {
	return []llm.ToolCapability{{ServerID: "system", Name: "system.get_load_average", Description: "load", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
}
func (fakeGeneralTools) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	if server != "system" || name != "system.get_load_average" {
		return nil, errors.New("unavailable")
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"load": map[string]any{"one": 0.5}}}, nil
}

type fakeRecoverableToolFailureTools struct{}

func (fakeRecoverableToolFailureTools) AvailableTools() []llm.ToolCapability {
	return []llm.ToolCapability{
		{ServerID: "file", Name: "file.list_directory", Description: "list files", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
		{ServerID: "system", Name: "system.get_load_average", Description: "load", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
}

func (fakeRecoverableToolFailureTools) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	if server == "file" && name == "file.list_directory" {
		return nil, errors.New("resolve allowlisted root /var/lib/safeops/lab: lstat /var/lib/safeops: no such file or directory")
	}
	if server == "system" && name == "system.get_load_average" {
		return &mcp.CallToolResult{StructuredContent: map[string]any{"load": map[string]any{"one": 0.5}}}, nil
	}
	return nil, errors.New("unavailable")
}

type sequencePlanner struct {
	decisions      []llm.Decision
	index          int
	sawObservation bool
	requests       []llm.DecisionRequest
}

type blockingPlanner struct{}

func (blockingPlanner) Decide(ctx context.Context, _ llm.DecisionRequest) (llm.Decision, error) {
	<-ctx.Done()
	return llm.Decision{}, ctx.Err()
}

func (p *sequencePlanner) Decide(_ context.Context, request llm.DecisionRequest) (llm.Decision, error) {
	p.requests = append(p.requests, request)
	if p.index > 0 && len(request.Observations) > 0 {
		p.sawObservation = true
	}
	if p.index >= len(p.decisions) {
		return llm.Decision{}, errors.New("no more decisions")
	}
	decision := p.decisions[p.index]
	p.index++
	return decision, nil
}

type fakeManagedActionTools struct {
	service string
}

func (f fakeManagedActionTools) AvailableTools() []llm.ToolCapability {
	return []llm.ToolCapability{{ServerID: "service", Name: "service.get_status", Description: "status", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
}

func (f fakeManagedActionTools) CallTool(_ context.Context, server, name string, _ any) (*mcp.CallToolResult, error) {
	if server != "service" || name != "service.get_status" {
		return nil, errors.New("unavailable")
	}
	return &mcp.CallToolResult{StructuredContent: map[string]any{"name": f.service, "active_state": "failed"}}, nil
}

type fakeManagedActionTargets struct {
	service string
}

func (f fakeManagedActionTargets) SnapshotProcess(_ context.Context, targetID string, pid int) (contracts.TargetSnapshot, error) {
	return contracts.TargetSnapshot{Type: "process", ID: targetID, PID: pid, StartTicks: 9911, Executable: "/opt/safeops/bin/safeops-cpu-hog", UID: 1000}, nil
}

func (f fakeManagedActionTargets) SnapshotService(_ context.Context, targetID, _ string) (contracts.TargetSnapshot, error) {
	service := f.service
	if service == "" {
		service = targetID
	}
	return contracts.TargetSnapshot{Type: "service", ID: targetID, ServiceName: service, ActiveState: "failed"}, nil
}

type allowAllScope struct{}

func (allowAllScope) Authorize(contracts.ActionEnvelope) error { return nil }

type denyScope struct {
	err error
}

func (s denyScope) Authorize(contracts.ActionEnvelope) error {
	if s.err != nil {
		return s.err
	}
	return fmt.Errorf("denied")
}
