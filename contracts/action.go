package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

type Effect string

const (
	Read  Effect = "read"
	Write Effect = "write"
)

type RiskLevel string

const (
	L0 RiskLevel = "L0"
	L1 RiskLevel = "L1"
	L2 RiskLevel = "L2"
	L3 RiskLevel = "L3"
)

type Outcome string

const (
	Allow           Outcome = "ALLOW"
	Deny            Outcome = "DENY"
	RequireApproval Outcome = "REQUIRE_APPROVAL"
)

type TargetRef struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	Criticality string `json:"criticality,omitempty"`
}
type TargetAuthorization struct {
	Target       TargetRef `json:"target"`
	RelatedTo    TargetRef `json:"related_to"`
	Relation     string    `json:"relation"`
	EvidenceRefs []string  `json:"evidence_refs"`
}
type IntentContext struct {
	OriginalRequest     string                `json:"original_request"`
	Objective           string                `json:"objective"`
	ObjectiveTargets    []TargetRef           `json:"objective_targets"`
	PlanStep            string                `json:"plan_step"`
	PlanTargets         []TargetRef           `json:"plan_targets"`
	AuthorizedRelations []TargetAuthorization `json:"authorized_relations,omitempty"`
}
type ActionProposal struct {
	ProposalID       string         `json:"proposal_id"`
	TaskID           string         `json:"task_id"`
	SessionID        string         `json:"session_id"`
	Tool             string         `json:"tool"`
	Effect           Effect         `json:"effect"`
	Arguments        map[string]any `json:"arguments"`
	Target           TargetRef      `json:"target"`
	BatchSize        int            `json:"batch_size"`
	Reversible       bool           `json:"reversible"`
	RollbackStrategy string         `json:"rollback_strategy,omitempty"`
	LabMode          bool           `json:"lab_mode"`
	SystemState      string         `json:"system_state,omitempty"`
	Intent           IntentContext  `json:"intent"`
}
type RiskResult struct {
	Level   RiskLevel `json:"risk_level"`
	Score   int       `json:"risk_score"`
	Factors []string  `json:"risk_factors"`
	Reason  string    `json:"risk_reason"`
}
type GuardDecision struct {
	DecisionID     string     `json:"decision_id"`
	Outcome        Outcome    `json:"outcome"`
	ReasonCodes    []string   `json:"reason_codes"`
	Reason         string     `json:"reason"`
	MatchedRuleIDs []string   `json:"matched_rule_ids"`
	PolicyVersion  string     `json:"policy_version"`
	Risk           RiskResult `json:"risk"`
}

func (p ActionProposal) Digest() (string, error) {
	if p.Tool == "" {
		return "", errors.New("tool is required")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func CanonicalTarget(target TargetRef) string {
	return strings.ToLower(strings.TrimSpace(target.Type)) + ":" + strings.ToLower(strings.TrimSpace(target.ID))
}
