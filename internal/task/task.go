package task

import (
	"encoding/json"
	"time"
)

type State string

const (
	New             State = "NEW"
	Investigating   State = "INVESTIGATING"
	Planning        State = "PLANNING"
	Executing       State = "EXECUTING"
	WaitingApproval State = "WAITING_APPROVAL"
	Verifying       State = "VERIFYING"
	Replanning      State = "REPLANNING"
	Completed       State = "COMPLETED"
	Failed          State = "FAILED"
	Cancelled       State = "CANCELLED"
)

type Step struct {
	ID          string `json:"step_id"`
	Description string `json:"description"`
	Tool        string `json:"tool,omitempty"`
	State       string `json:"state"`
}

type RuntimeObservation struct {
	ServerID    string          `json:"server_id"`
	Tool        string          `json:"tool"`
	Arguments   json.RawMessage `json:"arguments"`
	Result      json.RawMessage `json:"result"`
	EvidenceRef string          `json:"evidence_ref"`
	Digest      string          `json:"digest"`
}

type RuntimeGuardFeedback struct {
	Code            string   `json:"code"`
	Summary         string   `json:"summary"`
	Tool            string   `json:"tool"`
	AttemptedPath   string   `json:"attempted_path,omitempty"`
	AuthorizedPaths []string `json:"authorized_paths,omitempty"`
	ExcludedPaths   []string `json:"excluded_paths,omitempty"`
}

type RuntimeCheckpoint struct {
	Iterations         int                    `json:"iterations"`
	ToolCalls          int                    `json:"tool_calls"`
	Replans            int                    `json:"replans"`
	NoProgressRepeats  int                    `json:"no_progress_repeats"`
	LastProgressDigest string                 `json:"last_progress_digest,omitempty"`
	MaxIterations      int                    `json:"max_iterations"`
	MaxToolCalls       int                    `json:"max_tool_calls"`
	StartedAt          time.Time              `json:"started_at"`
	DeadlineAt         time.Time              `json:"deadline_at"`
	Observations       []RuntimeObservation   `json:"observations"`
	GuardFeedback      []RuntimeGuardFeedback `json:"guard_feedback,omitempty"`
}

type WorkerLease struct {
	Owner     string    `json:"owner"`
	Token     string    `json:"token"`
	Fence     uint64    `json:"fence"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Task struct {
	ID                string                     `json:"task_id"`
	SessionID         string                     `json:"session_id"`
	Objective         string                     `json:"objective"`
	OriginalRequest   string                     `json:"original_request"`
	State             State                      `json:"state"`
	IntentType        string                     `json:"intent_type"`
	Plan              []Step                     `json:"plan"`
	CurrentStep       int                        `json:"current_step"`
	CompletedSteps    []string                   `json:"completed_steps"`
	Findings          []string                   `json:"findings"`
	SelectedResources []string                   `json:"selected_resources"`
	EvidenceRefs      []string                   `json:"evidence_refs"`
	PendingAction     json.RawMessage            `json:"pending_action,omitempty"`
	PendingApprovalID string                     `json:"pending_approval_id,omitempty"`
	VerificationGoal  string                     `json:"verification_goal,omitempty"`
	WorkflowData      map[string]json.RawMessage `json:"workflow_data,omitempty"`
	Runtime           RuntimeCheckpoint          `json:"runtime"`
	WorkerLease       WorkerLease                `json:"worker_lease,omitempty"`
	Checkpoint        int                        `json:"checkpoint"`
	FailureReason     string                     `json:"failure_reason,omitempty"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

func (t *Task) Transition(state State) {
	t.State = state
	t.Checkpoint++
	t.UpdatedAt = time.Now().UTC()
}
