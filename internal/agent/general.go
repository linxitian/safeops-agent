package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/id"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

const maxObservationBytes = 128 << 10

func (o *Orchestrator) runGeneral(ctx context.Context, value task.Task, emit EventSink) (task.Task, error) {
	if o.Capabilities == nil {
		return value, errors.New("general runtime capability source is not configured")
	}
	capabilities := o.Capabilities.AvailableTools()
	if len(capabilities) == 0 {
		return value, errors.New("no healthy MCP tools are available")
	}
	available := map[string]llm.ToolCapability{}
	for _, capability := range capabilities {
		available[capability.ServerID+"\x00"+capability.Name] = capability
	}

	fresh := value.IntentType == ""
	if fresh {
		value.IntentType = "general_read_operation"
		value.Transition(task.Planning)
		initializeRuntime(&value.Runtime, time.Now().UTC())
		if err := o.Store.SaveTask(ctx, value); err != nil {
			return value, err
		}
		if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "provider": "openai-compatible", "effect_scope": "discovered read MCP tools only"}); err != nil {
			return value, err
		}
		if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"strategy": "bounded ReAct / Plan-Execute", "max_iterations": value.Runtime.MaxIterations, "max_tool_calls": value.Runtime.MaxToolCalls, "deadline_at": value.Runtime.DeadlineAt, "completion_criteria": "at least one MCP evidence reference and a structured final decision"}); err != nil {
			return value, err
		}
	}
	emitEvent(emit, value, "正在进行受限多步骤调查")

	for {
		if err := beginDecision(&value.Runtime, time.Now().UTC()); err != nil {
			return value, err
		}
		value.Transition(task.Investigating)
		if err := o.Store.SaveTask(ctx, value); err != nil {
			return value, err
		}
		decision, err := o.Planner.Decide(ctx, llm.DecisionRequest{
			Objective:       value.Objective,
			OriginalRequest: value.OriginalRequest,
			Tools:           capabilities,
			Observations:    plannerObservations(value.Runtime.Observations),
			Iteration:       value.Runtime.Iterations,
			ToolCalls:       value.Runtime.ToolCalls,
		})
		if err != nil {
			return value, err
		}
		originalDecision := decision
		bootstrappedEvidence := false
		decision, bootstrappedEvidence, err = requireEvidenceBeforeFinal(decision, value, capabilities)
		if err != nil {
			return value, err
		}
		decisionRecord := map[string]any{"objective": value.Objective, "decision_kind": decision.Kind, "decision_summary": decision.DecisionSummary, "selected_server": decision.ServerID, "selected_tool": decision.Tool, "expected_observation": decision.ExpectedObservation}
		if bootstrappedEvidence {
			decisionRecord["local_guardrail"] = "converted_final_without_evidence_to_tool"
			decisionRecord["planner_original_kind"] = originalDecision.Kind
			decisionRecord["planner_original_summary"] = originalDecision.DecisionSummary
		}
		if err := o.appendTrace(ctx, value, trace.DecisionRecorded, decisionRecord); err != nil {
			return value, err
		}
		if bootstrappedEvidence {
			emitEvent(emit, value, "模型过早给出结论，正在补充 MCP 证据")
		}

		switch decision.Kind {
		case llm.DecisionReplan:
			if err := recordReplan(&value.Runtime); err != nil {
				return value, err
			}
			value.Transition(task.Replanning)
			if err := o.Store.SaveTask(ctx, value); err != nil {
				return value, err
			}
			if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"strategy": "replan", "summary": decision.DecisionSummary, "replan_count": value.Runtime.Replans}); err != nil {
				return value, err
			}
			continue
		case llm.DecisionFinal:
			if len(value.Runtime.Observations) == 0 || len(value.EvidenceRefs) == 0 {
				return value, errors.New("completion gate rejected a final answer without MCP evidence")
			}
			return o.completeGeneral(ctx, value, decision.FinalAnswer, emit)
		case llm.DecisionTool:
			capability, ok := available[decision.ServerID+"\x00"+decision.Tool]
			if !ok {
				return value, fmt.Errorf("planner selected unavailable tool %s/%s", decision.ServerID, decision.Tool)
			}
			if decision.Arguments == nil {
				decision.Arguments = map[string]any{}
			}
			if err := validateToolArguments(capability.InputSchema, decision.Arguments); err != nil {
				return value, fmt.Errorf("tool arguments failed discovered schema: %w", err)
			}
			if err := reserveToolCall(&value.Runtime); err != nil {
				return value, err
			}
			arguments, err := json.Marshal(decision.Arguments)
			if err != nil {
				return value, err
			}
			stepID := fmt.Sprintf("step_%02d", len(value.Plan)+1)
			value.Plan = append(value.Plan, task.Step{ID: stepID, Description: decision.DecisionSummary, Tool: decision.Tool, State: "RUNNING"})
			value.CurrentStep = len(value.Plan) - 1
			if err := o.Store.SaveTask(ctx, value); err != nil {
				return value, err
			}
			target := contracts.TargetRef{Type: "host", ID: "local"}
			proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: decision.Tool, Effect: contracts.Read, Arguments: decision.Arguments, Target: target, BatchSize: 1, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: decision.DecisionSummary, PlanTargets: []contracts.TargetRef{target}}}
			digest, err := proposal.Digest()
			if err != nil {
				return value, err
			}
			if err := o.appendTrace(ctx, value, trace.ActionProposed, map[string]any{"proposal_id": proposal.ProposalID, "tool": proposal.Tool, "effect": proposal.Effect, "target": proposal.Target, "proposal_digest": digest}); err != nil {
				return value, err
			}
			if o.Safety == nil {
				return value, errors.New("safety pipeline is not configured")
			}
			safety := o.Safety.Evaluate(proposal)
			if err := o.appendTrace(ctx, value, trace.StaticGuardResult, safety.Static); err != nil {
				return value, err
			}
			if safety.Static.Outcome == contracts.Deny {
				return value, fmt.Errorf("static guard denied %s: %s", decision.Tool, safety.Static.Reason)
			}
			if err := o.appendTrace(ctx, value, trace.IntentGuardResult, safety.Intent); err != nil {
				return value, err
			}
			if safety.Intent.Outcome == contracts.Deny {
				return value, fmt.Errorf("intent guard denied %s: %s", decision.Tool, safety.Intent.Reason)
			}
			if err := o.appendTrace(ctx, value, trace.RiskEvaluated, safety.Risk); err != nil {
				return value, err
			}
			if safety.Final.Outcome != contracts.Allow || safety.Risk.Level != contracts.L0 {
				return value, fmt.Errorf("general runtime only allows L0 reads; %s resolved to %s/%s", decision.Tool, safety.Final.Outcome, safety.Risk.Level)
			}
			toolCtx, cancel := context.WithTimeout(ctx, o.timeout())
			if err := o.appendTrace(toolCtx, value, trace.ToolCall, map[string]any{"server": decision.ServerID, "tool": decision.Tool, "arguments": decision.Arguments}); err != nil {
				cancel()
				return value, err
			}
			result, callErr := o.Registry.CallTool(toolCtx, decision.ServerID, decision.Tool, decision.Arguments)
			cancel()
			if callErr != nil {
				return value, callErr
			}
			if result.IsError {
				return value, fmt.Errorf("tool %s: %s", decision.Tool, textResult(result))
			}
			if result.StructuredContent == nil {
				return value, fmt.Errorf("tool %s returned no structured content", decision.Tool)
			}
			captureSelectedResources(&value, decision.Tool, result.StructuredContent)
			observationJSON, err := boundedObservation(result.StructuredContent)
			if err != nil {
				return value, err
			}
			traceEvent, err := o.Trace.Append(ctx, value.ID, value.SessionID, trace.ToolResult, map[string]any{"server": decision.ServerID, "tool": decision.Tool, "structured_output": json.RawMessage(observationJSON)})
			if err != nil {
				return value, err
			}
			evidenceRef := fmt.Sprintf("trace://%s/%d", value.ID, traceEvent.Sequence)
			observation := task.RuntimeObservation{ServerID: decision.ServerID, Tool: decision.Tool, Arguments: arguments, Result: observationJSON, EvidenceRef: evidenceRef}
			observationErr := recordObservation(&value.Runtime, observation)
			value.Plan[len(value.Plan)-1].State = "COMPLETED"
			value.CompletedSteps = append(value.CompletedSteps, stepID)
			value.EvidenceRefs = append(value.EvidenceRefs, evidenceRef)
			finding := fmt.Sprintf("%s 返回结构化证据（%s）", decision.Tool, shortDigest(observationJSON))
			value.Findings = append(value.Findings, finding)
			if err := o.Store.SaveTask(ctx, value); err != nil {
				return value, err
			}
			if err := o.appendTrace(ctx, value, trace.FindingsUpdated, map[string]any{"finding": finding, "evidence_ref": evidenceRef}); err != nil {
				return value, err
			}
			emitEvent(emit, value, "已获得证据："+finding)
			if observationErr != nil {
				return value, observationErr
			}
		}
	}
}

func requireEvidenceBeforeFinal(decision llm.Decision, value task.Task, capabilities []llm.ToolCapability) (llm.Decision, bool, error) {
	if decision.Kind != llm.DecisionFinal || (len(value.Runtime.Observations) > 0 && len(value.EvidenceRefs) > 0) {
		return decision, false, nil
	}
	capability, ok := selectBootstrapEvidenceTool(capabilities)
	if !ok {
		return decision, false, errors.New("completion gate rejected a final answer without MCP evidence and no no-argument read tool is available for bootstrap evidence")
	}
	return llm.Decision{
		Kind:                llm.DecisionTool,
		DecisionSummary:     "模型在缺少 MCP 证据时尝试完成；本地安全门改为先采集只读系统证据",
		ServerID:            capability.ServerID,
		Tool:                capability.Name,
		Arguments:           map[string]any{},
		ExpectedObservation: "至少一条带 evidence_ref 的只读 MCP 结构化观察",
	}, true, nil
}

func selectBootstrapEvidenceTool(capabilities []llm.ToolCapability) (llm.ToolCapability, bool) {
	preferred := []string{
		"system\x00system.get_overview",
		"diagnostic\x00diagnostic.build_snapshot",
		"system\x00system.get_load_average",
		"system\x00system.get_memory_metrics",
		"system\x00system.get_cpu_metrics",
		"system\x00system.get_kernel_info",
		"system\x00system.get_uptime",
		"network\x00network.get_interfaces",
	}
	byID := map[string]llm.ToolCapability{}
	for _, capability := range capabilities {
		if validateToolArguments(capability.InputSchema, map[string]any{}) == nil {
			byID[capability.ServerID+"\x00"+capability.Name] = capability
		}
	}
	for _, key := range preferred {
		if capability, ok := byID[key]; ok {
			return capability, true
		}
	}
	for _, capability := range capabilities {
		if validateToolArguments(capability.InputSchema, map[string]any{}) == nil {
			return capability, true
		}
	}
	return llm.ToolCapability{}, false
}

func (o *Orchestrator) completeGeneral(ctx context.Context, value task.Task, answer string, emit EventSink) (task.Task, error) {
	answer = strings.TrimSpace(answer)
	value.Transition(task.Completed)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if _, err := o.Store.UpdateSession(ctx, value.SessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: value.ID, CreatedAt: time.Now().UTC()})
		s.Summary = "已完成受限 MCP 多步骤只读调查"
		if len(value.SelectedResources) > 0 {
			s.SelectedResources = append([]string(nil), value.SelectedResources...)
		}
		s.UpdatedAt = time.Now().UTC()
		return nil
	}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.TaskCompleted, map[string]any{"completion_criteria_met": true, "evidence_count": len(value.EvidenceRefs)}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.Final, map[string]any{"answer": answer, "evidence_refs": value.EvidenceRefs}); err != nil {
		return value, err
	}
	emitEvent(emit, value, "任务完成")
	return value, nil
}

func captureSelectedResources(value *task.Task, tool string, structured any) {
	if tool != "file.find_large" {
		return
	}
	object, ok := structured.(map[string]any)
	if !ok {
		return
	}
	items, ok := object["files"].([]any)
	if !ok {
		return
	}
	seen := map[string]bool{}
	for _, existing := range value.SelectedResources {
		seen[existing] = true
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path, _ := entry["path"].(string)
		if path != "" && !seen[path] && len(value.SelectedResources) < 100 {
			seen[path] = true
			value.SelectedResources = append(value.SelectedResources, path)
		}
	}
}

func plannerObservations(values []task.RuntimeObservation) []llm.Observation {
	out := make([]llm.Observation, 0, len(values))
	for _, value := range values {
		out = append(out, llm.Observation{Tool: value.Tool, Arguments: append(json.RawMessage(nil), value.Arguments...), Result: append(json.RawMessage(nil), value.Result...), EvidenceRef: value.EvidenceRef})
	}
	return out
}

func boundedObservation(value any) (json.RawMessage, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(b) <= maxObservationBytes {
		return b, nil
	}
	sum := sha256.Sum256(b)
	return json.Marshal(map[string]any{"truncated": true, "original_bytes": len(b), "sha256": hex.EncodeToString(sum[:]), "next_action": "rerun the same read tool with narrower bounded arguments"})
}

func shortDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:6])
}
