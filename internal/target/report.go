package target

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/registry"
)

type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Details string `json:"details"`
}

type Report struct {
	SchemaVersion  int                 `json:"schema_version"`
	ReportID       string              `json:"report_id"`
	GeneratedAt    time.Time           `json:"generated_at"`
	Scope          string              `json:"scope"`
	EvidenceLevel  string              `json:"evidence_level"`
	TargetVerified bool                `json:"target_verified"`
	Kernel         platform.KernelInfo `json:"kernel"`
	GoVersion      string              `json:"go_version"`
	CGOEnabled     string              `json:"cgo_enabled"`
	Overall        Status              `json:"overall"`
	Checks         []Check             `json:"checks"`
	Notes          []string            `json:"notes"`
}

func Probe(ctx context.Context) Report {
	report := newReport("probe")
	linux := platform.NewLinux()
	kernel, err := linux.Kernel(ctx)
	if err != nil {
		report.Checks = append(report.Checks, Check{Name: "kernel_info", Status: Fail, Details: err.Error()})
	} else {
		report.Kernel = kernel
		report.Checks = append(report.Checks, Check{Name: "kernel_info", Status: Pass, Details: fmt.Sprintf("%s %s %s", kernel.OSName, kernel.OSVersion, kernel.Kernel)})
	}
	if runtime.GOOS == "linux" {
		report.Checks = append(report.Checks, Check{Name: "goos_linux", Status: Pass, Details: runtime.GOOS})
	} else {
		report.Checks = append(report.Checks, Check{Name: "goos_linux", Status: Fail, Details: runtime.GOOS})
	}
	if runtime.GOARCH == "loong64" {
		report.Checks = append(report.Checks, Check{Name: "goarch_loong64", Status: Pass, Details: runtime.GOARCH})
	} else {
		report.Checks = append(report.Checks, Check{Name: "goarch_loong64", Status: Warn, Details: runtime.GOARCH + " (development host, not authoritative target)"})
	}
	osName := strings.ToLower(report.Kernel.OSName)
	if strings.Contains(osName, "kylin") || strings.Contains(osName, "麒麟") {
		report.Checks = append(report.Checks, Check{Name: "kylin_os_release", Status: Pass, Details: report.Kernel.OSName})
	} else {
		report.Checks = append(report.Checks, Check{Name: "kylin_os_release", Status: Warn, Details: firstNonEmpty(report.Kernel.OSName, "OS name unavailable")})
	}
	paths := platform.DiscoverCommandPaths()
	for name, path := range map[string]string{"systemctl": paths.Systemctl, "journalctl": paths.Journalctl} {
		status := Pass
		details := path
		if path == "" {
			status, details = Fail, "not found"
		}
		report.Checks = append(report.Checks, Check{Name: "command_" + name, Status: status, Details: details})
	}
	report.Checks = append(report.Checks,
		fixedCommandCheck(ctx, "glibc_version", "getconf", []string{"GNU_LIBC_VERSION"}, Fail),
		fixedPathCommandCheck(ctx, "systemd_version", paths.Systemctl, []string{"--version"}, Fail),
		fixedPathCommandCheck(ctx, "journalctl_json_support", paths.Journalctl, []string{"--output=json", "--lines=0", "--no-pager"}, Fail),
		fixedCommandCheck(ctx, "command_ss", "ss", []string{"--version"}, Fail),
		fixedCommandCheck(ctx, "command_ip", "ip", []string{"-V"}, Fail),
		fixedCommandCheck(ctx, "command_git", "git", []string{"--version"}, Warn),
		fixedCommandCheck(ctx, "command_go", "go", []string{"version"}, Warn),
		fixedCommandCheck(ctx, "command_gcc", "gcc", []string{"--version"}, Warn),
	)
	appendRead := func(name string, call func() error) {
		if err := call(); err != nil {
			report.Checks = append(report.Checks, Check{Name: name, Status: Fail, Details: err.Error()})
		} else {
			report.Checks = append(report.Checks, Check{Name: name, Status: Pass, Details: "read succeeded"})
		}
	}
	appendRead("proc_cpu", func() error { _, err := linux.CPU(ctx); return err })
	appendRead("proc_memory", func() error { _, err := linux.Memory(ctx); return err })
	appendRead("proc_load", func() error { _, err := linux.Load(ctx); return err })
	appendRead("proc_filesystem", func() error {
		info, err := os.Stat("/proc")
		if err == nil && !info.IsDir() {
			return errors.New("/proc is not a directory")
		}
		return err
	})
	appendRead("root_statfs", func() error { _, err := linux.Disk(ctx, "/"); return err })
	report.finish()
	return report
}

func Test(ctx context.Context, mcpConfig string) Report {
	report := Probe(ctx)
	report.Scope = "test"
	cfg, err := registry.Load(mcpConfig)
	if err != nil {
		report.Checks = append(report.Checks, Check{Name: "mcp_manifest", Status: Fail, Details: err.Error()})
		report.finish()
		return report
	}
	report.Checks = append(report.Checks, Check{Name: "mcp_manifest", Status: Pass, Details: fmt.Sprintf("%d server manifests", len(cfg.Servers))})
	reg := registry.New(cfg)
	defer reg.Close()
	if err := reg.Start(ctx); err != nil {
		report.Checks = append(report.Checks, Check{Name: "mcp_registry_start", Status: Fail, Details: err.Error()})
		report.finish()
		return report
	}
	states := reg.States()
	healthy, tools := 0, 0
	for _, state := range states {
		if state.Status == registry.StatusHealthy {
			healthy++
		}
		tools += len(state.Tools)
	}
	status := Pass
	if healthy != 8 || tools != 39 {
		status = Fail
	}
	report.Checks = append(report.Checks, Check{Name: "mcp_discovery", Status: status, Details: fmt.Sprintf("healthy=%d/8 tools=%d/39", healthy, tools)})
	for _, state := range states {
		if state.Status != registry.StatusHealthy {
			continue
		}
		healthCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := reg.Health(healthCtx, state.Manifest.ID)
		cancel()
		checkStatus, details := Pass, "ping succeeded"
		if err != nil {
			checkStatus, details = Fail, err.Error()
		}
		report.Checks = append(report.Checks, Check{Name: "mcp_ping_" + state.Manifest.ID, Status: checkStatus, Details: details})
	}
	calls := targetToolCalls(os.Getpid())
	if err := validateTargetCallCoverage(states, calls); err != nil {
		report.Checks = append(report.Checks, Check{Name: "mcp_call_plan", Status: Fail, Details: boundedDetail(err.Error())})
	} else {
		report.Checks = append(report.Checks, Check{Name: "mcp_call_plan", Status: Pass, Details: fmt.Sprintf("%d/%d discovered tools have one native call", len(calls), tools)})
	}
	appendTargetToolCallChecks(ctx, &report, reg, calls)
	report.finish()
	return report
}

func Doctor(ctx context.Context, mcpConfig string) Report {
	report := Test(ctx, mcpConfig)
	report.Scope = "doctor"
	for _, directory := range []string{"/var/lib/safeops", "/var/lib/safeops/lab", "/run/safeops"} {
		info, err := os.Stat(directory)
		status, details := Pass, "directory exists"
		if err != nil || !info.IsDir() {
			status, details = Warn, "directory is absent; installer has not prepared it"
		}
		report.Checks = append(report.Checks, Check{Name: "directory_" + strings.Trim(strings.ReplaceAll(directory, "/", "_"), "_"), Status: status, Details: details})
	}
	report.finish()
	return report
}

func Write(report Report, directory string) (string, string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", "", errors.New("report directory is required")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return "", "", err
	}
	base := "target-" + safeScope(report.Scope) + "-report"
	jsonPath := filepath.Join(directory, base+".json")
	markdownPath := filepath.Join(directory, base+".txt")
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
	sum := sha256.Sum256(b)
	if err := atomicWrite(jsonPath+".sha256", []byte(hex.EncodeToString(sum[:])+"  "+filepath.Base(jsonPath)+"\n")); err != nil {
		return "", "", err
	}
	return jsonPath, markdownPath, nil
}

func newReport(scope string) Report {
	now := time.Now().UTC()
	host, _ := os.Hostname()
	sum := sha256.Sum256([]byte(host + "\x00" + runtime.GOARCH + "\x00" + now.Format(time.RFC3339Nano)))
	return Report{SchemaVersion: 1, ReportID: "target_" + hex.EncodeToString(sum[:10]), GeneratedAt: now, Scope: scope, EvidenceLevel: "NATIVE_EXECUTION_REPORT", TargetVerified: false, GoVersion: runtime.Version(), CGOEnabled: buildSetting("CGO_ENABLED"), Notes: []string{"This report proves only what this native run observed.", "target_verified remains false until the project audits an official Kylin V11 VM report."}}
}

func (r *Report) finish() {
	sort.SliceStable(r.Checks, func(i, j int) bool { return r.Checks[i].Name < r.Checks[j].Name })
	r.Overall = Pass
	for _, check := range r.Checks {
		if check.Status == Fail {
			r.Overall = Fail
			return
		}
		if check.Status == Warn {
			r.Overall = Warn
		}
	}
}

func buildSetting(name string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == name {
			return setting.Value
		}
	}
	return "unknown"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func fixedCommandCheck(ctx context.Context, checkName, command string, args []string, missingStatus Status) Check {
	path := platform.FindFixedBinary(command)
	if path == "" {
		details := "not found"
		if command == "go" {
			details += "; running binary was built with " + runtime.Version()
		}
		return Check{Name: checkName, Status: missingStatus, Details: details}
	}
	return fixedPathCommandCheck(ctx, checkName, path, args, missingStatus)
}

func fixedPathCommandCheck(ctx context.Context, checkName, path string, args []string, missingStatus Status) Check {
	if path == "" {
		return Check{Name: checkName, Status: missingStatus, Details: "not found"}
	}
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := (platform.OSFixedRunner{MaxOutputBytes: 64 << 10}).Run(commandCtx, path, args...)
	details := compactOutput(output)
	if err != nil {
		if details == "" {
			details = err.Error()
		} else {
			details = details + "; " + err.Error()
		}
		return Check{Name: checkName, Status: Fail, Details: details}
	}
	if details == "" {
		details = "supported"
	}
	return Check{Name: checkName, Status: Pass, Details: details}
}

func compactOutput(output []byte) string {
	details := strings.Join(strings.Fields(string(output)), " ")
	if len(details) > 512 {
		details = details[:512] + "..."
	}
	return details
}

func safeScope(scope string) string {
	switch scope {
	case "probe", "test", "doctor", "report":
		return scope
	default:
		return "report"
	}
}

func atomicWrite(path string, data []byte) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".target-report-*.tmp")
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
	fmt.Fprintf(&builder, "# SafeOps Target Report\n\n- Report ID: `%s`\n- Generated: `%s`\n- Scope: `%s`\n- Overall: `%s`\n- Native architecture: `%s`\n- OS: `%s`\n- Target verified: `%t`\n\n", report.ReportID, report.GeneratedAt.Format(time.RFC3339), report.Scope, report.Overall, report.Kernel.Architecture, report.Kernel.OSName, report.TargetVerified)
	builder.WriteString("| Check | Status | Details |\n|---|---:|---|\n")
	for _, check := range report.Checks {
		details := strings.ReplaceAll(check.Details, "|", "\\|")
		fmt.Fprintf(&builder, "| `%s` | %s | %s |\n", check.Name, check.Status, details)
	}
	builder.WriteString("\nThis report is native execution evidence, not an automatic `TARGET_VERIFIED` declaration.\n")
	return builder.String()
}
