package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/id"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

const (
	demoCPUUnit       = "safeops-cpu-hog.service"
	demoCPUExecutable = "/opt/safeops/bin/safeops-cpu-hog"
)

func detectCPURecovery(request string) bool {
	normalized := strings.ToLower(strings.TrimSpace(request))
	mentionsCPU := strings.Contains(normalized, "cpu") || strings.Contains(normalized, "处理器")
	mentionsPressure := strings.Contains(normalized, "高") || strings.Contains(normalized, "占用") || strings.Contains(normalized, "忙")
	// “处理器” names the CPU; its prefix must not be interpreted as the action
	// verb “处理”. Recovery routing requires an action outside that noun.
	actionText := strings.ReplaceAll(normalized, "处理器", "")
	requestsAction := strings.Contains(actionText, "处理") || strings.Contains(actionText, "恢复") || strings.Contains(actionText, "结束") || strings.Contains(actionText, "修复")
	return mentionsCPU && mentionsPressure && requestsAction
}

func (o *Orchestrator) runCPURecovery(ctx context.Context, value task.Task, emit EventSink) (task.Task, error) {
	if o.Actions == nil || o.ActionTargets == nil || o.Registry == nil || o.Safety == nil || o.Trace == nil {
		return value, errors.New("CPU recovery requires MCP, safety, approval, trace, and target snapshot dependencies")
	}
	value.IntentType = "cpu_hog_recovery"
	value.Plan = []task.Step{
		{ID: "cpu_baseline", Description: "采样处置前 CPU 使用率", Tool: "system.get_cpu_metrics", State: "PENDING"},
		{ID: "cpu_process", Description: "定位受控 CPU 压力进程", Tool: "process.search", State: "PENDING"},
		{ID: "cpu_journal", Description: "读取受控 CPU unit 日志", Tool: "journal.query_unit", State: "PENDING"},
		{ID: "cpu_rca", Description: "关联负载与进程资源生成 RCA", Tool: "diagnostic.high_cpu", State: "PENDING"},
		{ID: "cpu_terminate", Description: "终止证据绑定的受控 CPU 进程", Tool: "process.terminate", State: "PENDING"},
		{ID: "cpu_verify_metric", Description: "重新采样 CPU 使用率", Tool: "system.get_cpu_metrics", State: "PENDING"},
		{ID: "cpu_verify_process", Description: "验证受控 CPU 进程已消失", Tool: "process.search", State: "PENDING"},
	}
	value.WorkflowData = map[string]json.RawMessage{}
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "unit": demoCPUUnit, "executable": demoCPUExecutable, "scope": "fixed controlled Lab"}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"steps": value.Plan, "completion_criteria": "exact CPU hog process absent and fresh aggregate CPU sample lower than the persisted baseline"}); err != nil {
		return value, err
	}
	emitEvent(emit, value, "正在采集 CPU 压力的指标、进程和日志证据")
	reads := []struct {
		server, tool string
		arguments    map[string]any
	}{
		{server: "system", tool: "system.get_cpu_metrics", arguments: map[string]any{}},
		{server: "process", tool: "process.search", arguments: map[string]any{"query": "safeops-cpu-hog", "limit": 20}},
		{server: "journal", tool: "journal.query_unit", arguments: map[string]any{"unit": demoCPUUnit, "lines": 100}},
		{server: "diagnostic", tool: "diagnostic.high_cpu", arguments: map[string]any{"limit": 20}},
	}
	outputs := make([]any, len(reads))
	var err error
	for index, read := range reads {
		value, outputs[index], err = o.callWorkflowReadStep(ctx, value, index, read.server, read.tool, read.arguments)
		if err != nil {
			return value, err
		}
		emitEvent(emit, value, "已获得证据："+read.tool)
	}
	var baseline struct {
		UsagePercent float64 `json:"usage_percent"`
	}
	var processes struct {
		Processes []platform.ProcessInfo `json:"processes"`
	}
	var diagnosis struct {
		RCA rca.Result `json:"rca"`
	}
	if err := decodeStructured(outputs[0], &baseline); err != nil {
		return value, err
	}
	if err := decodeStructured(outputs[1], &processes); err != nil {
		return value, err
	}
	if err := decodeStructured(outputs[3], &diagnosis); err != nil {
		return value, err
	}
	if baseline.UsagePercent <= 0 {
		return value, errors.New("CPU baseline is unavailable; recovery action is not justified")
	}
	if err := o.appendTrace(ctx, value, trace.RCAResult, diagnosis.RCA); err != nil {
		return value, err
	}
	owner, err := uniqueControlledProcess(processes.Processes, demoCPUExecutable)
	if err != nil {
		return value, err
	}
	baselineJSON, _ := json.Marshal(baseline.UsagePercent)
	value.WorkflowData["cpu_baseline_percent"] = baselineJSON
	targetID := fmt.Sprintf("pid:%d:start:%d", owner.PID, owner.StartTicks)
	snapshot, err := o.ActionTargets.SnapshotProcess(ctx, targetID, owner.PID)
	if err != nil {
		return value, err
	}
	if snapshot.StartTicks != owner.StartTicks || snapshot.Executable != demoCPUExecutable {
		return value, errors.New("TARGET_CHANGED: CPU hog snapshot no longer matches investigation evidence")
	}
	processTarget := contracts.TargetRef{Type: "process", ID: snapshot.ID}
	hostTarget := contracts.TargetRef{Type: "host", ID: "local"}
	value.CurrentStep = 4
	value.Plan[4].State = "WAITING_APPROVAL"
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "selected_hypothesis": diagnosis.RCA.CandidateCauses, "evidence_used": value.EvidenceRefs, "selected_tool": "process.terminate", "target": processTarget, "expected_observation": "exact CPU hog PID/start/executable disappears and aggregate CPU is resampled"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: "process.terminate", Effect: contracts.Write, Arguments: map[string]any{"pid": owner.PID}, Target: processTarget, BatchSize: 1, Reversible: false, LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{hostTarget}, PlanStep: value.Plan[4].Description, PlanTargets: []contracts.TargetRef{processTarget}, AuthorizedRelations: []contracts.TargetAuthorization{{Target: processTarget, RelatedTo: hostTarget, Relation: "PROCESS_CONSUMES_CPU", EvidenceRefs: append([]string(nil), value.EvidenceRefs...)}}}}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		return waiting, err
	}
	emitEvent(emit, waiting, "受控 CPU 进程已通过双重护栏，等待 L2 人工审批")
	return waiting, nil
}

func (o *Orchestrator) verifyCPURecovery(ctx context.Context, value task.Task) (task.Task, error) {
	value.Transition(task.Verifying)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	verified, metricOutput, err := o.callWorkflowReadStep(ctx, value, 5, "system", "system.get_cpu_metrics", map[string]any{})
	if err != nil {
		return verified, err
	}
	verified, processOutput, err := o.callWorkflowReadStep(ctx, verified, 6, "process", "process.search", map[string]any{"query": "safeops-cpu-hog", "limit": 20})
	if err != nil {
		return verified, err
	}
	var after struct {
		UsagePercent float64 `json:"usage_percent"`
	}
	var processes struct {
		Processes []platform.ProcessInfo `json:"processes"`
	}
	if err := decodeStructured(metricOutput, &after); err != nil {
		return verified, err
	}
	if err := decodeStructured(processOutput, &processes); err != nil {
		return verified, err
	}
	baselineRaw := verified.WorkflowData["cpu_baseline_percent"]
	var baseline float64
	if len(baselineRaw) == 0 || json.Unmarshal(baselineRaw, &baseline) != nil {
		return verified, errors.New("durable CPU baseline is missing")
	}
	for _, process := range processes.Processes {
		if process.Executable == demoCPUExecutable {
			return verified, errors.New("CPU hog process is still present after executor verification")
		}
	}
	metricRecovered := after.UsagePercent+0.5 < baseline
	verificationEvent, err := o.Trace.Append(ctx, verified.ID, verified.SessionID, trace.VerificationResult, map[string]any{"verified": metricRecovered, "process_absent": true, "baseline_usage_percent": baseline, "after_usage_percent": after.UsagePercent, "minimum_drop_percent": .5})
	if err != nil {
		return verified, err
	}
	verified.EvidenceRefs = append(verified.EvidenceRefs, fmt.Sprintf("trace://%s/%d", verified.ID, verificationEvent.Sequence))
	if !metricRecovered {
		return verified, fmt.Errorf("CPU recovery completion gate failed: baseline=%.1f%% after=%.1f%%", baseline, after.UsagePercent)
	}
	verified.Findings = append(verified.Findings, fmt.Sprintf("受控 CPU 进程已消失，aggregate CPU 从 %.1f%% 降至 %.1f%%", baseline, after.UsagePercent))
	answer := fmt.Sprintf("已通过指标、进程、日志和 RCA 证据确认受控 CPU 压力进程。该精确 PID 经 L2 人工审批、目标重验证和固定 SIGTERM 后已消失；新的 CPU 采样从 %.1f%% 降至 %.1f%%，恢复条件已满足。", baseline, after.UsagePercent)
	verified.Transition(task.Completed)
	if err := o.Store.SaveTask(ctx, verified); err != nil {
		return verified, err
	}
	if _, err := o.Store.UpdateSession(ctx, verified.SessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: verified.ID, CreatedAt: time.Now().UTC()})
		s.Summary = "受控 CPU 压力已完成多源诊断、审批处置和指标恢复验证"
		s.UpdatedAt = time.Now().UTC()
		return nil
	}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.TaskCompleted, map[string]any{"completion_criteria_met": true, "process_absent": true, "metric_recovered": true}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.Final, map[string]any{"answer": answer, "evidence_refs": verified.EvidenceRefs}); err != nil {
		return verified, err
	}
	return verified, nil
}

func uniqueControlledProcess(values []platform.ProcessInfo, executable string) (platform.ProcessInfo, error) {
	matches := make([]platform.ProcessInfo, 0, 1)
	for _, value := range values {
		if value.Executable == executable {
			matches = append(matches, value)
		}
	}
	if len(matches) != 1 {
		return platform.ProcessInfo{}, fmt.Errorf("expected exactly one controlled process %s, got %d", executable, len(matches))
	}
	return matches[0], nil
}
