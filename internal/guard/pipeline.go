package guard

import "safeops-agent/contracts"

type PipelineResult struct {
	Static contracts.GuardDecision `json:"static_guard"`
	Intent IntentDecision          `json:"intent_guard"`
	Risk   contracts.RiskResult    `json:"risk"`
	Final  contracts.GuardDecision `json:"final_decision"`
}

type SafetyPipeline struct {
	Static StaticGuard
	Intent IntentGuard
	Risk   RiskEngine
}

func NewSafetyPipeline(catalog *Catalog) SafetyPipeline {
	return SafetyPipeline{Static: NewStaticGuard(catalog), Intent: IntentGuard{PolicyVersion: catalog.VersionID()}, Risk: RiskEngine{Catalog: catalog}}
}

func (p SafetyPipeline) Evaluate(proposal contracts.ActionProposal) PipelineResult {
	static := p.Static.Precheck(proposal)
	result := PipelineResult{Static: static, Final: static}
	if static.Outcome == contracts.Deny {
		return result
	}
	intent := p.Intent.Evaluate(proposal)
	result.Intent = intent
	if intent.Outcome == contracts.Deny {
		result.Final = deny(static, intent.ReasonCode, "intent-guard", intent.Reason)
		return result
	}
	risk := p.Risk.Evaluate(proposal)
	result.Risk = risk
	final := static
	final.Risk = risk
	policy, _ := p.Static.Catalog.Policy(proposal.Tool)
	if risk.Level == contracts.L3 {
		final = deny(final, "L3_DEFAULT_DENY", policy.RuleID, "L3 关键操作默认拒绝")
	} else if proposal.Effect == contracts.Write && (policy.Approval || risk.Level == contracts.L2) {
		final.Outcome = contracts.RequireApproval
		final.ReasonCodes = []string{"WRITE_REQUIRES_APPROVAL"}
		final.Reason = "本地策略或上下文风险要求人工审批"
	}
	result.Final = final
	return result
}
