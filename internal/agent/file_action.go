package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"safeops-agent/contracts"
	sessioncontext "safeops-agent/internal/context"
	"safeops-agent/internal/id"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type FileTargetSnapshotter interface {
	SnapshotFile(context.Context, string, string) (contracts.TargetSnapshot, error)
}

func detectFileAction(request string) string {
	normalized := strings.ToLower(strings.TrimSpace(request))
	if strings.Contains(normalized, "隔离") || strings.Contains(normalized, "quarantine") {
		return "quarantine"
	}
	if strings.Contains(normalized, "恢复") || strings.Contains(normalized, "restore") {
		if strings.Contains(normalized, "文件") || strings.Contains(normalized, "第") || strings.Contains(normalized, "这个") || strings.Contains(normalized, "它") {
			return "restore"
		}
	}
	return ""
}

func (o *Orchestrator) runFileAction(ctx context.Context, value task.Task, kind, request string, emit EventSink) (task.Task, error) {
	if o.Actions == nil || o.FileTargets == nil {
		return value, errors.New("file action workflow is not configured; read-only investigation remains available")
	}
	s, err := o.Store.GetSession(ctx, value.SessionID)
	if err != nil {
		return value, err
	}
	resource, index, err := sessioncontext.ResolveResource(request, s.SelectedResources)
	if err != nil {
		return value, err
	}
	value.SelectedResources = append([]string(nil), s.SelectedResources...)
	value.IntentType = "file_" + kind
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "resolved_resource": resource, "ordinal_index": index, "source": "session.selected_resources"}); err != nil {
		return value, err
	}
	tool := "file.quarantine"
	targetPath := resource
	arguments := map[string]any{}
	rollbackStrategy := "restore file from quarantine manifest"
	description := fmt.Sprintf("隔离第 %d 个已选择 Lab 文件", index+1)
	if kind == "restore" {
		tool = "file.restore_quarantine"
		if s.PinnedContext == nil || s.PinnedContext["quarantine_id:"+resource] == "" || s.PinnedContext["quarantine_path:"+resource] == "" {
			return value, errors.New("selected resource has no committed quarantine context")
		}
		arguments["quarantine_id"] = s.PinnedContext["quarantine_id:"+resource]
		targetPath = s.PinnedContext["quarantine_path:"+resource]
		rollbackStrategy = "re-quarantine restored file"
		description = fmt.Sprintf("恢复第 %d 个已选择 Lab 文件", index+1)
	}
	snapshot, err := o.FileTargets.SnapshotFile(ctx, targetPath, targetPath)
	if err != nil {
		return value, err
	}
	target := contracts.TargetRef{Type: "file", ID: snapshot.ID}
	value.Plan = []task.Step{{ID: "step_file_action", Description: description, Tool: tool, State: "WAITING_APPROVAL"}}
	value.CurrentStep = 0
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"steps": value.Plan, "completion_criteria": "approved action executes against the same file snapshot and verification succeeds"}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "decision_summary": description, "selected_tool": tool, "target": target, "rollback_strategy": rollbackStrategy}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: tool, Effect: contracts.Write, Arguments: arguments, Target: target, BatchSize: 1, Reversible: true, RollbackStrategy: rollbackStrategy, LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: description, PlanTargets: []contracts.TargetRef{target}}}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		return waiting, err
	}
	emitEvent(emit, waiting, "操作已通过双重护栏，正在等待人工审批")
	return waiting, nil
}
