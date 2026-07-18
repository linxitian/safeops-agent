package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"safeops-agent/contracts"
	"safeops-agent/internal/id"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

var (
	managedServiceNoArgsSchema = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	managedProcessSchema       = json.RawMessage(`{"type":"object","properties":{"pid":{"type":"integer","minimum":2,"maximum":4194304}},"required":["pid"],"additionalProperties":false}`)
	processTargetPattern       = regexp.MustCompile(`^pid:([1-9][0-9]{0,9}):start:([0-9]{1,20})$`)
	managedServiceNegative     = regexp.MustCompile(`(?:不要|别|不得|禁止|无需|不需|不应|不能|不可)(?:再|去|执行)?(?:重启|重新启动)|(?:do not|don't|dont|never|must not|should not|cannot|can't|without)(?:\s+\w+){0,3}\s+restart(?:ing)?\b|(?:no|avoid)\s+(?:service\s+)?restart(?:ing)?\b`)
	managedProcessNegative     = regexp.MustCompile(`(?:不要|别|不得|禁止|无需|不需|不应|不能|不可)(?:再|去|执行)?(?:终止|结束|停止|杀掉|杀死|杀)(?:该|这个|目标)?进程|(?:do not|don't|dont|never|must not|should not|cannot|can't|without)(?:\s+\w+){0,3}\s+(?:terminate|kill|stop)(?:ing)?\b|(?:no|avoid)\s+(?:process\s+)?(?:termination|killing|stopping)\b`)
)

func (o *Orchestrator) managedActionCapabilities() []llm.ManagedActionCapability {
	if o.Actions == nil || o.ActionTargets == nil {
		return nil
	}
	return []llm.ManagedActionCapability{
		{
			Name:              "service.restart",
			Description:       "Request approval to restart one allowlisted systemd service. target.type must be service and target.id must be the exact unit name from MCP evidence. arguments must be empty.",
			TargetType:        "service",
			InputSchema:       managedServiceNoArgsSchema,
			ApprovalRequired:  true,
			Reversible:        false,
			RequiresEvidence:  true,
			ExecutionBoundary: "fixed SafeOps service.restart handler; unit comes from revalidated target snapshot; no shell or model command string",
		},
		{
			Name:              "process.terminate",
			Description:       "Request approval to send fixed SIGTERM to one allowlisted process. target.type must be process and target.id must be pid:<pid>:start:<start_ticks> from MCP evidence. arguments.pid must match target.id.",
			TargetType:        "process",
			InputSchema:       managedProcessSchema,
			ApprovalRequired:  true,
			Reversible:        false,
			RequiresEvidence:  true,
			ExecutionBoundary: "fixed SafeOps process.terminate handler; PID/start/executable snapshot is revalidated; no shell, SIGKILL, or model command string",
		},
	}
}

func managedActionByName(capabilities []llm.ManagedActionCapability, name string) (llm.ManagedActionCapability, bool) {
	for _, capability := range capabilities {
		if capability.Name == name {
			return capability, true
		}
	}
	return llm.ManagedActionCapability{}, false
}

func (o *Orchestrator) prepareGeneralManagedAction(ctx context.Context, value task.Task, decision llm.Decision, capabilities []llm.ManagedActionCapability, emit EventSink) (task.Task, error) {
	if o.Actions == nil || o.ActionTargets == nil {
		return value, errors.New("managed write actions are not configured")
	}
	capability, ok := managedActionByName(capabilities, strings.TrimSpace(decision.Tool))
	if !ok {
		return value, fmt.Errorf("planner selected unavailable managed action %s", decision.Tool)
	}
	if decision.Arguments == nil {
		decision.Arguments = map[string]any{}
	}
	if err := validateToolArguments(capability.InputSchema, decision.Arguments); err != nil {
		return value, fmt.Errorf("managed action arguments failed schema: %w", err)
	}
	target := contracts.TargetRef{Type: strings.ToLower(strings.TrimSpace(decision.Target.Type)), ID: strings.TrimSpace(decision.Target.ID), Criticality: strings.TrimSpace(decision.Target.Criticality)}
	if target.Type != capability.TargetType {
		return value, fmt.Errorf("managed action %s requires target type %s", capability.Name, capability.TargetType)
	}
	if !managedActionIntentAllows(value.OriginalRequest, capability.Name) {
		return value, fmt.Errorf("operator request does not explicitly authorize managed action %s", capability.Name)
	}
	arguments := cloneArguments(decision.Arguments)
	snapshot, err := o.snapshotManagedActionTarget(ctx, capability.Name, target, arguments)
	if err != nil {
		return value, err
	}
	if !managedActionEvidenceSupports(value, target, snapshot) {
		return value, errors.New("managed action target is not supported by prior MCP evidence")
	}
	description := strings.TrimSpace(decision.DecisionSummary)
	if description == "" {
		description = "申请受管写动作"
	}
	stepID := fmt.Sprintf("step_%02d", len(value.Plan)+1)
	value.Plan = append(value.Plan, task.Step{ID: stepID, Description: description, Tool: capability.Name, State: "WAITING_APPROVAL"})
	value.CurrentStep = len(value.Plan) - 1
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"strategy": "managed_action_request", "step_id": stepID, "tool": capability.Name, "target": target, "completion_criteria": "human-approved action executes against the same revalidated target snapshot"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{
		ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID,
		Tool: capability.Name, Effect: contracts.Write, Arguments: arguments, Target: target,
		BatchSize: 1, Reversible: capability.Reversible, RollbackStrategy: capability.RollbackStrategy, LabMode: true,
		Intent: contracts.IntentContext{
			OriginalRequest:  value.OriginalRequest,
			Objective:        value.Objective,
			ObjectiveTargets: []contracts.TargetRef{target},
			PlanStep:         description,
			PlanTargets:      []contracts.TargetRef{target},
		},
	}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		return waiting, err
	}
	emitEvent(emit, waiting, "受管动作已通过本地护栏，正在等待人工审批")
	return waiting, nil
}

func (o *Orchestrator) snapshotManagedActionTarget(ctx context.Context, tool string, target contracts.TargetRef, arguments map[string]any) (contracts.TargetSnapshot, error) {
	switch tool {
	case "service.restart":
		if target.ID == "" {
			return contracts.TargetSnapshot{}, errors.New("service action target is required")
		}
		return o.ActionTargets.SnapshotService(ctx, target.ID, target.ID)
	case "process.terminate":
		pid, startTicks, err := parseManagedProcessTarget(target.ID)
		if err != nil {
			return contracts.TargetSnapshot{}, err
		}
		argumentPID, err := integerArgument(arguments, "pid")
		if err != nil {
			return contracts.TargetSnapshot{}, err
		}
		if argumentPID != pid {
			return contracts.TargetSnapshot{}, errors.New("process.terminate pid argument does not match target id")
		}
		snapshot, err := o.ActionTargets.SnapshotProcess(ctx, target.ID, pid)
		if err != nil {
			return contracts.TargetSnapshot{}, err
		}
		if snapshot.StartTicks != startTicks {
			return contracts.TargetSnapshot{}, errors.New("TARGET_CHANGED: process start time no longer matches MCP evidence")
		}
		return snapshot, nil
	default:
		return contracts.TargetSnapshot{}, fmt.Errorf("unsupported managed action %s", tool)
	}
}

func parseManagedProcessTarget(targetID string) (int, uint64, error) {
	match := processTargetPattern.FindStringSubmatch(strings.TrimSpace(targetID))
	if len(match) != 3 {
		return 0, 0, errors.New("process target id must be pid:<pid>:start:<start_ticks>")
	}
	pid64, err := strconv.ParseInt(match[1], 10, 32)
	if err != nil || pid64 <= 1 {
		return 0, 0, errors.New("process target pid is invalid")
	}
	startTicks, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil || startTicks == 0 {
		return 0, 0, errors.New("process target start_ticks is invalid")
	}
	return int(pid64), startTicks, nil
}

func integerArgument(arguments map[string]any, name string) (int, error) {
	value, exists := arguments[name]
	if !exists {
		return 0, fmt.Errorf("%s is required", name)
	}
	number, ok := numeric(value)
	if !ok || number != number {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if number > float64(int(^uint(0)>>1)) || number < float64(-int(^uint(0)>>1)-1) {
		return 0, fmt.Errorf("%s is outside int range", name)
	}
	if number != float64(int(number)) {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return int(number), nil
}

func cloneArguments(arguments map[string]any) map[string]any {
	out := make(map[string]any, len(arguments))
	for key, value := range arguments {
		out[key] = value
	}
	return out
}

func managedActionEvidenceSupports(value task.Task, target contracts.TargetRef, snapshot contracts.TargetSnapshot) bool {
	if len(value.EvidenceRefs) == 0 || len(value.Runtime.Observations) == 0 {
		return false
	}
	for _, observation := range value.Runtime.Observations {
		if strings.TrimSpace(observation.EvidenceRef) == "" {
			continue
		}
		var structured any
		if err := json.Unmarshal(observation.Result, &structured); err != nil {
			continue
		}
		switch target.Type {
		case "service":
			if !serviceEvidenceTool(observation.Tool) {
				continue
			}
			if structuredServiceIdentity(structured, target.ID, snapshot.ServiceName) {
				return true
			}
		case "process":
			if !processEvidenceTool(observation.Tool) {
				continue
			}
			if structuredProcessIdentity(structured, snapshot.PID, snapshot.StartTicks) {
				return true
			}
		}
	}
	return false
}

func managedActionIntentAllows(request, tool string) bool {
	request = strings.ToLower(strings.TrimSpace(request))
	if containsAny(request, "不要执行", "不要操作", "禁止执行", "只检查", "仅检查", "只查看", "仅查看", "只读", "只建议", "仅建议", "do not execute", "don't execute", "do not act", "read only", "read-only", "only inspect", "only check", "advice only") {
		return false
	}
	switch tool {
	case "service.restart":
		if managedServiceNegative.MatchString(request) {
			return false
		}
		return containsAny(request, "重启", "重新启动", "restart service", "restart the service", "service restart")
	case "process.terminate":
		if managedProcessNegative.MatchString(request) {
			return false
		}
		return containsAny(request, "终止进程", "结束进程", "停止进程", "杀掉进程", "terminate process", "terminate the process", "kill process", "kill the process", "stop process", "stop the process")
	default:
		return false
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func serviceEvidenceTool(tool string) bool {
	switch tool {
	case "service.get_status", "service.list_failed", "service.get_restart_count", "diagnostic.port_conflict", "diagnostic.build_snapshot":
		return true
	default:
		return false
	}
}

func processEvidenceTool(tool string) bool {
	switch tool {
	case "process.list_top", "process.search", "process.get_details", "process.get_resource_usage", "process.find_by_port", "network.check_port", "diagnostic.port_conflict", "diagnostic.high_cpu", "diagnostic.build_snapshot":
		return true
	default:
		return false
	}
}

func structuredServiceIdentity(value any, names ...string) bool {
	wanted := map[string]bool{}
	for _, name := range names {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			wanted[name] = true
		}
	}
	return walkStructured(value, func(object map[string]any) bool {
		for _, key := range []string{"name", "unit", "service_name"} {
			name, ok := object[key].(string)
			if ok && wanted[strings.ToLower(strings.TrimSpace(name))] {
				return true
			}
		}
		return false
	})
}

func structuredProcessIdentity(value any, pid int, startTicks uint64) bool {
	return walkStructured(value, func(object map[string]any) bool {
		observedPID, pidOK := numeric(object["pid"])
		observedStart, startOK := numeric(object["start_ticks"])
		return pidOK && startOK && observedPID == float64(pid) && observedStart == float64(startTicks)
	})
}

func walkStructured(value any, match func(map[string]any) bool) bool {
	switch typed := value.(type) {
	case map[string]any:
		if match(typed) {
			return true
		}
		for _, child := range typed {
			if walkStructured(child, match) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if walkStructured(child, match) {
				return true
			}
		}
	}
	return false
}
