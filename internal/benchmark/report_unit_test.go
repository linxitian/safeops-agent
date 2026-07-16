package benchmark

import (
	"strings"
	"testing"
	"time"
)

func TestNewReportInitializesEveryMetricAsNotMeasured(t *testing.T) {
	report := NewReport("unit")
	if report.SchemaVersion != 1 || report.Command != "unit" {
		t.Fatalf("unexpected report identity: %+v", report)
	}
	if len(report.Metrics) != len(metricNames) {
		t.Fatalf("metrics = %d, want %d", len(report.Metrics), len(metricNames))
	}
	for _, name := range metricNames {
		metric := report.Metrics[name]
		if metric.Measured || metric.Value != NotMeasured {
			t.Fatalf("metric %s should start as NOT_MEASURED: %+v", name, metric)
		}
	}
}

func TestReportMetricsSuitesAndMarkdownEscaping(t *testing.T) {
	report := NewReport("unit")
	report.setRate("Intent Classification Accuracy", 1, 3, "one|two\nthree")
	report.setDuration("Mean Diagnosis Latency", 12.34567, "timer")
	if report.Metrics["Intent Classification Accuracy"].Value != 33.333 {
		t.Fatalf("rate was not rounded to three decimals: %+v", report.Metrics["Intent Classification Accuracy"])
	}
	if report.Metrics["Mean Diagnosis Latency"].Value != 12.346 {
		t.Fatalf("duration was not rounded to three decimals: %+v", report.Metrics["Mean Diagnosis Latency"])
	}
	suite := Suite{Name: "unit", Cases: []Case{{ID: "case-1", Passed: true}}}
	suite.finish(time.Now())
	report.addSuite(suite)
	if report.Failed() {
		t.Fatal("passing suite marked report failed")
	}
	output := markdown(report)
	if !strings.Contains(output, "one\\|two three") {
		t.Fatalf("markdown did not escape table delimiters/newlines:\n%s", output)
	}
	if _, _, err := Write(report, " \t "); err == nil {
		t.Fatal("blank benchmark output directory was accepted")
	}
}

func TestFailedReportsAnyFailedSuite(t *testing.T) {
	report := NewReport("unit")
	report.addSuite(Suite{Name: "failed", Status: "FAIL"})
	if !report.Failed() {
		t.Fatal("failed suite did not fail the report")
	}
}
