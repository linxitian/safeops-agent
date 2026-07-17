package llm

import (
	"context"
	"encoding/json"
)

type DecisionKind string

const (
	DecisionTool   DecisionKind = "tool"
	DecisionFinal  DecisionKind = "final"
	DecisionReplan DecisionKind = "replan"
)

type ToolCapability struct {
	ServerID    string          `json:"server_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Observation struct {
	Tool        string          `json:"tool"`
	Arguments   json.RawMessage `json:"arguments"`
	Result      json.RawMessage `json:"result"`
	EvidenceRef string          `json:"evidence_ref"`
}

type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SessionContext contains only bounded, operator-visible durable context. It
// must never contain hidden model reasoning or privileged executor state.
type SessionContext struct {
	Summary           string           `json:"summary,omitempty"`
	RecentMessages    []SessionMessage `json:"recent_messages,omitempty"`
	SelectedResources []string         `json:"selected_resources,omitempty"`
}

type DecisionRequest struct {
	Objective       string           `json:"objective"`
	OriginalRequest string           `json:"original_request"`
	SessionContext  *SessionContext  `json:"session_context,omitempty"`
	Tools           []ToolCapability `json:"tools"`
	Observations    []Observation    `json:"observations"`
	Iteration       int              `json:"iteration"`
	ToolCalls       int              `json:"tool_calls"`
}

type Decision struct {
	Kind                DecisionKind   `json:"kind"`
	DecisionSummary     string         `json:"decision_summary"`
	ServerID            string         `json:"server_id,omitempty"`
	Tool                string         `json:"tool,omitempty"`
	Arguments           map[string]any `json:"arguments,omitempty"`
	ExpectedObservation string         `json:"expected_observation,omitempty"`
	FinalAnswer         string         `json:"final_answer,omitempty"`
}

type Provider interface {
	Decide(context.Context, DecisionRequest) (Decision, error)
}
