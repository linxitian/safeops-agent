package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"safeops-agent/contracts"
	sessioncontext "safeops-agent/internal/context"
	"safeops-agent/internal/id"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type FileTargetSnapshotter interface {
	SnapshotFile(context.Context, string, string) (contracts.TargetSnapshot, error)
	SnapshotNewFile(context.Context, string, string) (contracts.TargetSnapshot, error)
}

func detectFileAction(request string) string {
	normalized := strings.ToLower(strings.TrimSpace(request))
	if strings.Contains(normalized, "恢复") || strings.Contains(normalized, "restore") {
		if strings.Contains(normalized, "文件") || strings.Contains(normalized, "第") || strings.Contains(normalized, "这个") || strings.Contains(normalized, "它") {
			return "restore"
		}
	}
	if strings.Contains(normalized, "隔离") || strings.Contains(normalized, "quarantine") {
		return "quarantine"
	}
	if strings.Contains(normalized, "新建") || strings.Contains(normalized, "创建") || strings.Contains(normalized, "create") {
		return "create"
	}
	if strings.Contains(normalized, "删除") || strings.Contains(normalized, "delete") || strings.Contains(normalized, "remove") {
		return "delete"
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
	value.SelectedResources = append([]string(nil), s.SelectedResources...)
	value.IntentType = "file_" + kind
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	tool := "file.quarantine"
	targetPath, index, source := "", -1, "request.path"
	arguments := map[string]any{}
	rollbackStrategy := "restore file from quarantine manifest"
	description := "隔离已选择 Lab 文件"
	var snapshot contracts.TargetSnapshot
	switch kind {
	case "create":
		var content string
		targetPath, content, err = parseCreateFileRequest(request)
		if err != nil {
			return value, err
		}
		arguments["content"] = content
		tool = "file.create"
		rollbackStrategy = "delete created file through reversible quarantine"
		description = "新建 allowlist Lab 文件"
		snapshot, err = o.FileTargets.SnapshotNewFile(ctx, targetPath, targetPath)
	case "restore":
		var resource string
		resource, index, err = sessioncontext.ResolveResource(request, s.SelectedResources)
		if err != nil {
			return value, err
		}
		tool = "file.restore_quarantine"
		if s.PinnedContext == nil || s.PinnedContext["quarantine_id:"+resource] == "" || s.PinnedContext["quarantine_path:"+resource] == "" {
			return value, errors.New("selected resource has no committed quarantine context")
		}
		arguments["quarantine_id"] = s.PinnedContext["quarantine_id:"+resource]
		targetPath = s.PinnedContext["quarantine_path:"+resource]
		rollbackStrategy = "re-quarantine restored file"
		description = fmt.Sprintf("恢复第 %d 个已选择 Lab 文件", index+1)
		source = "session.selected_resources"
		snapshot, err = o.FileTargets.SnapshotFile(ctx, targetPath, targetPath)
	case "delete", "quarantine":
		targetPath, index, source, err = resolveFileActionTarget(request, s.SelectedResources)
		if err != nil {
			return value, err
		}
		if kind == "delete" {
			tool = "file.delete"
			description = "删除 allowlist Lab 文件（可恢复隔离）"
			if index >= 0 {
				description = fmt.Sprintf("删除第 %d 个已选择 Lab 文件（可恢复隔离）", index+1)
			}
		} else if index >= 0 {
			description = fmt.Sprintf("隔离第 %d 个已选择 Lab 文件", index+1)
		}
		snapshot, err = o.FileTargets.SnapshotFile(ctx, targetPath, targetPath)
	default:
		return value, fmt.Errorf("unsupported file action kind %q", kind)
	}
	if err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "resolved_resource": targetPath, "ordinal_index": index, "source": source}); err != nil {
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

func resolveFileActionTarget(request string, resources []string) (string, int, string, error) {
	if path := filePathFromRequest(request); path != "" {
		return path, -1, "request.path", nil
	}
	resource, index, err := sessioncontext.ResolveResource(request, resources)
	if err != nil {
		return "", -1, "", err
	}
	return resource, index, "session.selected_resources", nil
}

func parseCreateFileRequest(request string) (string, string, error) {
	return parseCreateFileRequestAt(request, time.Now())
}

func parseCreateFileRequestAt(request string, now time.Time) (string, string, error) {
	path := filePathFromRequest(request)
	if path == "" {
		return "", "", errors.New("file.create requires an absolute allowlisted target path in the request")
	}
	return path, parseCreateFileContent(request, now), nil
}

func filePathFromRequest(request string) string {
	path := firstAbsolutePath(request)
	if path == "" {
		return ""
	}
	if combined := splitJoinedActionPath(path); combined != "" {
		return combined
	}
	if filepath.Ext(filepath.Base(path)) == "" {
		if filename := firstPlainFilename(request); filename != "" {
			return filepath.Join(path, filename)
		}
	}
	return path
}

func splitJoinedActionPath(path string) string {
	markers := []string{"创建文件", "新建文件", "删除文件", "隔离文件", "恢复文件", "创建", "新建", "删除", "隔离", "恢复"}
	for _, marker := range markers {
		index := strings.LastIndex(path, marker)
		if index <= 0 {
			continue
		}
		directory := path[:index]
		if directory == "" || filepath.Ext(filepath.Base(directory)) != "" {
			continue
		}
		if filename := firstPlainFilename(path[index+len(marker):]); filename != "" {
			return filepath.Join(directory, filename)
		}
	}
	return ""
}

func parseCreateFileContent(request string, now time.Time) string {
	normalized := strings.ToLower(request)
	if strings.Contains(request, "今天日期") || strings.Contains(request, "今日日期") || strings.Contains(normalized, "today") {
		return now.Local().Format("2006-01-02")
	}
	for _, marker := range []string{"内容是", "内容为", "写入", "填写", "填入", "content:"} {
		if index := strings.Index(strings.ToLower(request), strings.ToLower(marker)); index >= 0 {
			content := strings.TrimSpace(request[index+len(marker):])
			if path := firstAbsolutePath(content); path != "" {
				if pathIndex := strings.Index(content, path); pathIndex >= 0 {
					content = content[:pathIndex]
				}
			}
			return strings.Trim(content, " \t\r\n\"'“”‘’，。；;：:在到至")
		}
	}
	return ""
}

func firstAbsolutePath(request string) string {
	re := regexp.MustCompile(`/[^\s"'，。；;]+`)
	match := re.FindString(request)
	return strings.TrimRight(match, ".,，。；;：:")
}

func firstPlainFilename(request string) string {
	re := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_./-])([A-Za-z0-9][A-Za-z0-9._-]{0,127}\.[A-Za-z0-9][A-Za-z0-9._-]{0,16})(?:$|[^A-Za-z0-9_./-])`)
	match := re.FindStringSubmatch(request)
	if len(match) < 2 {
		return ""
	}
	return filepath.Base(match[1])
}
