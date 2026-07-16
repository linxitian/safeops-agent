package benchmark

import (
	"context"
	"fmt"
)

var Commands = []string{"intent", "tool-selection", "safety", "rca", "continuity", "performance", "all"}

func Run(ctx context.Context, command, policyPath string) (Report, error) {
	report := NewReport(command)
	switch command {
	case "intent":
		runIntent(&report)
	case "tool-selection":
		runToolSelection(ctx, &report, policyPath)
	case "safety":
		runSafety(ctx, &report, policyPath)
	case "rca":
		runRCA(&report)
	case "continuity":
		runContinuity(ctx, &report, policyPath)
	case "performance":
		runPerformance(&report)
	case "all":
		runIntent(&report)
		runToolSelection(ctx, &report, policyPath)
		runSafety(ctx, &report, policyPath)
		runRCA(&report)
		runContinuity(ctx, &report, policyPath)
		runPerformance(&report)
	default:
		return report, fmt.Errorf("unknown benchmark command %q", command)
	}
	return report, nil
}
