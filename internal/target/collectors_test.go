package target

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"safeops-agent/internal/perception"
)

type fakeTargetCollector struct {
	name   string
	values []perception.Observation
	err    error
}

func (c fakeTargetCollector) Name() string { return c.name }
func (c fakeTargetCollector) Collect(context.Context) ([]perception.Observation, error) {
	return c.values, c.err
}

func TestTargetCollectorPlanUsesAllSevenRealCollectorTypes(t *testing.T) {
	lab := t.TempDir()
	config := filepath.Join(lab, "config")
	if err := os.Mkdir(config, 0o750); err != nil {
		t.Fatal(err)
	}
	collectors, err := newTargetCollectors(lab, config)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(collectors))
	for _, collector := range collectors {
		names = append(names, collector.Name())
	}
	status, details := componentPlanCheck("collector", names, targetCollectorNames)
	if status != Pass {
		t.Fatalf("production collector plan failed: %s", details)
	}
}

func TestTargetCollectorChecksPersistCountsNotObservationValues(t *testing.T) {
	collectors := completeFakeCollectors()
	adapters := targetTestAdapters()
	report := newReport("test")
	appendTargetCollectorChecks(context.Background(), &report, collectors, adapters)
	report.finish()
	if report.Overall != Pass || len(report.Checks) != 12 {
		t.Fatalf("overall=%s checks=%d values=%+v", report.Overall, len(report.Checks), report.Checks)
	}
	for _, check := range report.Checks {
		if check.Status != Pass {
			t.Fatalf("target collector check failed: %+v", check)
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"journal-body-must-not-persist", "config-hash-must-not-persist"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("observation value %q escaped into target report", forbidden)
		}
	}
	for _, name := range []string{"collector_procfs", "collector_journal", "collector_config_change", "collector_adapter_prometheus", "collector_adapter_opentelemetry"} {
		if findTargetCheck(report.Checks, name) == nil {
			t.Fatalf("required count-only check missing: %s", name)
		}
	}
}

func TestTargetCollectorChecksAggregateAndRedactPartialFailures(t *testing.T) {
	collectors := completeFakeCollectors()
	for index, collector := range collectors {
		value := collector.(fakeTargetCollector)
		switch value.name {
		case "journal":
			value.err = errors.New("permission denied api_key=must-not-leak")
		case "config_change":
			value.values = nil
		}
		collectors[index] = value
	}
	report := newReport("test")
	appendTargetCollectorChecks(context.Background(), &report, collectors, targetTestAdapters())
	report.finish()
	if report.Overall != Fail || len(report.Checks) != 12 {
		t.Fatalf("overall=%s checks=%d", report.Overall, len(report.Checks))
	}
	journal := findTargetCheck(report.Checks, "collector_journal")
	config := findTargetCheck(report.Checks, "collector_config_change")
	if journal == nil || journal.Status != Fail || !strings.Contains(journal.Details, "[REDACTED]") || strings.Contains(journal.Details, "must-not-leak") {
		t.Fatalf("journal failure was not bounded and redacted: %+v", journal)
	}
	if config == nil || config.Status != Fail || !strings.Contains(config.Details, "no valid observations") {
		t.Fatalf("empty collector was not failed explicitly: %+v", config)
	}
	if healthy := findTargetCheck(report.Checks, "collector_procfs"); healthy == nil || healthy.Status != Pass {
		t.Fatalf("partial failure hid a healthy collector: %+v", healthy)
	}
	if adapter := findTargetCheck(report.Checks, "collector_adapter_opentelemetry"); adapter == nil || adapter.Status != Pass {
		t.Fatalf("partial batch did not continue through adapter: %+v", adapter)
	}
}

func TestComponentPlanCheckRejectsMissingDuplicateAndUnexpectedNames(t *testing.T) {
	status, details := componentPlanCheck("collector", []string{"procfs", "procfs", "unknown"}, []string{"procfs", "disk"})
	if status != Fail {
		t.Fatalf("broken plan passed: %s", details)
	}
	for _, wanted := range []string{"duplicate procfs", "missing disk", "unexpected unknown"} {
		if !strings.Contains(details, wanted) {
			t.Fatalf("plan detail %q does not contain %q", details, wanted)
		}
	}
}

func completeFakeCollectors() []perception.Collector {
	now := time.Unix(100, 0).UTC()
	collectors := make([]perception.Collector, 0, len(targetCollectorNames))
	for index, name := range targetCollectorNames {
		value := any(index + 1)
		if name == "journal" {
			value = "journal-body-must-not-persist"
		}
		if name == "config_change" {
			value = "config-hash-must-not-persist"
		}
		collectors = append(collectors, fakeTargetCollector{name: name, values: []perception.Observation{{
			ObservationID: name + "-observation",
			Source:        name,
			SourceType:    "target_fixture",
			Timestamp:     now,
			Host:          "kylin-fixture",
			ResourceType:  "host",
			ResourceID:    "fixture",
			MetricName:    name + "_metric",
			Value:         value,
			Unit:          "count",
			Severity:      "info",
			RawReference:  "fixture",
		}}})
	}
	return collectors
}

func targetTestAdapters() []perception.Adapter {
	return []perception.Adapter{
		perception.PrometheusAdapter{Config: perception.PrometheusAdapterConfig{Namespace: "safeops", MaxSamples: 100}},
		perception.OpenTelemetryAdapter{Config: perception.OpenTelemetryAdapterConfig{ServiceName: "safeops-agent", MaxDataPoints: 100}},
	}
}

func findTargetCheck(checks []Check, name string) *Check {
	for index := range checks {
		if checks[index].Name == name {
			return &checks[index]
		}
	}
	return nil
}
