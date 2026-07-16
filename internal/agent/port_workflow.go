package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/id"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/retrieval"
	"safeops-agent/internal/session"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

const (
	demoServiceUnit      = "safeops-demo-web.service"
	demoPort             = 18081
	demoHolderExecutable = "/opt/safeops/bin/safeops-port-holder"
	demoHealthURL        = "http://127.0.0.1:18081/healthz"
)

type HealthEvidence struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	BodySHA256 string `json:"body_sha256"`
}

type HealthVerifier interface {
	Verify(context.Context, string) (HealthEvidence, error)
}

type HTTPHealthVerifier struct {
	Client *http.Client
}

func (v HTTPHealthVerifier) Verify(ctx context.Context, endpoint string) (HealthEvidence, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host != "127.0.0.1:18081" || parsed.Path != "/healthz" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return HealthEvidence{}, errors.New("health endpoint is outside the fixed loopback Demo target")
	}
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return HealthEvidence{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return HealthEvidence{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		return HealthEvidence{}, err
	}
	if len(body) > 4096 {
		return HealthEvidence{}, errors.New("health response exceeds 4 KiB")
	}
	if response.StatusCode != http.StatusOK {
		return HealthEvidence{}, fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	sum := sha256.Sum256(body)
	return HealthEvidence{URL: endpoint, StatusCode: response.StatusCode, BodySHA256: hex.EncodeToString(sum[:])}, nil
}

func detectPortRecovery(request string) bool {
	normalized := strings.ToLower(strings.TrimSpace(request))
	mentionsWeb := strings.Contains(normalized, "web") || strings.Contains(normalized, "网页")
	mentionsFailure := strings.Contains(normalized, "启动失败") || strings.Contains(normalized, "起不来") || strings.Contains(normalized, "failed to start")
	requestsRecovery := strings.Contains(normalized, "恢复") || strings.Contains(normalized, "修复") || strings.Contains(normalized, "recover")
	return mentionsWeb && mentionsFailure && requestsRecovery
}

func (o *Orchestrator) runPortRecovery(ctx context.Context, value task.Task, emit EventSink) (task.Task, error) {
	if o.Actions == nil || o.ActionTargets == nil {
		return value, errors.New("port recovery requires approval-bound action preparation and target snapshots")
	}
	if o.Registry == nil || o.Safety == nil || o.Trace == nil {
		return value, errors.New("port recovery read, safety, and trace dependencies are required")
	}
	value.IntentType = "port_conflict_recovery"
	value.Plan = []task.Step{
		{ID: "port_status", Description: "读取 Demo Web 服务状态", Tool: "service.get_status", State: "PENDING"},
		{ID: "port_journal", Description: "读取 Demo Web 服务日志", Tool: "journal.query_unit", State: "PENDING"},
		{ID: "port_socket", Description: "检查 Demo 端口占用", Tool: "network.check_port", State: "PENDING"},
		{ID: "port_owner", Description: "定位 Demo 端口占用进程", Tool: "process.find_by_port", State: "PENDING"},
		{ID: "port_rca", Description: "关联多源证据并生成 RCA", Tool: "diagnostic.port_conflict", State: "PENDING"},
		{ID: "port_terminate", Description: "终止证据绑定的受控端口占用进程", Tool: "process.terminate", State: "PENDING"},
		{ID: "port_restart", Description: "重启 allowlist Demo Web 服务", Tool: "service.restart", State: "PENDING"},
		{ID: "port_verify_service", Description: "验证 Demo Web 服务 active", Tool: "service.get_status", State: "PENDING"},
		{ID: "port_verify_socket", Description: "验证 Demo 端口重新监听", Tool: "network.check_port", State: "PENDING"},
		{ID: "port_verify_http", Description: "验证 loopback HTTP healthz", Tool: "http.verify_health", State: "PENDING"},
	}
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentParsed, map[string]any{"intent_type": value.IntentType, "unit": demoServiceUnit, "port": demoPort, "scope": "fixed controlled Lab"}); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.PlanCreated, map[string]any{"steps": value.Plan, "completion_criteria": "service active, port listening, loopback health HTTP 200; each write separately approved and verified"}); err != nil {
		return value, err
	}
	emitEvent(emit, value, "正在采集端口冲突的多源证据")

	var outputs [5]any
	reads := []struct {
		server    string
		tool      string
		arguments map[string]any
	}{
		{server: "service", tool: "service.get_status", arguments: map[string]any{"unit": demoServiceUnit}},
		{server: "journal", tool: "journal.query_unit", arguments: map[string]any{"unit": demoServiceUnit, "lines": 200}},
		{server: "network", tool: "network.check_port", arguments: map[string]any{"port": demoPort, "protocol": "tcp"}},
		{server: "process", tool: "process.find_by_port", arguments: map[string]any{"port": demoPort}},
		{server: "diagnostic", tool: "diagnostic.port_conflict", arguments: map[string]any{"unit": demoServiceUnit, "port": demoPort}},
	}
	var err error
	for index, read := range reads {
		value, outputs[index], err = o.callWorkflowReadStep(ctx, value, index, read.server, read.tool, read.arguments)
		if err != nil {
			return value, err
		}
		emitEvent(emit, value, "已获得证据："+read.tool)
	}

	var status struct {
		Service platform.ServiceStatus `json:"service"`
	}
	var port struct {
		Occupied bool `json:"occupied"`
	}
	var owners struct {
		Processes []platform.ProcessInfo `json:"processes"`
	}
	var diagnosis struct {
		Diagnosis struct {
			RCA rca.Result `json:"rca"`
		} `json:"diagnosis"`
		Knowledge []retrieval.Result `json:"knowledge"`
	}
	if err := decodeStructured(outputs[0], &status); err != nil {
		return value, fmt.Errorf("decode service evidence: %w", err)
	}
	if err := decodeStructured(outputs[2], &port); err != nil {
		return value, fmt.Errorf("decode port evidence: %w", err)
	}
	if err := decodeStructured(outputs[3], &owners); err != nil {
		return value, fmt.Errorf("decode process evidence: %w", err)
	}
	if err := decodeStructured(outputs[4], &diagnosis); err != nil {
		return value, fmt.Errorf("decode RCA evidence: %w", err)
	}
	if len(diagnosis.Knowledge) > 0 {
		if err := o.appendTrace(ctx, value, trace.KnowledgeRetrieved, map[string]any{"results": diagnosis.Knowledge, "retrieved_document_refs": knowledgeRefs(diagnosis.Knowledge)}); err != nil {
			return value, err
		}
	}
	if err := o.appendTrace(ctx, value, trace.RCAResult, diagnosis.Diagnosis.RCA); err != nil {
		return value, err
	}
	if status.Service.ActiveState == "active" {
		return value, errors.New("Demo service is already active; destructive recovery is not justified")
	}
	if !port.Occupied || diagnosis.Diagnosis.RCA.DiagnosisLevel != rca.D1 || diagnosis.Diagnosis.RCA.RootCause == "" {
		return value, errors.New("complete D1 port-conflict evidence is required before proposing process termination")
	}
	owner, err := uniqueDemoHolder(owners.Processes)
	if err != nil {
		return value, err
	}
	targetID := fmt.Sprintf("pid:%d:start:%d", owner.PID, owner.StartTicks)
	snapshot, err := o.ActionTargets.SnapshotProcess(ctx, targetID, owner.PID)
	if err != nil {
		return value, err
	}
	if snapshot.StartTicks != owner.StartTicks || snapshot.Executable != demoHolderExecutable {
		return value, errors.New("TARGET_CHANGED: port owner snapshot no longer matches the controlled holder evidence")
	}
	processTarget := contracts.TargetRef{Type: "process", ID: snapshot.ID}
	serviceTarget := contracts.TargetRef{Type: "service", ID: demoServiceUnit}
	value.CurrentStep = 5
	value.Plan[5].State = "WAITING_APPROVAL"
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "selected_hypothesis": diagnosis.Diagnosis.RCA.RootCause, "evidence_used": value.EvidenceRefs, "selected_tool": "process.terminate", "target": processTarget, "expected_observation": "exact PID/start/executable disappears after fixed SIGTERM"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{
		ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: "process.terminate", Effect: contracts.Write,
		Arguments: map[string]any{"pid": owner.PID}, Target: processTarget, BatchSize: 1, Reversible: false, LabMode: true,
		Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{serviceTarget}, PlanStep: value.Plan[5].Description, PlanTargets: []contracts.TargetRef{processTarget}, AuthorizedRelations: []contracts.TargetAuthorization{{Target: processTarget, RelatedTo: serviceTarget, Relation: "PROCESS_OCCUPIES_REQUIRED_PORT", EvidenceRefs: append([]string(nil), value.EvidenceRefs...)}}},
	}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	if err != nil {
		return waiting, err
	}
	emitEvent(emit, waiting, "端口占用进程已通过双重护栏，等待 L2 人工审批")
	return waiting, nil
}

func (o *Orchestrator) callWorkflowReadStep(ctx context.Context, value task.Task, index int, server, tool string, arguments map[string]any) (task.Task, any, error) {
	if index < 0 || index >= len(value.Plan) {
		return value, nil, errors.New("port workflow step index is invalid")
	}
	value.CurrentStep = index
	value.Plan[index].State = "RUNNING"
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, nil, err
	}
	target := contracts.TargetRef{Type: "host", ID: "local"}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: tool, Effect: contracts.Read, Arguments: arguments, Target: target, BatchSize: 1, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: value.Plan[index].Description, PlanTargets: []contracts.TargetRef{target}}}
	digest, err := proposal.Digest()
	if err != nil {
		return value, nil, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "current_step": value.Plan[index].Description, "selected_tool": tool, "expected_observation": "bounded structured Lab evidence"}); err != nil {
		return value, nil, err
	}
	if err := o.appendTrace(ctx, value, trace.ActionProposed, map[string]any{"proposal_id": proposal.ProposalID, "tool": tool, "effect": proposal.Effect, "target": target, "proposal_digest": digest}); err != nil {
		return value, nil, err
	}
	safety := o.Safety.Evaluate(proposal)
	if err := o.appendTrace(ctx, value, trace.StaticGuardResult, safety.Static); err != nil {
		return value, nil, err
	}
	if err := o.appendTrace(ctx, value, trace.IntentGuardResult, safety.Intent); err != nil {
		return value, nil, err
	}
	if err := o.appendTrace(ctx, value, trace.RiskEvaluated, safety.Risk); err != nil {
		return value, nil, err
	}
	if safety.Final.Outcome != contracts.Allow || safety.Risk.Level != contracts.L0 {
		return value, nil, fmt.Errorf("read step %s was not allowed: %s/%s", tool, safety.Final.Outcome, safety.Risk.Level)
	}
	toolCtx, cancel := context.WithTimeout(ctx, o.timeout())
	defer cancel()
	if err := o.appendTrace(toolCtx, value, trace.ToolCall, map[string]any{"server": server, "tool": tool, "arguments": arguments}); err != nil {
		return value, nil, err
	}
	result, err := o.Registry.CallTool(toolCtx, server, tool, arguments)
	if err != nil {
		return value, nil, err
	}
	if result.IsError {
		return value, nil, fmt.Errorf("tool %s: %s", tool, textResult(result))
	}
	if result.StructuredContent == nil {
		return value, nil, fmt.Errorf("tool %s returned no structured content", tool)
	}
	bounded, err := boundedObservation(result.StructuredContent)
	if err != nil {
		return value, nil, err
	}
	event, err := o.Trace.Append(ctx, value.ID, value.SessionID, trace.ToolResult, map[string]any{"server": server, "tool": tool, "structured_output": json.RawMessage(bounded)})
	if err != nil {
		return value, nil, err
	}
	evidenceRef := fmt.Sprintf("trace://%s/%d", value.ID, event.Sequence)
	value.Plan[index].State = "COMPLETED"
	if !containsString(value.CompletedSteps, value.Plan[index].ID) {
		value.CompletedSteps = append(value.CompletedSteps, value.Plan[index].ID)
	}
	value.EvidenceRefs = append(value.EvidenceRefs, evidenceRef)
	value.Findings = append(value.Findings, fmt.Sprintf("%s 返回结构化证据（%s）", tool, shortDigest(bounded)))
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, nil, err
	}
	if err := o.appendTrace(ctx, value, trace.FindingsUpdated, map[string]any{"finding": value.Findings[len(value.Findings)-1], "evidence_ref": evidenceRef}); err != nil {
		return value, nil, err
	}
	return value, result.StructuredContent, nil
}

func (o *Orchestrator) ContinueAfterAction(ctx context.Context, value task.Task, envelope contracts.ActionEnvelope, result executor.Result) (task.Task, bool, error) {
	switch value.IntentType {
	case "port_conflict_recovery":
		switch envelope.Proposal.Tool {
		case "process.terminate":
			waiting, err := o.preparePortServiceRestart(ctx, value)
			return waiting, true, err
		case "service.restart":
			completed, err := o.verifyPortRecovery(ctx, value)
			return completed, true, err
		default:
			return value, true, fmt.Errorf("unexpected port workflow action %s", envelope.Proposal.Tool)
		}
	case "cpu_hog_recovery":
		if envelope.Proposal.Tool != "process.terminate" {
			return value, true, fmt.Errorf("unexpected CPU workflow action %s", envelope.Proposal.Tool)
		}
		completed, err := o.verifyCPURecovery(ctx, value)
		return completed, true, err
	case "disk_log_recovery":
		switch envelope.Proposal.Tool {
		case "process.terminate":
			waiting, err := o.prepareDiskQuarantine(ctx, value)
			return waiting, true, err
		case "file.quarantine":
			completed, err := o.verifyDiskRecovery(ctx, value, result)
			return completed, true, err
		default:
			return value, true, fmt.Errorf("unexpected disk workflow action %s", envelope.Proposal.Tool)
		}
	default:
		return value, false, nil
	}
}

func (o *Orchestrator) preparePortServiceRestart(ctx context.Context, value task.Task) (task.Task, error) {
	snapshot, err := o.ActionTargets.SnapshotService(ctx, demoServiceUnit, demoServiceUnit)
	if err != nil {
		return value, err
	}
	target := contracts.TargetRef{Type: "service", ID: snapshot.ID}
	value.CurrentStep = 6
	value.Plan[6].State = "WAITING_APPROVAL"
	value.Transition(task.Planning)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	if err := o.appendTrace(ctx, value, trace.DecisionRecorded, map[string]any{"objective": value.Objective, "current_step": value.Plan[6].Description, "evidence_used": value.EvidenceRefs, "selected_tool": "service.restart", "target": target, "expected_observation": "fixed systemctl restart succeeds and systemctl show reports active"}); err != nil {
		return value, err
	}
	proposal := contracts.ActionProposal{ProposalID: id.New("proposal"), TaskID: value.ID, SessionID: value.SessionID, Tool: "service.restart", Effect: contracts.Write, Arguments: map[string]any{"unit": demoServiceUnit}, Target: target, BatchSize: 1, Reversible: false, LabMode: true, Intent: contracts.IntentContext{OriginalRequest: value.OriginalRequest, Objective: value.Objective, ObjectiveTargets: []contracts.TargetRef{target}, PlanStep: value.Plan[6].Description, PlanTargets: []contracts.TargetRef{target}}}
	waiting, _, err := o.Actions.Prepare(ctx, value, proposal, snapshot)
	return waiting, err
}

func (o *Orchestrator) verifyPortRecovery(ctx context.Context, value task.Task) (task.Task, error) {
	value.Transition(task.Verifying)
	if err := o.Store.SaveTask(ctx, value); err != nil {
		return value, err
	}
	verified, serviceOutput, err := o.callWorkflowReadStep(ctx, value, 7, "service", "service.get_status", map[string]any{"unit": demoServiceUnit})
	if err != nil {
		return verified, err
	}
	verified, portOutput, err := o.callWorkflowReadStep(ctx, verified, 8, "network", "network.check_port", map[string]any{"port": demoPort, "protocol": "tcp"})
	if err != nil {
		return verified, err
	}
	var status struct {
		Service platform.ServiceStatus `json:"service"`
	}
	var port struct {
		Occupied bool `json:"occupied"`
	}
	if err := decodeStructured(serviceOutput, &status); err != nil {
		return verified, err
	}
	if err := decodeStructured(portOutput, &port); err != nil {
		return verified, err
	}
	if status.Service.ActiveState != "active" || !port.Occupied {
		return verified, fmt.Errorf("post-recovery verification failed: active=%s port_occupied=%t", status.Service.ActiveState, port.Occupied)
	}
	health := o.Health
	if health == nil {
		health = HTTPHealthVerifier{}
	}
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	healthEvidence, err := health.Verify(healthCtx, demoHealthURL)
	cancel()
	if err != nil {
		return verified, fmt.Errorf("verify Demo HTTP health: %w", err)
	}
	healthEvent, err := o.Trace.Append(ctx, verified.ID, verified.SessionID, trace.VerificationResult, map[string]any{"verified": true, "source": "fixed_loopback_http", "evidence": healthEvidence})
	if err != nil {
		return verified, err
	}
	verified.CurrentStep = 9
	verified.Plan[9].State = "COMPLETED"
	if !containsString(verified.CompletedSteps, verified.Plan[9].ID) {
		verified.CompletedSteps = append(verified.CompletedSteps, verified.Plan[9].ID)
	}
	verified.EvidenceRefs = append(verified.EvidenceRefs, fmt.Sprintf("trace://%s/%d", verified.ID, healthEvent.Sequence))
	verified.Findings = append(verified.Findings, "Demo Web 服务 active、端口重新监听且 loopback healthz 返回 HTTP 200")
	answer := "已确认 Web 服务因端口被受控 Lab 进程占用而启动失败。两个写动作分别通过本地护栏、人工审批和目标重验证：占用进程已用固定 SIGTERM 终止，Demo Web 服务已重启；服务状态、端口监听和 loopback HTTP healthz 均验证通过。"
	verified.Transition(task.Completed)
	if err := o.Store.SaveTask(ctx, verified); err != nil {
		return verified, err
	}
	if _, err := o.Store.UpdateSession(ctx, verified.SessionID, func(s *session.Session) error {
		s.Messages = append(s.Messages, session.Message{ID: id.New("msg"), Role: session.RoleAssistant, Content: answer, TaskID: verified.ID, CreatedAt: time.Now().UTC()})
		s.Summary = "端口冲突已完成多源诊断、逐动作审批、恢复和三重验证"
		s.UpdatedAt = time.Now().UTC()
		return nil
	}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.TaskCompleted, map[string]any{"completion_criteria_met": true, "service_active": true, "port_listening": true, "http_status": healthEvidence.StatusCode}); err != nil {
		return verified, err
	}
	if err := o.appendTrace(ctx, verified, trace.Final, map[string]any{"answer": answer, "evidence_refs": verified.EvidenceRefs}); err != nil {
		return verified, err
	}
	return verified, nil
}

func decodeStructured(value any, destination any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, destination)
}

func uniqueDemoHolder(values []platform.ProcessInfo) (platform.ProcessInfo, error) {
	matches := make([]platform.ProcessInfo, 0, 1)
	for _, value := range values {
		if value.Executable == demoHolderExecutable {
			matches = append(matches, value)
		}
	}
	if len(matches) != 1 {
		return platform.ProcessInfo{}, fmt.Errorf("expected exactly one allowlisted Demo port holder, got %d", len(matches))
	}
	return matches[0], nil
}

func knowledgeRefs(values []retrieval.Result) []string {
	refs := make([]string, 0, len(values))
	for _, value := range values {
		if value.DocumentID != "" {
			refs = append(refs, "knowledge://"+value.DocumentID)
		}
	}
	return refs
}
