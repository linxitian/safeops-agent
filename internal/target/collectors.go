package target

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"safeops-agent/internal/perception"
	"safeops-agent/internal/platform"
	"safeops-agent/internal/safefs"
)

var targetCollectorNames = []string{
	"config_change",
	"disk",
	"journal",
	"network",
	"procfs",
	"system_config",
	"systemd",
}

var targetAdapterNames = []string{"opentelemetry", "prometheus"}

func appendNativeCollectorChecks(ctx context.Context, report *Report) {
	collectors, err := newTargetCollectors(targetLabRoot, targetConfigPath)
	if err != nil {
		report.Checks = append(report.Checks, Check{Name: "collector_setup", Status: Fail, Details: boundedDetail(err.Error())})
		return
	}
	adapters := []perception.Adapter{
		perception.PrometheusAdapter{Config: perception.PrometheusAdapterConfig{Namespace: "safeops", IncludeSeverity: true, MaxSamples: 2000}},
		perception.OpenTelemetryAdapter{Config: perception.OpenTelemetryAdapterConfig{ServiceName: "safeops-agent", MaxDataPoints: 2000}},
	}
	appendTargetCollectorChecks(ctx, report, collectors, adapters)
}

func newTargetCollectors(labRoot, configPath string) ([]perception.Collector, error) {
	labReader, err := safefs.NewReader(labRoot)
	if err != nil {
		return nil, fmt.Errorf("lab reader: %w", err)
	}
	configReader, err := safefs.NewReader(configPath)
	if err != nil {
		return nil, fmt.Errorf("config reader: %w", err)
	}
	linux := platform.NewLinux()
	commands := platform.NewCommandPlatform()
	return []perception.Collector{
		perception.ProcfsCollector{Platform: linux, Processes: linux, ProcessLimit: 10},
		perception.DiskCollector{Platform: linux, Reader: labReader, Paths: []string{labRoot}, MaxMounts: 128, MaxDepth: 4, MaxEntries: 500, LargeFileMin: 1, LargeFileLimit: 20},
		perception.NetworkCollector{Platform: linux, SocketLimit: 200},
		perception.SystemdCollector{Platform: commands, Units: []string{targetServiceUnit, "safeops-privexec.service"}, MaxUnits: 16},
		perception.JournalCollector{Platform: commands, Queries: []platform.JournalQuery{{Unit: targetServiceUnit, Lines: 20, Priority: 7}}},
		perception.SystemConfigCollector{Platform: linux, Sysctls: linux, SelectedSysctls: []string{"kernel.pid_max", "net.core.somaxconn"}, MaxMounts: 128},
		perception.ConfigChangeCollector{Reader: configReader, Paths: []string{configPath}, Limit: 1, MaxFileBytes: 1 << 20, MaxTotalBytes: 1 << 20},
	}, nil
}

func appendTargetCollectorChecks(ctx context.Context, report *Report, collectors []perception.Collector, adapters []perception.Adapter) {
	collectorNames := make([]string, 0, len(collectors))
	for _, collector := range collectors {
		if collector == nil {
			collectorNames = append(collectorNames, "")
			continue
		}
		collectorNames = append(collectorNames, collector.Name())
	}
	collectorPlanStatus, collectorPlanDetails := componentPlanCheck("collector", collectorNames, targetCollectorNames)
	report.Checks = append(report.Checks, Check{Name: "collector_plan", Status: collectorPlanStatus, Details: collectorPlanDetails})

	batch := perception.CollectAll(ctx, collectors, perception.CollectionLimits{
		PerCollectorTimeout: 5 * time.Second,
		MaxObservations:     2000,
		MaxIssues:           64,
	})
	counts := map[string]int{}
	metrics := map[string]map[string]bool{}
	sourceTypes := map[string]map[string]bool{}
	for _, value := range batch.Observations {
		counts[value.Source]++
		if metrics[value.Source] == nil {
			metrics[value.Source] = map[string]bool{}
			sourceTypes[value.Source] = map[string]bool{}
		}
		metrics[value.Source][value.MetricName] = true
		sourceTypes[value.Source][value.SourceType] = true
	}
	issues := map[string][]string{}
	for _, issue := range batch.Issues {
		detail := issue.Error
		if issue.Timeout {
			detail = "timeout: " + detail
		}
		issues[issue.Collector] = append(issues[issue.Collector], detail)
	}
	seenChecks := map[string]int{}
	for _, name := range collectorNames {
		seenChecks[name]++
		checkName := "collector_" + safeCheckName(name)
		if seenChecks[name] > 1 {
			checkName += fmt.Sprintf("_%d", seenChecks[name])
		}
		status := Pass
		details := fmt.Sprintf("observations=%d metrics=%d source_types=%d", counts[name], len(metrics[name]), len(sourceTypes[name]))
		if name == "" || counts[name] == 0 {
			status = Fail
			details += "; no valid observations"
		}
		if len(issues[name]) > 0 {
			status = Fail
			details += "; issues=" + boundedDetail(strings.Join(issues[name], "; "))
		}
		report.Checks = append(report.Checks, Check{Name: checkName, Status: status, Details: details})
	}
	batchStatus := Pass
	if batch.CollectorsRun != len(collectors) || batch.CollectorsFailed != 0 || batch.Truncated || batch.IssuesTruncated || len(batch.Observations) == 0 {
		batchStatus = Fail
	}
	report.Checks = append(report.Checks, Check{
		Name:   "collector_batch",
		Status: batchStatus,
		Details: fmt.Sprintf("run=%d/%d failed=%d observations=%d issues=%d truncated=%t issues_truncated=%t",
			batch.CollectorsRun, len(collectors), batch.CollectorsFailed, len(batch.Observations), len(batch.Issues), batch.Truncated, batch.IssuesTruncated),
	})

	adapterNames := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			adapterNames = append(adapterNames, "")
			continue
		}
		adapterNames = append(adapterNames, adapter.Name())
	}
	adapterPlanStatus, adapterPlanDetails := componentPlanCheck("adapter", adapterNames, targetAdapterNames)
	report.Checks = append(report.Checks, Check{Name: "collector_adapter_plan", Status: adapterPlanStatus, Details: adapterPlanDetails})
	for _, adapter := range adapters {
		if adapter == nil {
			report.Checks = append(report.Checks, Check{Name: "collector_adapter_unknown", Status: Fail, Details: "nil adapter"})
			continue
		}
		adapterCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		value, err := adapter.Adapt(adapterCtx, batch)
		cancel()
		status, details := adapterResult(adapter.Name(), value, err)
		report.Checks = append(report.Checks, Check{Name: "collector_adapter_" + safeCheckName(adapter.Name()), Status: status, Details: details})
	}
}

func componentPlanCheck(kind string, actual, expected []string) (Status, string) {
	counts := map[string]int{}
	for _, name := range actual {
		counts[name]++
	}
	expectedSet := map[string]bool{}
	for _, name := range expected {
		expectedSet[name] = true
	}
	var problems []string
	for _, name := range expected {
		switch counts[name] {
		case 0:
			problems = append(problems, "missing "+name)
		case 1:
		default:
			problems = append(problems, "duplicate "+name)
		}
	}
	for name := range counts {
		if !expectedSet[name] {
			problems = append(problems, "unexpected "+firstNonEmpty(name, "<empty>"))
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		return Fail, boundedDetail(kind + " plan: " + strings.Join(problems, ", "))
	}
	return Pass, fmt.Sprintf("%d/%d unique %ss configured", len(actual), len(expected), kind)
}

func adapterResult(name string, value any, err error) (Status, string) {
	if err != nil {
		return Fail, boundedDetail(err.Error())
	}
	switch name {
	case "prometheus":
		payload, ok := value.(perception.PrometheusPayload)
		if !ok {
			return Fail, "adapter returned unexpected payload type"
		}
		status := Pass
		if len(payload.Samples) == 0 || payload.Truncated {
			status = Fail
		}
		return status, fmt.Sprintf("samples=%d skipped_non_numeric=%d truncated=%t", len(payload.Samples), payload.SkippedNonNumeric, payload.Truncated)
	case "opentelemetry":
		payload, ok := value.(perception.OpenTelemetryPayload)
		if !ok {
			return Fail, "adapter returned unexpected payload type"
		}
		status := Pass
		if len(payload.Metrics)+len(payload.Logs) == 0 || payload.Truncated {
			status = Fail
		}
		return status, fmt.Sprintf("metrics=%d logs=%d truncated=%t", len(payload.Metrics), len(payload.Logs), payload.Truncated)
	default:
		return Fail, "unsupported target adapter"
	}
}

func safeCheckName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var out strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' {
			out.WriteRune(char)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}
