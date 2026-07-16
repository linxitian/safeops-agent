package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"safeops-agent/contracts"
	"safeops-agent/internal/id"
)

type IntentGuard struct{ PolicyVersion string }
type IntentDecision struct {
	DecisionID    string            `json:"decision_id"`
	Outcome       contracts.Outcome `json:"outcome"`
	ReasonCode    string            `json:"reason_code"`
	Reason        string            `json:"reason"`
	IntentDigest  string            `json:"intent_digest"`
	EvidenceRefs  []string          `json:"evidence_refs,omitempty"`
	PolicyVersion string            `json:"policy_version"`
}

func (g IntentGuard) Evaluate(proposal contracts.ActionProposal) IntentDecision {
	decision := IntentDecision{DecisionID: id.New("decision"), PolicyVersion: g.PolicyVersion, IntentDigest: intentDigest(proposal.Intent)}
	target := contracts.CanonicalTarget(proposal.Target)
	if target == ":" {
		decision.Outcome = contracts.Deny
		decision.ReasonCode = "INTENT_TARGET_MISSING"
		decision.Reason = "Action target is missing"
		return decision
	}
	if !containsTarget(proposal.Intent.PlanTargets, target) {
		decision.Outcome = contracts.Deny
		decision.ReasonCode = "INTENT_PLAN_TARGET_MISMATCH"
		decision.Reason = "Action target is not authorized by the current plan step"
		return decision
	}
	if containsTarget(proposal.Intent.ObjectiveTargets, target) {
		decision.Outcome = contracts.Allow
		decision.ReasonCode = "INTENT_DIRECT_TARGET"
		decision.Reason = "Action target directly matches the structured objective target"
		return decision
	}
	for _, authorization := range proposal.Intent.AuthorizedRelations {
		if contracts.CanonicalTarget(authorization.Target) == target && containsTarget(proposal.Intent.ObjectiveTargets, contracts.CanonicalTarget(authorization.RelatedTo)) && authorization.Relation != "" && len(authorization.EvidenceRefs) > 0 {
			decision.Outcome = contracts.Allow
			decision.ReasonCode = "INTENT_EVIDENCE_RELATED_TARGET"
			decision.Reason = "Action target is linked to an objective target by explicit evidence"
			decision.EvidenceRefs = append([]string(nil), authorization.EvidenceRefs...)
			return decision
		}
	}
	decision.Outcome = contracts.Deny
	decision.ReasonCode = "INTENT_TARGET_MISMATCH"
	decision.Reason = "Action target is neither an objective target nor an evidence-authorized related resource"
	return decision
}
func containsTarget(targets []contracts.TargetRef, canonical string) bool {
	for _, target := range targets {
		if contracts.CanonicalTarget(target) == canonical {
			return true
		}
	}
	return false
}
func intentDigest(intent contracts.IntentContext) string {
	b, _ := json.Marshal(intent)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
