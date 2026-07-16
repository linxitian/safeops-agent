package guard

import (
	"fmt"
	"path/filepath"
	"strings"

	"safeops-agent/contracts"
)

type RiskEngine struct{ Catalog *Catalog }

func (e RiskEngine) Evaluate(proposal contracts.ActionProposal) contracts.RiskResult {
	policy, known := e.Catalog.Policy(proposal.Tool)
	score := 0
	factors := []string{}
	base := contracts.L3
	if known {
		base = policy.BaseRisk
	}
	switch base {
	case contracts.L0:
		score = 5
	case contracts.L1:
		score = 20
	case contracts.L2:
		score = 50
	default:
		score = 90
	}
	factors = append(factors, "BASE_"+string(base))
	if !known {
		factors = append(factors, "UNKNOWN_TOOL")
	}
	if proposal.Effect == contracts.Write {
		score += 10
		factors = append(factors, "WRITE_EFFECT")
	}
	critical := isCriticalTarget(proposal.Target)
	if critical {
		score += 50
		factors = append(factors, "CRITICAL_TARGET")
	}
	batch := proposal.BatchSize
	if batch <= 0 {
		batch = 1
	}
	if batch >= 10 {
		score += 35
		factors = append(factors, "LARGE_BATCH")
	} else if batch > 1 {
		score += 10
		factors = append(factors, "MULTI_TARGET")
	}
	if proposal.Effect == contracts.Write && !proposal.Reversible {
		score += 15
		factors = append(factors, "NOT_REVERSIBLE")
	}
	if proposal.Reversible && proposal.RollbackStrategy == "" {
		score += 10
		factors = append(factors, "ROLLBACK_UNDECLARED")
	}
	if strings.EqualFold(proposal.SystemState, "degraded") || strings.EqualFold(proposal.SystemState, "critical") {
		score += 10
		factors = append(factors, "SYSTEM_UNSTABLE")
	}
	if proposal.LabMode && !critical {
		score -= 10
		factors = append(factors, "LAB_SCOPE")
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return contracts.RiskResult{Level: levelForScore(score), Score: score, Factors: factors, Reason: fmt.Sprintf("工具基础风险、目标关键性、范围、批量、可逆性、Lab 和系统状态合成得分 %d", score)}
}

func levelForScore(score int) contracts.RiskLevel {
	switch {
	case score < 20:
		return contracts.L0
	case score < 50:
		return contracts.L1
	case score < 80:
		return contracts.L2
	default:
		return contracts.L3
	}
}

func isCriticalTarget(target contracts.TargetRef) bool {
	if strings.EqualFold(target.Criticality, "critical") {
		return true
	}
	id := strings.ToLower(strings.TrimSpace(target.ID))
	if target.Type == "service" {
		for _, name := range []string{"sshd", "ssh.service", "sshd.service", "systemd", "dbus.service", "network.service", "networkmanager.service", "safeops-privexec.service"} {
			if id == name {
				return true
			}
		}
	}
	if target.Type == "file" {
		clean := filepath.Clean(id)
		for _, prefix := range []string{"/boot", "/etc", "/usr", "/bin", "/sbin", "/root"} {
			if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}
