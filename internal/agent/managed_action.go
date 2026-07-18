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
		result := string(observation.Result)
		lowerResult := strings.ToLower(result)
		switch target.Type {
		case "service":
			for _, name := range []string{target.ID, snapshot.ServiceName} {
				name = strings.ToLower(strings.TrimSpace(name))
				if name != "" && strings.Contains(lowerResult, name) {
					return true
				}
			}
		case "process":
			pidToken := `"pid":` + strconv.Itoa(snapshot.PID)
			startToken := `"start_ticks":` + strconv.FormatUint(snapshot.StartTicks, 10)
			if strings.Contains(result, pidToken) && strings.Contains(result, startToken) {
				return true
			}
		}
	}
	return false
}
