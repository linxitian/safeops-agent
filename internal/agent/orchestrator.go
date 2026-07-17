package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/contracts"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/id"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

const (
	MaxIterations = 12
	MaxToolCalls  = 30
)

type RuntimeEvent struct {
	Sequence  uint64     `json:"sequence"`
	Type      string     `json:"type"`
	TaskID    string     `json:"task_id"`
	State     task.State `json:"state"`
	Message   string     `json:"message"`
	Timestamp time.Time  `json:"timestamp"`
}

type EventSink func(RuntimeEvent)

type ToolCaller interface {
	CallTool(context.Context, string, string, any) (*mcp.CallToolResult, error)
}
type SafetyEvaluator interface {
	Evaluate(contracts.ActionProposal) guard.PipelineResult
}
type CapabilitySource interface {
	AvailableTools() []llm.ToolCapability
}
type MutationTargetSnapshotter interface {
	SnapshotProcess(context.Context, string, int) (contracts.TargetSnapshot, error)
	SnapshotService(context.Context, string, string) (contracts.TargetSnapshot, error)
}

type Orchestrator struct {
	Store         storage.Store
	Registry      ToolCaller
	Capabilities  CapabilitySource
	Planner       llm.Provider
	Actions       *ActionPreparer
	FileTargets   FileTargetSnapshotter
	ActionTargets MutationTargetSnapshotter
	Health        HealthVerifier
	Safety        SafetyEvaluator
	Trace         *trace.Writer
	ToolTimeout   time.Duration
	WorkerID      string
	LeaseTTL      time.Duration
}

type metricIntent struct {
	CPU    bool
	Memory bool
}

// Prepare durably creates the user message, Task and first audit facts before
// an API returns 202 or an SSE event is published.
func (o *Orchestrator) Prepare(ctx context.Context, taskID, sessionID, request string) (task.Task, error) {
	now := time.Now().UTC()
	t := task.Task{ID: taskID, SessionID: sessionID, Objective: request, OriginalRequest: request, State: task.New, CreatedAt: now, UpdatedAt: now}
	if _, err := o.Store.UpdateSession(ctx, sessionID, func(s *session.Session) error {
		if len(s.Messages) == 0 && isDefaultSessionName(s.Name) {
			s.Name = sessionTitleFromRequest(request)
		}
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleUser, Content: request, TaskID: taskID, CreatedAt: now})
		s.UpdatedAt = now
		return nil
	}); err != nil {
		return t, err
	}
	if err := o.Store.SaveTask(ctx, t); err != nil {
		return t, err
	}
	if err := o.appendTrace(ctx, t, trace.Received, map[string]any{"request": request}); err != nil {
		return t, err
	}
	if err := o.appendTrace(ctx, t, trace.TaskCreated, map[string]any{"state": t.State}); err != nil {
		return t, err
	}
	return t, nil
}

func (o *Orchestrator) Run(ctx context.Context, taskID, sessionID, request string, emit EventSink) (resultTask task.Task, err error) {
	if emit == nil {
		emit = func(RuntimeEvent) {}
	}
	t, err := o.Store.GetTask(ctx, taskID)
	if errors.Is(err, storage.ErrNotFound) {
		t, err = o.Prepare(ctx, taskID, sessionID, request)
	}
	if err != nil {
		return task.Task{}, err
	}
	if t.SessionID != sessionID || t.OriginalRequest != request {
		return t, errors.New("prepared task does not match request")
	}
	if terminalTask(t.State) || t.State == task.WaitingApproval {
		return t, nil
	}
	leaseToken := id.New("lease")
	workerID := strings.TrimSpace(o.WorkerID)
	if workerID == "" {
		workerID = "safeops-worker"
	}
	t, err = o.Store.ClaimTask(ctx, taskID, workerID, leaseToken, o.leaseTTL())
	if err != nil {
		return t, err
	}
	leaseFence := t.WorkerLease.Fence
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		released, releaseErr := o.Store.ReleaseTask(releaseCtx, taskID, leaseToken, leaseFence)
		if releaseErr != nil {
			if err == nil {
				err = fmt.Errorf("release task worker lease: %w", releaseErr)
			}
			return
		}
		resultTask = released
	}()
	emitEvent(emit, t, "正在理解请求")
	defer func() {
		if err == nil {
			return
		}
		t.FailureReason = err.Error()
		t.Transition(task.Failed)
		_ = o.Store.SaveTask(context.Background(), t)
		_ = o.appendTrace(context.Background(), t, trace.TaskFailed, map[string]any{"error": err.Error()})
		emitEvent(emit, t, "任务失败："+err.Error())
		resultTask = t
	}()
	if (t.State == task.Executing || t.State == task.Verifying) && len(t.PendingAction) > 0 {
		err = errors.New("in-flight privileged action requires manual reconciliation after worker loss; automatic re-execution is forbidden")
		return t, err
	}
	if detectPortRecovery(request) {
		t, err = o.runPortRecovery(ctx, t, emit)
		return t, err
	}
	if detectCPURecovery(request) {
		t, err = o.runCPURecovery(ctx, t, emit)
		return t, err
	}
	if detectDiskRecovery(request) {
		t, err = o.runDiskRecovery(ctx, t, emit)
		return t, err
	}
	if kind := detectFileAction(request); kind != "" {
		t, err = o.runFileAction(ctx, t, kind, request, emit)
		return t, err
	}

	intent, intentErr := parseMetricIntent(request)
	if intentErr != nil {
		if o.Planner == nil {
			return t, intentErr
		}
		t, err = o.runGeneral(ctx, t, emit)
		return t, err
	}
	t.IntentType = "system_metrics_read"
	t.Transition(task.Planning)
	if err := o.appendTrace(ctx, t, trace.IntentParsed, map[string]any{"intent_type": t.IntentType, "cpu": intent.CPU, "memory": intent.Memory}); err != nil {
		return t, err
	}
	if intent.CPU {
		t.Plan = append(t.Plan, task.Step{ID: "step_cpu", Description: "通过 MCP 读取真实 CPU 指标", Tool: "system.get_cpu_metrics", State: "PENDING"})
	}
	if intent.Memory {
		t.Plan = append(t.Plan, task.Step{ID: "step_memory", Description: "通过 MCP 读取真实内存指标", Tool: "system.get_memory_metrics", State: "PENDING"})
	}
	t.Transition(task.Investigating)
	if err := o.Store.SaveTask(ctx, t); err != nil {
		return t, err
	}
	if err := o.appendTrace(ctx, t, trace.PlanCreated, map[string]any{"steps": t.Plan, "completion_criteria": "所有请求的指标均获得新的 MCP 证据"}); err != nil {
		return t, err
	}
	emitEvent(emit, t, "正在采集系统信息")

	outputs := map[string]map[string]any{}
	calls := 0
	for index := range t.Plan {
		if index >= MaxIterations || calls >= MaxToolCalls {
			return t, errors.New("agent execution limit reached")
		}
		step := &t.Plan[index]
		step.State = "RUNNING"
		t.CurrentStep = index
		t.Transition(task.Investigating)
		if err := o.Store.SaveTask(ctx, t); err != nil {
			return t, err
		}
		if err := o.appendTrace(ctx, t, trace.DecisionRecorded, map[string]any{"objective": t.Objective, "current_step": step.Description, "decision_summary": "选择只读系统感知工具收集完成条件所需证据", "selected_tool": step.Tool, "expected_observation": "结构化 Linux 实时指标"}); err != nil {
			return t, err
		}
		if o.Safety == nil {
			return t, errors.New("safety pipeline is not configured")
		}
		target := contracts.TargetRef{Type: "host", ID: "local"}
		proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: t.ID, SessionID: t.SessionID, Tool: step.Tool, Effect: contracts.Read, Arguments: map[string]any{}, Target: target, BatchSize: 1, Intent: contracts.IntentContext{OriginalRequest: t.OriginalRequest, Objective: t.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: step.Description, PlanTargets: []contracts.TargetRef{target}}}
		digest, digestErr := proposal.Digest()
		if digestErr != nil {
			return t, digestErr
		}
		if err := o.appendTrace(ctx, t, trace.ActionProposed, map[string]any{"proposal_id": proposal.ProposalID, "tool": proposal.Tool, "effect": proposal.Effect, "target": proposal.Target, "proposal_digest": digest}); err != nil {
			return t, err
		}
		safety := o.Safety.Evaluate(proposal)
		if err := o.appendTrace(ctx, t, trace.StaticGuardResult, safety.Static); err != nil {
			return t, err
		}
		if safety.Static.Outcome == contracts.Deny {
			return t, fmt.Errorf("static guard denied %s: %s", step.Tool, safety.Static.Reason)
		}
		if err := o.appendTrace(ctx, t, trace.IntentGuardResult, safety.Intent); err != nil {
			return t, err
		}
		if safety.Intent.Outcome == contracts.Deny {
			return t, fmt.Errorf("intent guard denied %s: %s", step.Tool, safety.Intent.Reason)
		}
		if err := o.appendTrace(ctx, t, trace.RiskEvaluated, safety.Risk); err != nil {
			return t, err
		}
		if safety.Final.Outcome != contracts.Allow {
			return t, fmt.Errorf("safety pipeline did not allow read tool %s: %s", step.Tool, safety.Final.Reason)
		}
		toolCtx, cancel := context.WithTimeout(ctx, o.timeout())
		if err := o.appendTrace(toolCtx, t, trace.ToolCall, map[string]any{"server": "system", "tool": step.Tool, "arguments": map[string]any{}}); err != nil {
			cancel()
			return t, err
		}
		calls++
		result, callErr := o.Registry.CallTool(toolCtx, "system", step.Tool, map[string]any{})
		cancel()
		if callErr != nil {
			return t, callErr
		}
		if result.IsError {
			return t, fmt.Errorf("tool %s: %s", step.Tool, textResult(result))
		}
		structured, ok := result.StructuredContent.(map[string]any)
		if !ok {
			return t, fmt.Errorf("tool %s returned no structured object", step.Tool)
		}
		outputs[step.Tool] = structured
		event, traceErr := o.Trace.Append(ctx, t.ID, t.SessionID, trace.ToolResult, map[string]any{"tool": step.Tool, "structured_output": structured})
		if traceErr != nil {
			return t, traceErr
		}
		step.State = "COMPLETED"
		t.CompletedSteps = append(t.CompletedSteps, step.ID)
		finding := summarizeFinding(step.Tool, structured)
		t.Findings = append(t.Findings, finding)
		t.EvidenceRefs = append(t.EvidenceRefs, fmt.Sprintf("trace://%s/%d", t.ID, event.Sequence))
		t.Transition(task.Investigating)
		if err := o.Store.SaveTask(ctx, t); err != nil {
			return t, err
		}
		if err := o.appendTrace(ctx, t, trace.FindingsUpdated, map[string]any{"finding": finding, "evidence_ref": t.EvidenceRefs[len(t.EvidenceRefs)-1]}); err != nil {
			return t, err
		}
		emitEvent(emit, t, "已获得证据："+finding)
	}

	if len(t.CompletedSteps) != len(t.Plan) {
		return t, errors.New("completion criteria not met")
	}
	answer := formatAnswer(outputs)
	t.Transition(task.Completed)
	if err := o.Store.SaveTask(ctx, t); err != nil {
		return t, err
	}
	if _, err := o.Store.UpdateSession(ctx, sessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: t.ID, CreatedAt: time.Now().UTC()})
		s.Summary = "已完成系统 CPU/内存只读感知"
		s.UpdatedAt = time.Now().UTC()
		return nil
	}); err != nil {
		return t, err
	}
	if err := o.appendTrace(ctx, t, trace.TaskCompleted, map[string]any{"completion_criteria_met": true}); err != nil {
		return t, err
	}
	if err := o.appendTrace(ctx, t, trace.Final, map[string]any{"answer": answer}); err != nil {
		return t, err
	}
	emitEvent(emit, t, "任务完成")
	return t, nil
}

// RecoverIncomplete resumes only work that is safe to repeat. A task that may
// have crossed the privileged execution boundary is claimed and failed closed
// for manual reconciliation instead of executing its pending action twice.
func (o *Orchestrator) RecoverIncomplete(ctx context.Context, emit EventSink) []error {
	values, err := o.Store.ListTasks(ctx)
	if err != nil {
		return []error{err}
	}
	var failures []error
	for _, value := range values {
		if terminalTask(value.State) || value.State == task.WaitingApproval {
			continue
		}
		if value.WorkerLease.Token != "" {
			wait := time.Until(value.WorkerLease.ExpiresAt)
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return append(failures, ctx.Err())
				case <-timer.C:
				}
			}
		}
		taskCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		_, runErr := o.Run(taskCtx, value.ID, value.SessionID, value.OriginalRequest, emit)
		cancel()
		if runErr != nil {
			failures = append(failures, fmt.Errorf("recover task %s: %w", value.ID, runErr))
		}
	}
	return failures
}

func terminalTask(state task.State) bool {
	return state == task.Completed || state == task.Failed || state == task.Cancelled
}

func (o *Orchestrator) leaseTTL() time.Duration {
	if o.LeaseTTL <= 0 {
		return 3 * time.Minute
	}
	return o.LeaseTTL
}

func (o *Orchestrator) timeout() time.Duration {
	if o.ToolTimeout <= 0 {
		return 10 * time.Second
	}
	return o.ToolTimeout
}
func (o *Orchestrator) appendTrace(ctx context.Context, t task.Task, typ trace.Type, data any) error {
	_, err := o.Trace.Append(ctx, t.ID, t.SessionID, typ, data)
	return err
}
func emitEvent(emit EventSink, t task.Task, message string) {
	emit(RuntimeEvent{Type: "task.progress", TaskID: t.ID, State: t.State, Message: message, Timestamp: time.Now().UTC()})
}

func parseMetricIntent(request string) (metricIntent, error) {
	normalized := strings.ToLower(strings.TrimSpace(request))
	intent := metricIntent{CPU: strings.Contains(normalized, "cpu") || strings.Contains(normalized, "处理器"), Memory: strings.Contains(normalized, "内存") || strings.Contains(normalized, "memory")}
	if !intent.CPU && !intent.Memory {
		return metricIntent{}, errors.New("当前已实现的纵切片仅支持 CPU/内存只读查询")
	}
	return intent, nil
}

func isDefaultSessionName(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "新会话", "新对话", "系统感知会话":
		return true
	default:
		return false
	}
}

func sessionTitleFromRequest(request string) string {
	title := strings.Join(strings.Fields(strings.TrimSpace(request)), " ")
	runes := []rune(title)
	if len(runes) > 36 {
		title = string(runes[:36])
	}
	if title == "" {
		return "新会话"
	}
	return title
}

// ClassifyMetricIntent exposes the production deterministic metric classifier
// for evaluation without duplicating its matching rules in benchmark code.
func ClassifyMetricIntent(request string) ([]string, error) {
	intent, err := parseMetricIntent(request)
	if err != nil {
		return nil, err
	}
	labels := make([]string, 0, 2)
	if intent.CPU {
		labels = append(labels, "cpu")
	}
	if intent.Memory {
		labels = append(labels, "memory")
	}
	return labels, nil
}
func textResult(result *mcp.CallToolResult) string {
	var values []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			values = append(values, text.Text)
		}
	}
	return strings.Join(values, "; ")
}
func summarizeFinding(tool string, out map[string]any) string {
	switch tool {
	case "system.get_cpu_metrics":
		return fmt.Sprintf("CPU 使用率 %.1f%%", number(out["usage_percent"]))
	case "system.get_memory_metrics":
		memory, _ := out["memory"].(map[string]any)
		return fmt.Sprintf("内存使用率 %.1f%%（已用 %s / 总计 %s）", number(out["usage_percent"]), bytesText(number(memory["used_bytes"])), bytesText(number(memory["total_bytes"])))
	}
	return tool + " 返回结构化证据"
}
func formatAnswer(outputs map[string]map[string]any) string {
	parts := []string{"已通过真实 MCP 工具读取当前 Linux 状态："}
	if out := outputs["system.get_cpu_metrics"]; out != nil {
		parts = append(parts, fmt.Sprintf("- CPU：采样窗口内使用率约 %.1f%%。", number(out["usage_percent"])))
	}
	if out := outputs["system.get_memory_metrics"]; out != nil {
		memory, _ := out["memory"].(map[string]any)
		parts = append(parts, fmt.Sprintf("- 内存：使用率约 %.1f%%，已用 %s，总计 %s，可用 %s。", number(out["usage_percent"]), bytesText(number(memory["used_bytes"])), bytesText(number(memory["total_bytes"])), bytesText(number(memory["available_bytes"]))))
	}
	return strings.Join(parts, "\n")
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case uint64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func bytesText(v float64) string {
	const gib = 1024 * 1024 * 1024
	const mib = 1024 * 1024
	if v >= gib {
		return fmt.Sprintf("%.2f GiB", v/gib)
	}
	return fmt.Sprintf("%.1f MiB", v/mib)
}
