package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/id"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/safefs"
	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

const (
	demoLabRoot             = "/var/lib/safeops/lab"
	demoGrowthPath          = "/var/lib/safeops/lab/growth.log"
	demoLogWriterExecutable = "/opt/safeops/bin/safeops-log-writer"
)

func detectDiskRecovery(request string) bool {
	normalized := strings.ToLower(strings.TrimSpace(request))
	mentionsStorage := strings.Contains(normalized, "磁盘") || strings.Contains(normalized, "日志") || strings.Contains(normalized, "disk")
	mentionsGrowth := strings.Contains(normalized, "增长") || strings.Contains(normalized, "变大") || strings.Contains(normalized, "占用") || strings.Contains(normalized, "满")
	requestsAction := strings.Contains(normalized, "处理") || strings.Contains(normalized, "隔离") || strings.Contains(normalized, "恢复") || strings.Contains(normalized, "修复")
	return mentionsStorage && mentionsGrowth && requestsAction
}

func fixedLabReadScopeCompatible(request string, allowedPaths ...string) bool {
	authorized, excluded := requestPathScopes(request)
	allowed := make(map[string]bool, len(allowedPaths))
	for _, path := range normalizedUniquePaths(allowedPaths) {
		allowed[path] = true
	}
	for _, path := range authorized {
		if !allowed[path] {
			return false
		}
	}
	for _, path := range excluded {
		if pathWithinScope(demoLabRoot, path) || pathWithinScope(path, demoLabRoot) {
			return false
		}
	}
	return true
}

func (o *Orchestrator) runDiskRecovery(ctx context.Context, value task.Task, emit EventSink) (task.Task, error) {
	if o.Actions == nil || o.ActionTargets == nil || o.FileTargets == nil || o.Registry == nil || o.Safety == nil || o.Trace == nil {
		return value, errors.New("disk recovery requires MCP, safety, approvals, process/file snapshots, and trace")
	}
	value.IntentType = "disk_log_recovery"
	value.Plan = []task.Step{
		{ID: "disk_pressure", Description: "读取 Lab 所在文件系统容量证据", Tool: "diagnostic.disk_pressure", State: "PENDING"},
		{ID: "disk_large", Description: "定位 Lab 内异常增长文件", Tool: "file.find_large", State: "PENDING"},
		{ID: "disk_writer", Description: "定位命令行绑定 growth.log 的受控写入进程", Tool: "process.search", State: "PENDING"},
		{ID: "disk_stop_writer", Description: "终止证据绑定的受控日志写入进程", Tool: "process.terminate", State: "PENDING"},
		{ID: "disk_quarantine", Description: "把 growth.log 原子隔离到可恢复清单", Tool: "file.quarantine", State: "PENDING"},
		{ID: "disk_verify_writer", Description: "验证受控写入进程已消失", Tool: "process.search", State: "PENDING"},
		{ID: "disk_verify_active", Description: "验证活动 Lab 目录不再包含 growth.log", Tool: "file.find_large", State: "PENDING"},
		{ID: "disk_verify_fs", Description: "重新读取文件系统容量并声明隔离语义", Tool: "system.get_disk_usage", State: "PENDING"},
	}
	value.WorkflowData = map[string]json.RawMessage{}
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "lab_root": demoLabRoot, "growth_path": demoGrowthPath, "scope": "fixed controlled Lab"}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"steps": value.Plan, "completion_criteria": "writer absent, growth.log absent from active Lab root, quarantine manifest retained; no claim of physical filesystem reclamation"}); err != nil {
		return value, err
	}
	emitEvent(emit, value, "正在采集文件系统、大文件和写入进程证据")
	reads := []struct {
		server, tool string
		arguments    map[string]any
	}{
		{server: "diagnostic", tool: "diagnostic.disk_pressure", arguments: map[string]any{"path": demoLabRoot, "warning_ratio": .5}},
		{server: "file", tool: "file.find_large", arguments: map[string]any{"path": demoLabRoot, "minimum_bytes": 1 << 20, "max_depth": 2, "limit": 20}},
		{server: "process", tool: "process.search", arguments: map[string]any{"query": "safeops-log-writer", "limit": 20}},
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
	var large struct {
		Files []safefs.Metadata `json:"files"`
	}
	var processes struct {
		Processes []platform.ProcessInfo `json:"processes"`
	}
	if err := decodeStructured(outputs[1], &large); err != nil {
		return value, err
	}
	if err := decodeStructured(outputs[2], &processes); err != nil {
		return value, err
	}
	file, err := exactGrowthFile(large.Files)
	if err != nil {
		return value, err
	}
	writer, err := uniqueControlledProcess(processes.Processes, demoLogWriterExecutable)
	if err != nil {
		return value, err
	}
	if !strings.Contains(writer.Command, demoGrowthPath) {
		return value, errors.New("log writer command evidence is not bound to the fixed growth.log path")
	}
	components := rca.ConfidenceComponents{SignalMatch: 1, GraphConsistency: 1}
	diagnosis := rca.Result{DiagnosisLevel: rca.D2, ConfidenceComponents: components, Confidence: components.Score(), Culprit: fmt.Sprintf("pid:%d:start:%d", writer.PID, writer.StartTicks), CandidateCauses: []rca.CandidateCause{{Cause: "受控 safeops-log-writer 命令行绑定 growth.log，且该文件达到大型文件阈值", Score: components.Score(), EvidenceRefs: append([]string(nil), value.EvidenceRefs...)}}, EvidenceRefs: append([]string(nil), value.EvidenceRefs...), MissingEvidence: []string{"物理文件系统空间只能在永久清理或跨文件系统迁移后释放"}, Remediation: []string{"先终止受控写入进程", "把文件隔离到可恢复清单", "不要宣称同文件系统隔离释放了物理空间"}}
	if err := o.appendTrace(ctx, value, trace.RCAResult, diagnosis); err != nil {
		return value, err
	}
	sizeJSON, _ := json.Marshal(file.SizeBytes)
	value.WorkflowData["growth_size_bytes"] = sizeJSON
	value.SelectedResources = []string{demoGrowthPath}
	targetID := fmt.Sprintf("pid:%d:start:%d", writer.PID, writer.StartTicks)
	snapshot, err := o.ActionTargets.SnapshotProcess(ctx, targetID, writer.PID)
	if err != nil {
		return value, err
	}
	if snapshot.StartTicks != writer.StartTicks || snapshot.Executable != demoLogWriterExecutable {
		return value, errors.New("TARGET_CHANGED: log writer snapshot no longer matches investigation evidence")
	}
	processTarget := contracts.TargetRef{Type: "process", ID: snapshot.ID}
	fileTarget := contracts.TargetRef{Type: "file", ID: demoGrowthPath}
	value.CurrentStep = 3
	value.Plan[3].State = "WAITING_APPROVAL"
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "selected_hypothesis": diagnosis.CandidateCauses, "evidence_used": value.EvidenceRefs, "selected_tool": "process.terminate", "target": processTarget, "expected_observation": "exact writer process disappears before file isolation"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: "process.terminate", Effect: contracts.Write, Arguments: map[string]any{"pid": writer.PID}, Target: processTarget, BatchSize: 1, Reversible: false, LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{fileTarget}, PlanStep: value.Plan[3].Description, PlanTargets: []contracts.TargetRef{processTarget}, AuthorizedRelations: []contracts.TargetAuthorization{{Target: processTarget, RelatedTo: fileTarget, Relation: "PROCESS_WRITES_FILE", EvidenceRefs: append([]string(nil), value.EvidenceRefs...)}}}}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		return waiting, err
	}
	emitEvent(emit, waiting, "日志写入进程已通过双重护栏，等待 L2 人工审批")
	return waiting, nil
}

func (o *Orchestrator) prepareDiskQuarantine(ctx context.Context, value task.Task) (task.Task, error) {
	snapshot, err := o.FileTargets.SnapshotFile(ctx, demoGrowthPath, demoGrowthPath)
	if err != nil {
		return value, err
	}
	if snapshot.Size <= 0 {
		return value, errors.New("fresh growth.log snapshot has no quarantinable bytes")
	}
	freshSize, _ := json.Marshal(snapshot.Size)
	value.WorkflowData["growth_size_bytes"] = freshSize
	value.Findings = append(value.Findings, fmt.Sprintf("writer 停止后重新采集 growth.log 快照：%d 字节；第二次审批将只绑定该新快照", snapshot.Size))
	target := contracts.TargetRef{Type: "file", ID: snapshot.ID}
	value.CurrentStep = 4
	value.Plan[4].State = "WAITING_APPROVAL"
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "current_step": value.Plan[4].Description, "evidence_used": value.EvidenceRefs, "selected_tool": "file.quarantine", "target": target, "fresh_snapshot_size": snapshot.Size, "rollback_strategy": "restore exact quarantine manifest"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: "file.quarantine", Effect: contracts.Write, Arguments: map[string]any{}, Target: target, BatchSize: 1, Reversible: true, RollbackStrategy: "restore exact quarantine manifest", LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: value.Plan[4].Description, PlanTargets: []contracts.TargetRef{target}}}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	return waiting, err
}

func (o *Orchestrator) verifyDiskRecovery(ctx context.Context, value task.Task, result executor.Result) (task.Task, error) {
	value.Transition(task.Verifying)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	verified, processOutput, err := o.callWorkflowReadStep(ctx, value, 5, "process", "process.search", map[string]any{"query": "safeops-log-writer", "limit": 20})
	if err != nil {
		return verified, err
	}
	verified, fileOutput, err := o.callWorkflowReadStep(ctx, verified, 6, "file", "file.find_large", map[string]any{"path": demoLabRoot, "minimum_bytes": 1 << 20, "max_depth": 2, "limit": 20})
	if err != nil {
		return verified, err
	}
	verified, diskOutput, err := o.callWorkflowReadStep(ctx, verified, 7, "system", "system.get_disk_usage", map[string]any{"path": demoLabRoot})
	if err != nil {
		return verified, err
	}
	var processes struct {
		Processes []platform.ProcessInfo `json:"processes"`
	}
	var large struct {
		Files []safefs.Metadata `json:"files"`
	}
	if err := decodeStructured(processOutput, &processes); err != nil {
		return verified, err
	}
	if err := decodeStructured(fileOutput, &large); err != nil {
		return verified, err
	}
	for _, process := range processes.Processes {
		if process.Executable == demoLogWriterExecutable {
			return verified, errors.New("controlled log writer is still running")
		}
	}
	for _, file := range large.Files {
		if file.Path == demoGrowthPath {
			return verified, errors.New("growth.log is still present in the active Lab root")
		}
	}
	var size int64
	if json.Unmarshal(verified.WorkflowData["growth_size_bytes"], &size) != nil || size <= 0 {
		return verified, errors.New("durable growth.log size evidence is missing")
	}
	quarantineID, quarantinePath := "", ""
	if result.Verification != nil {
		quarantineID = result.Verification.Evidence["quarantine_id"]
		quarantinePath = result.Verification.Evidence["quarantined_path"]
	}
	if quarantineID == "" || quarantinePath == "" {
		return verified, errors.New("verified quarantine manifest evidence is missing")
	}
	verificationEvent, err := o.Trace.Append(ctx, verified.ID, verified.SessionID, trace.VerificationResult, map[string]any{"verified": true, "writer_absent": true, "active_lab_path_absent": true, "active_lab_bytes_relocated": size, "quarantine_id": quarantineID, "quarantine_path": quarantinePath, "physical_filesystem_space_reclaimed": false, "fresh_disk_observation": diskOutput})
	if err != nil {
		return verified, err
	}
	verified.EvidenceRefs = append(verified.EvidenceRefs, fmt.Sprintf("trace://%s/%d", verified.ID, verificationEvent.Sequence))
	verified.Findings = append(verified.Findings, fmt.Sprintf("写入进程已停止，growth.log 的 %d 字节已移出活动 Lab 路径并保留可恢复隔离清单；未声称释放物理文件系统空间", size))
	answer := fmt.Sprintf("已确认受控日志写入进程与 %s 的异常增长相关。进程经 L2 审批和固定 SIGTERM 停止，文件经独立 L1 审批隔离；活动 Lab 路径已减少 %d 字节，并已验证写入进程和原路径均不存在。隔离对象仍占用同一文件系统空间，以保留可恢复性；系统没有伪称物理空间已经释放。", demoGrowthPath, size)
	verified.Transition(task.Completed)
	if err := o.Store.SaveTask(ctx, verified); err != nil {
		return verified, err
	}
	if _, err := o.Store.UpdateSession(ctx, verified.SessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: verified.ID, CreatedAt: time.Now().UTC()})
		s.SelectedResources = []string{demoGrowthPath}
		s.Summary = "受控日志增长已完成证据关联、逐动作审批和可恢复隔离验证"
		s.UpdatedAt = time.Now().UTC()
		return nil
	}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.TaskCompleted, map[string]any{"completion_criteria_met": true, "writer_absent": true, "active_lab_path_absent": true, "rollback_available": true, "physical_space_claimed": false}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.Final, map[string]any{"answer": answer, "evidence_refs": verified.EvidenceRefs}); err != nil {
		return verified, err
	}
	return verified, nil
}

func exactGrowthFile(values []safefs.Metadata) (safefs.Metadata, error) {
	for _, value := range values {
		if value.Path == demoGrowthPath && value.IsRegular {
			return value, nil
		}
	}
	return safefs.Metadata{}, errors.New("fixed growth.log was not found above the controlled large-file threshold")
}
