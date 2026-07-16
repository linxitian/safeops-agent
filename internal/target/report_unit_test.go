package target

import (
	"strings"
	"testing"
)

func TestReportFinishSortsChecksAndUsesWorstStatus(t *testing.T) {
	report := Report{Checks: []Check{{Name: "z_warn", Status: Warn}, {Name: "a_pass", Status: Pass}}}
	report.finish()
	if report.Overall != Warn {
		t.Fatalf("overall = %s, want WARN", report.Overall)
	}
	if report.Checks[0].Name != "a_pass" {
		t.Fatalf("checks were not sorted: %+v", report.Checks)
	}
	report.Checks = append(report.Checks, Check{Name: "m_fail", Status: Fail})
	report.finish()
	if report.Overall != Fail {
		t.Fatalf("overall = %s, want FAIL", report.Overall)
	}
}

func TestReportHelpersKeepOutputBoundedAndSafe(t *testing.T) {
	for _, scope := range []string{"probe", "test", "doctor", "report"} {
		if safeScope(scope) != scope {
			t.Fatalf("safe scope changed valid scope %q", scope)
		}
	}
	if safeScope("../evil") != "report" {
		t.Fatal("unsafe scope was not normalized to report")
	}
	if compactOutput([]byte("one\n two\tthree")) != "one two three" {
		t.Fatalf("compact output did not normalize whitespace")
	}
	long := compactOutput([]byte(strings.Repeat("x", 600)))
	if len(long) != 515 || !strings.HasSuffix(long, "...") {
		t.Fatalf("long output was not bounded: len=%d suffix=%q", len(long), long[len(long)-3:])
	}
}
