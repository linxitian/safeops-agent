package rca

import (
	"fmt"
	"safeops-agent/internal/evidence"
	"safeops-agent/internal/platform"
	"strconv"
	"strings"
)

type PortConflictInput struct {
	Service        platform.ServiceStatus
	Port           int
	Logs           []platform.JournalEvent
	Sockets        []platform.SocketInfo
	Processes      []platform.ProcessInfo
	CaseSimilarity float64
}
type PortConflictOutput struct {
	RCA   Result            `json:"rca"`
	Graph evidence.Snapshot `json:"evidence_graph"`
}

func DiagnosePortConflict(in PortConflictInput) PortConflictOutput {
	graph := evidence.New()
	serviceID := "service:" + in.Service.Name
	portID := "port:" + strconv.Itoa(in.Port)
	_ = graph.Upsert(evidence.Node{ID: serviceID, Type: evidence.Service, Label: in.Service.Name, Attributes: map[string]any{"active_state": in.Service.ActiveState, "main_pid": in.Service.MainPID}, EvidenceRefs: []string{"service-status"}})
	_ = graph.Upsert(evidence.Node{ID: portID, Type: evidence.Port, Label: strconv.Itoa(in.Port), EvidenceRefs: []string{"network-port"}})
	hasPattern := false
	refs := []string{"service-status"}
	for index, event := range in.Logs {
		message := strings.ToLower(event.Message)
		if strings.Contains(message, "eaddrinuse") || strings.Contains(message, "address already in use") {
			hasPattern = true
			id := fmt.Sprintf("log:%d", index)
			ref := fmt.Sprintf("journal:%d", index)
			_ = graph.Upsert(evidence.Node{ID: id, Type: evidence.LogEvent, Label: event.Message, EvidenceRefs: []string{ref}})
			_ = graph.Link(evidence.Edge{From: serviceID, To: id, Relation: "SERVICE_EMITS_LOG", EvidenceRefs: []string{ref}})
			refs = append(refs, ref)
		}
	}
	occupied := false
	for _, socket := range in.Sockets {
		if socket.LocalPort == in.Port && socket.Listening {
			occupied = true
			refs = append(refs, "network-port")
			break
		}
	}
	culprit := ""
	if len(in.Processes) > 0 {
		process := in.Processes[0]
		culprit = fmt.Sprintf("pid:%d:start:%d", process.PID, process.StartTicks)
		_ = graph.Upsert(evidence.Node{ID: culprit, Type: evidence.Process, Label: process.Name, Attributes: map[string]any{"pid": process.PID, "start_ticks": process.StartTicks, "uid": process.UID}, EvidenceRefs: []string{"process-port-owner"}})
		_ = graph.Link(evidence.Edge{From: culprit, To: portID, Relation: "PROCESS_LISTENS_PORT", EvidenceRefs: []string{"process-port-owner", "network-port"}})
		refs = append(refs, "process-port-owner")
	}
	components := ConfidenceComponents{CaseSimilarity: in.CaseSimilarity}
	if occupied {
		components.SignalMatch = 1
	}
	if hasPattern {
		components.LogPatternMatch = 1
	}
	if occupied && culprit != "" {
		components.GraphConsistency = 1
	}
	result := Result{DiagnosisLevel: D3, ConfidenceComponents: components, Confidence: components.Score(), EvidenceRefs: unique(refs), Remediation: []string{"确认端口占用进程是否属于授权 Lab 目标", "通过 Guard、审批和目标重验证后处置占用进程", "重启目标服务并验证端口与 HTTP"}}
	switch {
	case occupied && hasPattern && culprit != "":
		result.DiagnosisLevel = D1
		result.RootCause = fmt.Sprintf("端口 %d 已被其他进程占用，导致 %s 启动时出现 EADDRINUSE", in.Port, in.Service.Name)
		result.Culprit = culprit
		result.CandidateCauses = []CandidateCause{{Cause: result.RootCause, Score: result.Confidence, EvidenceRefs: append([]string(nil), result.EvidenceRefs...)}}
	case occupied || hasPattern:
		result.DiagnosisLevel = D2
		cause := fmt.Sprintf("端口 %d 冲突是主要候选原因，但证据链尚不完整", in.Port)
		result.CandidateCauses = []CandidateCause{{Cause: cause, Score: result.Confidence, EvidenceRefs: append([]string(nil), result.EvidenceRefs...)}}
		if !occupied {
			result.MissingEvidence = append(result.MissingEvidence, "当前端口监听证据")
		}
		if !hasPattern {
			result.MissingEvidence = append(result.MissingEvidence, "EADDRINUSE 日志模式")
		}
		if culprit == "" {
			result.MissingEvidence = append(result.MissingEvidence, "端口占用进程身份")
		}
	default:
		result.CandidateCauses = []CandidateCause{{Cause: "服务启动失败原因未知", Score: result.Confidence, EvidenceRefs: append([]string(nil), result.EvidenceRefs...)}}
		result.MissingEvidence = []string{"EADDRINUSE 日志模式", "当前端口监听证据", "端口占用进程身份"}
	}
	return PortConflictOutput{RCA: result, Graph: graph.Snapshot()}
}
func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
