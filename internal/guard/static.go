package guard

import (
	"safeops-agent/contracts"
	"safeops-agent/internal/id"
	"strings"
)

type StaticGuard struct {
	Catalog *Catalog
	Risk    RiskEngine
}

func NewStaticGuard(catalog *Catalog) StaticGuard {
	return StaticGuard{Catalog: catalog, Risk: RiskEngine{Catalog: catalog}}
}
func (g StaticGuard) Evaluate(proposal contracts.ActionProposal) contracts.GuardDecision {
	decision := g.Precheck(proposal)
	if decision.Outcome == contracts.Deny {
		decision.Risk = g.Risk.Evaluate(proposal)
		return decision
	}
	risk := g.Risk.Evaluate(proposal)
	decision.Risk = risk
	policy, _ := g.Catalog.Policy(proposal.Tool)
	if policy.Effect == contracts.Read {
		return decision
	}
	if risk.Level == contracts.L3 {
		return deny(decision, "L3_DEFAULT_DENY", policy.RuleID, "L3 关键操作默认拒绝")
	}
	if policy.Approval || risk.Level == contracts.L2 {
		decision.Outcome = contracts.RequireApproval
		decision.ReasonCodes = []string{"WRITE_REQUIRES_APPROVAL"}
		decision.Reason = "本地策略或上下文风险要求人工审批"
		return decision
	}
	return decision
}

// Precheck performs static, context-independent policy checks. Context risk is
// deliberately evaluated later by SafetyPipeline after Intent Guard.
func (g StaticGuard) Precheck(proposal contracts.ActionProposal) contracts.GuardDecision {
	decision := contracts.GuardDecision{DecisionID: id.New("decision"), PolicyVersion: g.Catalog.VersionID()}
	policy, known := g.Catalog.Policy(proposal.Tool)
	if forbiddenCapability(proposal.Tool) {
		return deny(decision, "FORBIDDEN_CAPABILITY", "forbidden-capability", "禁止任意 Shell 或命令执行能力")
	}
	if !known {
		return deny(decision, "UNKNOWN_TOOL", "unknown-tool-deny", "本地安全策略没有该 Tool，默认拒绝")
	}
	if proposal.Effect != policy.Effect {
		return deny(decision, "EFFECT_MISMATCH", policy.RuleID, "Action Proposal effect 与本地 Tool Policy 不一致")
	}
	if !targetTypeAllowed(policy, proposal.Target.Type) {
		return deny(decision, "TARGET_TYPE_DENIED", policy.RuleID, "目标类型不在 Tool Policy allowlist")
	}
	if policy.Effect == contracts.Read {
		decision.Outcome = contracts.Allow
		decision.ReasonCodes = []string{"KNOWN_READ_TOOL"}
		decision.Reason = "本地策略确认该 Tool 为已知只读能力"
		decision.MatchedRuleIDs = []string{policy.RuleID}
		return decision
	}
	if policy.Reversible != proposal.Reversible {
		return deny(decision, "REVERSIBILITY_MISMATCH", policy.RuleID, "Action Proposal 的可逆性与本地 Tool Policy 不一致")
	}
	if proposal.Reversible && proposal.RollbackStrategy == "" {
		return deny(decision, "ROLLBACK_REQUIRED", policy.RuleID, "可逆写操作缺少明确 Rollback Strategy")
	}
	decision.MatchedRuleIDs = []string{policy.RuleID}
	decision.Outcome = contracts.Allow
	decision.ReasonCodes = []string{"POLICY_ALLOW"}
	decision.Reason = "已知写能力满足本地静态约束"
	return decision
}
func deny(decision contracts.GuardDecision, code, rule, reason string) contracts.GuardDecision {
	decision.Outcome = contracts.Deny
	decision.ReasonCodes = []string{code}
	decision.MatchedRuleIDs = []string{rule}
	decision.Reason = reason
	return decision
}
func targetTypeAllowed(policy ToolPolicy, targetType string) bool {
	if len(policy.AllowedTargetTypes) == 0 {
		return policy.Effect == contracts.Read
	}
	for _, allowed := range policy.AllowedTargetTypes {
		if allowed == targetType {
			return true
		}
	}
	return false
}
func forbiddenCapability(tool string) bool {
	normalized := strings.ToLower(strings.TrimSpace(tool))
	for _, name := range []string{"shell.execute", "terminal.run", "command.execute", "bash.run", "execute_command", "run_shell"} {
		if normalized == name {
			return true
		}
	}
	return false
}
