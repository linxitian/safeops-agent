package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"safeops-agent/contracts"
	"safeops-agent/internal/agent"
	"safeops-agent/internal/approval"
	sessioncontext "safeops-agent/internal/context"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/guard"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/rollback"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

func runContinuity(ctx context.Context, report *Report, policyPath string) {
	started := time.Now()
	suite := Suite{Name: "continuity"}
	contextCorrect, contextTotal := exerciseContextCases(&suite)
	report.setRate("Context Reference Resolution Accuracy", contextCorrect, contextTotal, "exact resource or expected ambiguity rejection from the production session context resolver")

	catalog, err := guard.LoadCatalog(policyPath)
	if err != nil {
		suite.Error = err.Error()
		suite.finish(started)
		report.addSuite(suite)
		return
	}
	resumeResults, err := exerciseApprovalResume(ctx, guard.NewSafetyPipeline(catalog))
	if err != nil {
		suite.Error = err.Error()
	} else {
		suite.Cases = append(suite.Cases, resumeResults.cases...)
		report.setRate("Approval Resume Success Rate", resumeResults.resumed, resumeResults.total, "approved persisted tasks completed through the production ApprovalResumer and Executor validator")
		report.setRate("Server Restart Recovery Rate", resumeResults.restartRecovered, resumeResults.restartTotal, "approved task recovered after reconstructing FileStore, ApprovalStore, Trace Writer, nonce store, and resumer from disk")
		report.setRate("Trace Coverage", resumeResults.traceEventsFound, resumeResults.traceEventsExpected, "required approval/execution/verification/completion event types present per resumed task")
		report.setRate("Trace Integrity Verification Rate", resumeResults.integrityVerified, resumeResults.total, "hash-chain VerifyIntegrity success for every resumed task")
	}
	rollbackCases, rollbackSuccesses, rollbackErr := exerciseRollback(ctx)
	suite.Cases = append(suite.Cases, rollbackCases...)
	if rollbackErr != nil {
		if suite.Error == "" {
			suite.Error = rollbackErr.Error()
		} else {
			suite.Error += "; " + rollbackErr.Error()
		}
	} else {
		report.setRate("Rollback Success Rate", rollbackSuccesses, len(rollbackCases), "real atomic quarantine and restore cycles on scoped temporary files")
	}
	suite.finish(started)
	report.addSuite(suite)
}

func exerciseContextCases(suite *Suite) (int, int) {
	fixtures := []struct {
		id        string
		category  string
		request   string
		resources []string
		expected  string
		wantError bool
	}{
		{id: "context_chinese_ordinal", category: "context reference", request: "把第三个恢复回来", resources: []string{"a", "b", "c"}, expected: "c"},
		{id: "context_arabic_ordinal", category: "context reference", request: "处理第 2 项", resources: []string{"a", "b", "c"}, expected: "b"},
		{id: "context_last", category: "context reference", request: "最后一个先隔离", resources: []string{"a", "b", "c"}, expected: "c"},
		{id: "context_single_pronoun", category: "context reference", request: "把它恢复", resources: []string{"only"}, expected: "only"},
		{id: "context_ambiguous", category: "ambiguous request", request: "把刚才那个处理掉", resources: []string{"a", "b"}, wantError: true},
		{id: "context_out_of_range", category: "ambiguous request", request: "处理第9个", resources: []string{"a", "b"}, wantError: true},
	}
	correct := 0
	for _, fixture := range fixtures {
		started := time.Now()
		actual, _, err := sessioncontext.ResolveResource(fixture.request, fixture.resources)
		passed := (fixture.wantError && err != nil) || (!fixture.wantError && err == nil && actual == fixture.expected)
		if passed {
			correct++
		}
		details := fmt.Sprintf("expected=%q actual=%q want_error=%t", fixture.expected, actual, fixture.wantError)
		if err != nil {
			details += " error=" + err.Error()
		}
		suite.Cases = append(suite.Cases, Case{ID: fixture.id, Category: fixture.category, Passed: passed, Details: details, Duration: time.Since(started)})
	}
	return correct, len(fixtures)
}

type resumeMeasurements struct {
	cases                                 []Case
	resumed, total                        int
	restartRecovered, restartTotal        int
	traceEventsFound, traceEventsExpected int
	integrityVerified                     int
}

func exerciseApprovalResume(ctx context.Context, pipeline guard.SafetyPipeline) (resumeMeasurements, error) {
	measurements := resumeMeasurements{}
	for index, reconstruct := range []bool{false, true} {
		caseStarted := time.Now()
		root, err := os.MkdirTemp("", "safeops-bench-resume-*")
		if err != nil {
			return measurements, err
		}
		taskID := fmt.Sprintf("bench_resume_task_%d", index)
		nonce := fmt.Sprintf("bench-resume-nonce-%d", index)
		store, approvalStore, traceWriter, record, err := createPendingResumeFixture(ctx, root, pipeline, taskID, nonce)
		if err != nil {
			os.RemoveAll(root)
			return measurements, err
		}
		if reconstruct {
			store, err = storage.NewFileStore(root)
			if err != nil {
				os.RemoveAll(root)
				return measurements, err
			}
			approvalStore, err = approval.NewStore(filepath.Join(root, "approvals"))
			if err != nil {
				os.RemoveAll(root)
				return measurements, err
			}
			traceWriter, err = trace.NewWriter(filepath.Join(root, "traces"))
			if err != nil {
				os.RemoveAll(root)
				return measurements, err
			}
			record, err = approvalStore.Get(ctx, record.ID)
			if err != nil {
				os.RemoveAll(root)
				return measurements, err
			}
			measurements.restartTotal++
		}
		nonces, err := executor.NewNonceStore(filepath.Join(root, "used-nonces.json"))
		if err != nil {
			os.RemoveAll(root)
			return measurements, err
		}
		engine := executor.Executor{Validator: executor.Validator{Secret: benchmarkSecret(), Pipeline: pipeline, Nonces: nonces, Approvals: approvalStore, Scope: allowBenchmarkScope{}, Targets: stableBenchmarkTarget{}}, Handlers: map[string]executor.Handler{"service.restart": executor.DryRunHandler{}}}
		resumer := agent.ApprovalResumer{Store: store, Executor: engine, Trace: traceWriter}
		completed, resumeErr := resumer.Resume(ctx, record)
		passed := resumeErr == nil && completed.State == task.Completed
		measurements.total++
		if passed {
			measurements.resumed++
			if reconstruct {
				measurements.restartRecovered++
			}
		}
		details := fmt.Sprintf("reconstructed=%t state=%s", reconstruct, completed.State)
		if resumeErr != nil {
			details += " error=" + resumeErr.Error()
		}
		measurements.cases = append(measurements.cases, Case{ID: fmt.Sprintf("approval_resume_%d", index), Category: map[bool]string{false: "approval resume", true: "server restart recovery"}[reconstruct], Passed: passed, Details: details, Duration: time.Since(caseStarted)})
		required := map[trace.Type]int{trace.ApprovalResult: 1, trace.Execution: 2, trace.Verification: 1, trace.TaskCompleted: 1, trace.Final: 1}
		measurements.traceEventsExpected += 6
		events, readErr := traceWriter.Read(taskID)
		if readErr == nil {
			present := map[trace.Type]int{}
			for _, event := range events {
				present[event.Type]++
			}
			for eventType, wanted := range required {
				measurements.traceEventsFound += min(present[eventType], wanted)
			}
		}
		if traceWriter.VerifyIntegrity(taskID) == nil {
			measurements.integrityVerified++
		}
		os.RemoveAll(root)
	}
	return measurements, nil
}

func createPendingResumeFixture(ctx context.Context, root string, pipeline guard.SafetyPipeline, taskID, nonce string) (*storage.FileStore, *approval.Store, *trace.Writer, approval.Record, error) {
	store, err := storage.NewFileStore(root)
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	approvalStore, err := approval.NewStore(filepath.Join(root, "approvals"))
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	traceWriter, err := trace.NewWriter(filepath.Join(root, "traces"))
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	now := time.Now().UTC()
	sessionID := "bench_resume_session"
	if err := store.SaveSession(ctx, session.Session{ID: sessionID, Name: "resume benchmark", PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	envelope, err := benchmarkEnvelope(pipeline, benchmarkSecret(), taskID, nonce)
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	envelope.SessionID = sessionID
	envelope.Proposal.SessionID = sessionID
	envelope.ProposalDigest, err = envelope.Proposal.Digest()
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	safety := pipeline.Evaluate(envelope.Proposal)
	envelope.IntentDigest, envelope.Risk = safety.Intent.IntentDigest, safety.Risk
	if err := envelope.Sign(benchmarkSecret()); err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	if err := approveBenchmarkEnvelope(ctx, approvalStore, benchmarkSecret(), &envelope); err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	record, err := approvalStore.Get(ctx, envelope.ApprovalID)
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	pending, err := contracts.MarshalEnvelope(envelope)
	if err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	value := task.Task{ID: taskID, SessionID: sessionID, Objective: "resume approved action", OriginalRequest: "恢复 Web 服务", State: task.WaitingApproval, IntentType: "service_recovery", Plan: []task.Step{{ID: "restart", Description: "restart demo service", Tool: "service.restart", State: "WAITING_APPROVAL"}}, PendingAction: pending, PendingApprovalID: record.ID, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveTask(ctx, value); err != nil {
		return nil, nil, nil, approval.Record{}, err
	}
	return store, approvalStore, traceWriter, record, nil
}

func benchmarkSecret() []byte {
	return []byte("safeops-benchmark-hmac-secret-32-bytes-minimum")
}

func exerciseRollback(ctx context.Context) ([]Case, int, error) {
	root, err := os.MkdirTemp("", "safeops-bench-rollback-*")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(root)
	labRoot, quarantineRoot := filepath.Join(root, "lab"), filepath.Join(root, "quarantine")
	if err := os.MkdirAll(labRoot, 0o750); err != nil {
		return nil, 0, err
	}
	manager, err := rollback.NewQuarantineManager([]string{labRoot}, quarantineRoot)
	if err != nil {
		return nil, 0, err
	}
	targets := executor.LinuxTargets{Linux: platform.NewLinux(), Commands: platform.NewCommandPlatform(), AllowedFileRoots: []string{labRoot, quarantineRoot}}
	cases := make([]Case, 0, 3)
	successes := 0
	for index := 0; index < 3; index++ {
		caseStarted := time.Now()
		path := filepath.Join(labRoot, fmt.Sprintf("sample-%d.log", index))
		content := []byte(fmt.Sprintf("safeops rollback fixture %d", index))
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return cases, successes, err
		}
		snapshot, err := targets.SnapshotFile(ctx, path, path)
		if err != nil {
			return cases, successes, err
		}
		operation, err := manager.Quarantine(ctx, fmt.Sprintf("rollback-task-%d", index), fmt.Sprintf("rollback-nonce-%d", index), snapshot)
		if err != nil {
			return cases, successes, err
		}
		quarantinedSnapshot, err := targets.SnapshotFile(ctx, operation.Manifest.QuarantinedPath, operation.Manifest.QuarantinedPath)
		if err != nil {
			return cases, successes, err
		}
		restored, err := manager.Restore(ctx, operation.Manifest.ID, quarantinedSnapshot)
		actual, readErr := os.ReadFile(path)
		passed := err == nil && readErr == nil && string(actual) == string(content) && restored.Manifest.Status == rollback.Restored
		if passed {
			successes++
		}
		details := fmt.Sprintf("quarantine_id=%s status=%s", operation.Manifest.ID, restored.Manifest.Status)
		if err != nil {
			details += " restore_error=" + err.Error()
		}
		if readErr != nil {
			details += " read_error=" + readErr.Error()
		}
		cases = append(cases, Case{ID: fmt.Sprintf("rollback_cycle_%d", index), Category: "real scoped file rollback", Passed: passed, Details: details, Duration: time.Since(caseStarted)})
	}
	return cases, successes, nil
}
