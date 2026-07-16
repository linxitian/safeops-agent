package benchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const NotMeasured = "NOT_MEASURED"

var metricNames = []string{
	"Intent Classification Accuracy",
	"Tool Selection Accuracy",
	"Context Reference Resolution Accuracy",
	"High Risk Recall",
	"Safety False Positive Rate",
	"Unauthorized Execution Rate",
	"RCA Top-1 Accuracy",
	"RCA Top-3 Accuracy",
	"Mean Diagnosis Latency",
	"P95 Diagnosis Latency",
	"Task Completion Rate",
	"Approval Resume Success Rate",
	"Server Restart Recovery Rate",
	"Trace Coverage",
	"Trace Integrity Verification Rate",
	"Rollback Success Rate",
}

type Metric struct {
	Name        string `json:"name"`
	Value       any    `json:"value"`
	Unit        string `json:"unit,omitempty"`
	Numerator   int    `json:"numerator,omitempty"`
	Denominator int    `json:"denominator,omitempty"`
	Method      string `json:"method"`
	Measured    bool   `json:"measured"`
}

type Case struct {
	ID       string        `json:"id"`
	Category string        `json:"category"`
	Passed   bool          `json:"passed"`
	Details  string        `json:"details"`
	Duration time.Duration `json:"duration_ns"`
}

type Suite struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Cases    []Case `json:"cases"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"duration_ns"`
}

type Report struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Command       string            `json:"command"`
	GOOS          string            `json:"goos"`
	GOARCH        string            `json:"goarch"`
	GoVersion     string            `json:"go_version"`
	Suites        []Suite           `json:"suites"`
	Metrics       map[string]Metric `json:"metrics"`
	Notes         []string          `json:"notes"`
}

func NewReport(command string) Report {
	metrics := make(map[string]Metric, len(metricNames))
	for _, name := range metricNames {
		metrics[name] = Metric{Name: name, Value: NotMeasured, Method: "suite not selected or no valid observations", Measured: false}
	}
	return Report{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		Command:       command,
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		GoVersion:     runtime.Version(),
		Metrics:       metrics,
		Notes: []string{
			"Every numeric metric in this report was computed from the recorded cases in this native run.",
			"NOT_MEASURED is retained when a suite was not selected or produced no valid observations.",
			"Synthetic case fixtures exercise production algorithms and persistence boundaries; they are not target-VM certification evidence.",
		},
	}
}

func (r *Report) setRate(name string, numerator, denominator int, method string) {
	if denominator <= 0 {
		return
	}
	r.Metrics[name] = Metric{Name: name, Value: round(float64(numerator) * 100 / float64(denominator)), Unit: "percent", Numerator: numerator, Denominator: denominator, Method: method, Measured: true}
}

func (r *Report) setDuration(name string, value float64, method string) {
	r.Metrics[name] = Metric{Name: name, Value: round(value), Unit: "milliseconds", Method: method, Measured: true}
}

func (r *Report) addSuite(suite Suite) {
	r.Suites = append(r.Suites, suite)
}

func (s *Suite) finish(start time.Time) {
	s.Duration = time.Since(start).Nanoseconds()
	s.Status = "PASS"
	if s.Error != "" {
		s.Status = "FAIL"
		return
	}
	for _, result := range s.Cases {
		if !result.Passed {
			s.Status = "FAIL"
			return
		}
	}
}

func (r Report) Failed() bool {
	for _, suite := range r.Suites {
		if suite.Status == "FAIL" {
			return true
		}
	}
	return false
}

func Write(report Report, directory string) (string, string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", "", errors.New("benchmark output directory is required")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return "", "", err
	}
	jsonPath := filepath.Join(directory, "benchmark-report.json")
	markdownPath := filepath.Join(directory, "benchmark-report.md")
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", "", err
	}
	b = append(b, '\n')
	if err := atomicWrite(jsonPath, b); err != nil {
		return "", "", err
	}
	if err := atomicWrite(markdownPath, []byte(markdown(report))); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func atomicWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".benchmark-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func markdown(report Report) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "# SafeOps Benchmark Report\n\n- Generated: `%s`\n- Command: `%s`\n- Platform: `%s/%s`\n- Go: `%s`\n\n", report.GeneratedAt.Format(time.RFC3339), report.Command, report.GOOS, report.GOARCH, report.GoVersion)
	builder.WriteString("| Metric | Value | Observations | Method |\n|---|---:|---:|---|\n")
	names := append([]string(nil), metricNames...)
	sort.Strings(names)
	for _, name := range names {
		metric := report.Metrics[name]
		value := fmt.Sprint(metric.Value)
		if metric.Unit != "" && metric.Measured {
			value += " " + metric.Unit
		}
		observations := "-"
		if metric.Denominator > 0 {
			observations = fmt.Sprintf("%d/%d", metric.Numerator, metric.Denominator)
		}
		fmt.Fprintf(&builder, "| %s | %s | %s | %s |\n", name, value, observations, escape(metric.Method))
	}
	for _, suite := range report.Suites {
		fmt.Fprintf(&builder, "\n## %s (%s)\n\n", suite.Name, suite.Status)
		if suite.Error != "" {
			fmt.Fprintf(&builder, "Error: `%s`\n\n", escape(suite.Error))
		}
		builder.WriteString("| Case | Category | Result | Details |\n|---|---|---:|---|\n")
		for _, result := range suite.Cases {
			status := "PASS"
			if !result.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(&builder, "| `%s` | %s | %s | %s |\n", result.ID, escape(result.Category), status, escape(result.Details))
		}
	}
	builder.WriteString("\nNumeric results above come only from this run; unselected metrics remain `NOT_MEASURED`.\n")
	return builder.String()
}

func escape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

func round(value float64) float64 {
	return float64(int(value*1000+0.5)) / 1000
}
