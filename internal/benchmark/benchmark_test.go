package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAllSuitesMeasureEveryRequiredMetricAndWriteFixedArtifacts(t *testing.T) {
	report, err := Run(context.Background(), "all", filepath.Join("..", "..", "policies", "tools.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() {
		for _, suite := range report.Suites {
			t.Logf("suite=%s status=%s error=%s cases=%+v", suite.Name, suite.Status, suite.Error, suite.Cases)
		}
		t.Fatal("all benchmark suites must pass their recorded fixtures")
	}
	for _, name := range metricNames {
		metric := report.Metrics[name]
		if !metric.Measured || metric.Value == NotMeasured {
			t.Fatalf("required metric was not measured: %s %+v", name, metric)
		}
	}
	jsonPath, markdownPath, err := Write(report, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(jsonPath) != "benchmark-report.json" || filepath.Base(markdownPath) != "benchmark-report.md" {
		t.Fatalf("unexpected artifact paths: %s %s", jsonPath, markdownPath)
	}
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Report
	if err := json.Unmarshal(b, &reopened); err != nil {
		t.Fatal(err)
	}
	if len(reopened.Suites) != 6 {
		t.Fatalf("got %d suites, want 6", len(reopened.Suites))
	}
}

func TestSingleSuiteLeavesUnselectedMetricsNotMeasured(t *testing.T) {
	report, err := Run(context.Background(), "intent", "unused")
	if err != nil {
		t.Fatal(err)
	}
	if report.Metrics["Intent Classification Accuracy"].Value == NotMeasured {
		t.Fatal("selected suite did not measure intent accuracy")
	}
	if report.Metrics["Rollback Success Rate"].Value != NotMeasured {
		t.Fatal("unselected metric was fabricated")
	}
}
