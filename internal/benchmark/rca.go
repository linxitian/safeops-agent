package benchmark

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
)

type rcaFixture struct {
	id            string
	category      string
	input         rca.PortConflictInput
	expectedLevel rca.Level
	expectedTerm  string
}

func rcaFixtures() []rcaFixture {
	service := platform.ServiceStatus{Name: "safeops-demo-web.service", ActiveState: "failed"}
	logEvent := platform.JournalEvent{Message: "listen tcp 127.0.0.1:18080: bind: address already in use", Priority: 3}
	socket := platform.SocketInfo{LocalPort: 18080, Listening: true, Inode: "9001"}
	process := platform.ProcessInfo{PID: 4242, StartTicks: 9911, Name: "safeops-port-holder", UID: 1000}
	return []rcaFixture{
		{id: "rca_complete_conflict", category: "complete multi-source evidence", input: rca.PortConflictInput{Service: service, Port: 18080, Logs: []platform.JournalEvent{logEvent}, Sockets: []platform.SocketInfo{socket}, Processes: []platform.ProcessInfo{process}}, expectedLevel: rca.D1, expectedTerm: "已被其他进程占用"},
		{id: "rca_log_only", category: "missing live signal", input: rca.PortConflictInput{Service: service, Port: 18080, Logs: []platform.JournalEvent{logEvent}}, expectedLevel: rca.D2, expectedTerm: "冲突是主要候选原因"},
		{id: "rca_socket_only", category: "missing log and identity", input: rca.PortConflictInput{Service: service, Port: 18080, Sockets: []platform.SocketInfo{socket}}, expectedLevel: rca.D2, expectedTerm: "冲突是主要候选原因"},
		{id: "rca_no_supporting_evidence", category: "ambiguous request", input: rca.PortConflictInput{Service: service, Port: 18080}, expectedLevel: rca.D3, expectedTerm: "原因未知"},
		{id: "rca_knowledge_augmented", category: "knowledge similarity", input: rca.PortConflictInput{Service: service, Port: 18080, Logs: []platform.JournalEvent{logEvent}, Sockets: []platform.SocketInfo{socket}, Processes: []platform.ProcessInfo{process}, CaseSimilarity: .8}, expectedLevel: rca.D1, expectedTerm: "已被其他进程占用"},
	}
}

func runRCA(report *Report) {
	started := time.Now()
	suite := Suite{Name: "rca"}
	top1, top3 := 0, 0
	fixtures := rcaFixtures()
	for _, fixture := range fixtures {
		caseStarted := time.Now()
		result := rca.DiagnosePortConflict(fixture.input).RCA
		levelCorrect := result.DiagnosisLevel == fixture.expectedLevel
		firstCorrect := len(result.CandidateCauses) > 0 && strings.Contains(result.CandidateCauses[0].Cause, fixture.expectedTerm)
		inTop3 := false
		for index, candidate := range result.CandidateCauses {
			if index >= 3 {
				break
			}
			if strings.Contains(candidate.Cause, fixture.expectedTerm) {
				inTop3 = true
				break
			}
		}
		if levelCorrect && firstCorrect {
			top1++
		}
		if levelCorrect && inTop3 {
			top3++
		}
		passed := levelCorrect && firstCorrect && inTop3
		suite.Cases = append(suite.Cases, Case{ID: fixture.id, Category: fixture.category, Passed: passed, Details: fmt.Sprintf("expected_level=%s actual_level=%s candidates=%d confidence=%.3f missing=%v", fixture.expectedLevel, result.DiagnosisLevel, len(result.CandidateCauses), result.Confidence, result.MissingEvidence), Duration: time.Since(caseStarted)})
	}
	report.setRate("RCA Top-1 Accuracy", top1, len(fixtures), "exact diagnosis-level match and expected cause in the first production RCA candidate")
	report.setRate("RCA Top-3 Accuracy", top3, len(fixtures), "exact diagnosis-level match and expected cause among the first three production RCA candidates")
	suite.finish(started)
	report.addSuite(suite)
}

func runPerformance(report *Report) {
	started := time.Now()
	suite := Suite{Name: "performance"}
	fixture := rcaFixtures()[0]
	const iterations = 2000
	durations := make([]time.Duration, 0, iterations)
	confidenceSum := 0.0
	for index := 0; index < iterations; index++ {
		caseStarted := time.Now()
		result := rca.DiagnosePortConflict(fixture.input).RCA
		durations = append(durations, time.Since(caseStarted))
		confidenceSum += result.Confidence
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	total := time.Duration(0)
	for _, duration := range durations {
		total += duration
	}
	mean := float64(total) / float64(iterations) / float64(time.Millisecond)
	p95Index := (iterations*95+99)/100 - 1
	p95 := float64(durations[p95Index]) / float64(time.Millisecond)
	passed := len(durations) == iterations && confidenceSum > 0
	suite.Cases = append(suite.Cases, Case{ID: "performance_port_conflict_rca", Category: "native production RCA latency", Passed: passed, Details: fmt.Sprintf("iterations=%d mean_ms=%.6f p95_ms=%.6f", iterations, mean, p95), Duration: time.Since(started)})
	report.setDuration("Mean Diagnosis Latency", mean, fmt.Sprintf("arithmetic mean of %d native DiagnosePortConflict calls", iterations))
	report.setDuration("P95 Diagnosis Latency", p95, fmt.Sprintf("nearest-rank p95 of %d native DiagnosePortConflict calls", iterations))
	suite.finish(started)
	report.addSuite(suite)
}
