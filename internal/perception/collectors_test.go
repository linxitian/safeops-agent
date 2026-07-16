package perception

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/safefs"
)

type fakePlatform struct {
	failMemory bool
}

func (fakePlatform) CPU(context.Context) (platform.CPUStat, error) {
	return platform.CPUStat{Busy: 30, Total: 100}, nil
}
func (p fakePlatform) Memory(context.Context) (platform.MemoryStat, error) {
	if p.failMemory {
		return platform.MemoryStat{}, errors.New("permission denied fixture")
	}
	return platform.MemoryStat{UsedBytes: 60, AvailableBytes: 40, SwapTotalBytes: 20, SwapFreeBytes: 15}, nil
}
func (fakePlatform) Load(context.Context) (platform.LoadAverage, error) {
	return platform.LoadAverage{One: 1, Five: 2, Fifteen: 3, RunningProcesses: 2}, nil
}
func (fakePlatform) Disk(context.Context, string) (platform.DiskUsage, error) {
	return platform.DiskUsage{Path: "/managed", TotalBytes: 100, UsedBytes: 90, FreeBytes: 10, UsedRatio: .9}, nil
}
func (fakePlatform) Mounts(context.Context) ([]platform.Mount, error) {
	return []platform.Mount{{Source: "/dev/vda", Target: "/managed", Filesystem: "ext4", Options: "rw"}}, nil
}
func (fakePlatform) Kernel(context.Context) (platform.KernelInfo, error) {
	return platform.KernelInfo{Architecture: "loong64", Kernel: "6.6", Hostname: "fixture", OSName: "Kylin", OSVersion: "V11"}, nil
}
func (fakePlatform) Uptime(context.Context) (time.Duration, error) { return 5 * time.Minute, nil }
func (fakePlatform) Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error) {
	return []platform.ProcessInfo{{PID: 22, StartTicks: 123, Name: "demo", State: "R", UID: 1000, CPUTicks: 44, RSSBytes: 4096}}, nil
}
func (fakePlatform) Sockets(context.Context, bool, int) ([]platform.SocketInfo, error) {
	return []platform.SocketInfo{{Protocol: "tcp", LocalAddress: "127.0.0.1", LocalPort: 18080, State: "LISTEN", Inode: "77", Listening: true}}, nil
}
func (fakePlatform) Interfaces(context.Context) ([]platform.InterfaceInfo, error) {
	return []platform.InterfaceInfo{{Name: "lo", MTU: 65536, Flags: []string{"up"}, Addresses: []string{"127.0.0.1/8"}, RXBytes: 10, TXBytes: 20}}, nil
}
func (fakePlatform) Service(context.Context, string) (platform.ServiceStatus, error) {
	return platform.ServiceStatus{Name: "safeops-demo.service", Description: "fixture", LoadState: "loaded", ActiveState: "failed", SubState: "failed", MainPID: 22, RestartCount: 3}, nil
}
func (fakePlatform) FailedServices(context.Context) ([]platform.ServiceStatus, error) {
	return []platform.ServiceStatus{{Name: "safeops-demo.service", ActiveState: "failed"}}, nil
}
func (fakePlatform) ServiceDependencies(context.Context, string) (platform.ServiceDependencies, error) {
	return platform.ServiceDependencies{Name: "safeops-demo.service", Requires: []string{"network.target"}, After: []string{"network.target"}}, nil
}
func (fakePlatform) Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error) {
	return []platform.JournalEvent{{Timestamp: time.Unix(10, 0).UTC(), Unit: "safeops-demo.service", PID: 22, Priority: 3, Message: "password=[REDACTED]", Cursor: "cursor-1", Redacted: true}}, nil
}
func (fakePlatform) Sysctls(context.Context, []string) ([]platform.SysctlSetting, error) {
	return []platform.SysctlSetting{{Key: "kernel.pid_max", Value: "4194304"}}, nil
}

func TestCollectorsNormalizeAllRequiredDomains(t *testing.T) {
	root := t.TempDir()
	secretBody := "password=must-never-appear"
	if err := os.WriteFile(filepath.Join(root, "agent.conf"), []byte(secretBody), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := safefs.NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	source := fakePlatform{}
	collectors := []Collector{
		ProcfsCollector{Platform: source, Processes: source},
		DiskCollector{Platform: source, Reader: reader, Paths: []string{root}, LargeFileMin: 1},
		NetworkCollector{Platform: source},
		SystemdCollector{Platform: source, Units: []string{"safeops-demo.service"}},
		JournalCollector{Platform: source},
		SystemConfigCollector{Platform: source, Sysctls: source},
		ConfigChangeCollector{Reader: reader},
	}
	batch := CollectAll(context.Background(), collectors, CollectionLimits{MaxObservations: 1000})
	if batch.CollectorsRun != len(collectors) || batch.CollectorsFailed != 0 {
		t.Fatalf("collector batch run=%d failed=%d issues=%v", batch.CollectorsRun, batch.CollectorsFailed, batch.Issues)
	}
	metrics := map[string]bool{}
	for _, value := range batch.Observations {
		if err := validateObservation(value); err != nil {
			t.Fatal(err)
		}
		metrics[value.MetricName] = true
	}
	for _, want := range []string{
		"cpu_busy_ticks", "process_cpu_ticks",
		"filesystem_used_ratio", "directory_size_bytes", "large_file_size_bytes",
		"connection_state", "listening_port", "network_receive_bytes",
		"service_state", "service_main_pid", "service_restart_count", "service_dependency_requires",
		"journal_message", "os_version", "kernel_release", "mount_filesystem", "sysctl_value",
		"config_mtime_unix_seconds", "config_size_bytes", "config_sha256",
	} {
		if !metrics[want] {
			t.Errorf("missing normalized metric %s", want)
		}
	}
	encoded, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secretBody) {
		t.Fatal("config collector persisted an allowlisted configuration body")
	}
	if !strings.Contains(string(encoded), "[REDACTED]") {
		t.Fatal("journal redaction marker was not preserved")
	}
}

type collectorFunc struct {
	name string
	fn   func(context.Context) ([]Observation, error)
}

func (c collectorFunc) Name() string { return c.name }
func (c collectorFunc) Collect(ctx context.Context) ([]Observation, error) {
	return c.fn(ctx)
}

func TestCollectAllPreservesPartialResultsAndEnforcesBounds(t *testing.T) {
	now := time.Now().UTC()
	makeValue := func(index int) Observation {
		return observation("partial", "fixture", "host", "fixture", "metric_"+string(rune('a'+index)), index, "count", "info", "fixture", nil, now)
	}
	partial := collectorFunc{name: "partial", fn: func(context.Context) ([]Observation, error) {
		return []Observation{makeValue(0), makeValue(1), makeValue(2)}, errors.New("permission denied fixture")
	}}
	slow := collectorFunc{name: "slow", fn: func(ctx context.Context) ([]Observation, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	batch := CollectAll(context.Background(), []Collector{partial, slow}, CollectionLimits{PerCollectorTimeout: 10 * time.Millisecond, MaxObservations: 2, MaxIssues: 4})
	if len(batch.Observations) != 2 || !batch.Truncated {
		t.Fatalf("observations=%d truncated=%v", len(batch.Observations), batch.Truncated)
	}
	if batch.CollectorsFailed != 2 || len(batch.Issues) != 2 {
		t.Fatalf("failed=%d issues=%+v", batch.CollectorsFailed, batch.Issues)
	}
	if !batch.Issues[1].Timeout {
		t.Fatalf("slow collector issue not marked timeout: %+v", batch.Issues[1])
	}
}

func TestProcfsCollectorReturnsHealthyPartialEvidence(t *testing.T) {
	values, err := (ProcfsCollector{Platform: fakePlatform{failMemory: true}}).Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permission denied fixture") {
		t.Fatalf("expected bounded permission issue, got %v", err)
	}
	if len(values) == 0 {
		t.Fatal("healthy CPU/load evidence was discarded after memory failure")
	}
}

func TestConfigChangeDetectsModifiedAndRemovedWithoutBodies(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "managed.conf")
	if err := os.WriteFile(path, []byte("token=hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := safefs.NewReader(root)
	if err != nil {
		t.Fatal(err)
	}
	baseline := map[string]ConfigFingerprint{
		path:                             {SHA256: strings.Repeat("0", 64), SizeBytes: 1, Modified: time.Unix(1, 0)},
		filepath.Join(root, "gone.conf"): {SHA256: strings.Repeat("1", 64), SizeBytes: 1, Modified: time.Unix(1, 0)},
	}
	values, err := (ConfigChangeCollector{Reader: reader, Baseline: baseline}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	modified, removed := false, false
	for _, value := range values {
		modified = modified || value.Labels["change"] == "modified"
		removed = removed || value.MetricName == "config_change_state" && value.Value == "removed"
		if strings.Contains(strings.ToLower(strings.TrimSpace(toString(value.Value))), "token=hidden") {
			t.Fatal("configuration body escaped through Observation")
		}
	}
	if !modified || !removed {
		t.Fatalf("modified=%v removed=%v observations=%+v", modified, removed, values)
	}
}

func TestTelemetryAdaptersSeparateMetricsAndBoundedLogs(t *testing.T) {
	now := time.Now().UTC()
	batch := CollectionBatch{Observations: []Observation{
		observation("systemd", "fixture", "service", "demo", "restart-count", uint64(3), "count", "info", "fixture", nil, now),
		observation("journal", "fixture", "log", "1", "message", strings.Repeat("x", 5000), "text", "warning", "fixture", nil, now),
	}}
	value, err := (PrometheusAdapter{Config: PrometheusAdapterConfig{Namespace: "safeops", IncludeSeverity: true}}).Adapt(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	prom := value.(PrometheusPayload)
	if len(prom.Samples) != 1 || prom.SkippedNonNumeric != 1 || prom.Samples[0].Name != "safeops_restart_count" {
		t.Fatalf("unexpected prometheus payload: %+v", prom)
	}
	value, err = (OpenTelemetryAdapter{}).Adapt(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	otel := value.(OpenTelemetryPayload)
	if len(otel.Metrics) != 1 || len(otel.Logs) != 1 || len(otel.Logs[0].Body) > 4100 {
		t.Fatalf("unexpected otel payload: metrics=%d logs=%d body=%d", len(otel.Metrics), len(otel.Logs), len(otel.Logs[0].Body))
	}
}

func toString(value any) string {
	text, _ := value.(string)
	return text
}
