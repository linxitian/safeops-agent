package perception

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"safeops-agent/internal/platform"
)

// Observation is the collector-independent evidence contract consumed by the
// Agent, RCA and Evidence Graph layers. Value is intentionally typed as any:
// numeric metrics, bounded strings such as service state, and small structured
// values all use the same envelope without preserving raw command output.
type Observation struct {
	ObservationID string            `json:"observation_id"`
	Source        string            `json:"source"`
	SourceType    string            `json:"source_type"`
	Timestamp     time.Time         `json:"timestamp"`
	Host          string            `json:"host"`
	ResourceType  string            `json:"resource_type"`
	ResourceID    string            `json:"resource_id"`
	MetricName    string            `json:"metric_name"`
	Value         any               `json:"value"`
	Unit          string            `json:"unit"`
	Severity      string            `json:"severity"`
	Labels        map[string]string `json:"labels,omitempty"`
	RawReference  string            `json:"raw_reference"`
}

type Collector interface {
	Name() string
	Collect(context.Context) ([]Observation, error)
}

type CollectorIssue struct {
	Collector string `json:"collector"`
	Error     string `json:"error"`
	Timeout   bool   `json:"timeout"`
}

type CollectionBatch struct {
	StartedAt        time.Time        `json:"started_at"`
	CompletedAt      time.Time        `json:"completed_at"`
	Observations     []Observation    `json:"observations"`
	Issues           []CollectorIssue `json:"issues,omitempty"`
	Truncated        bool             `json:"truncated"`
	IssuesTruncated  bool             `json:"issues_truncated"`
	CollectorsRun    int              `json:"collectors_run"`
	CollectorsFailed int              `json:"collectors_failed"`
}

type CollectionLimits struct {
	PerCollectorTimeout time.Duration
	MaxObservations     int
	MaxIssues           int
}

func (l CollectionLimits) normalized() CollectionLimits {
	if l.PerCollectorTimeout <= 0 {
		l.PerCollectorTimeout = 5 * time.Second
	}
	if l.PerCollectorTimeout > 30*time.Second {
		l.PerCollectorTimeout = 30 * time.Second
	}
	if l.MaxObservations <= 0 {
		l.MaxObservations = 2000
	}
	if l.MaxObservations > 10000 {
		l.MaxObservations = 10000
	}
	if l.MaxIssues <= 0 {
		l.MaxIssues = 64
	}
	if l.MaxIssues > 256 {
		l.MaxIssues = 256
	}
	return l
}

type collectResult struct {
	values []Observation
	err    error
}

// CollectAll runs every collector with an independent deadline. A failed or
// timed-out source does not erase observations from healthy sources. Global
// observation and error budgets keep a degraded host from producing an
// unbounded batch.
func CollectAll(ctx context.Context, collectors []Collector, limits CollectionLimits) CollectionBatch {
	limits = limits.normalized()
	batch := CollectionBatch{StartedAt: time.Now().UTC()}
	for _, collector := range collectors {
		if collector == nil {
			appendIssue(&batch, limits, CollectorIssue{Collector: "unknown", Error: "nil collector"})
			batch.CollectorsFailed++
			continue
		}
		if err := ctx.Err(); err != nil {
			appendIssue(&batch, limits, CollectorIssue{Collector: collector.Name(), Error: err.Error()})
			batch.CollectorsFailed++
			break
		}
		batch.CollectorsRun++
		collectorCtx, cancel := context.WithTimeout(ctx, limits.PerCollectorTimeout)
		result := make(chan collectResult, 1)
		go func() {
			values, err := collector.Collect(collectorCtx)
			result <- collectResult{values: values, err: err}
		}()
		var collected collectResult
		select {
		case collected = <-result:
		case <-collectorCtx.Done():
			collected.err = collectorCtx.Err()
		}
		timedOut := errors.Is(collected.err, context.DeadlineExceeded)
		cancel()
		for _, value := range collected.values {
			if len(batch.Observations) >= limits.MaxObservations {
				batch.Truncated = true
				break
			}
			if err := validateObservation(value); err != nil {
				appendIssue(&batch, limits, CollectorIssue{Collector: collector.Name(), Error: err.Error()})
				continue
			}
			batch.Observations = append(batch.Observations, value)
		}
		if collected.err != nil {
			batch.CollectorsFailed++
			appendIssue(&batch, limits, CollectorIssue{Collector: collector.Name(), Error: collected.err.Error(), Timeout: timedOut})
		}
	}
	sort.SliceStable(batch.Observations, func(i, j int) bool {
		if batch.Observations[i].Timestamp.Equal(batch.Observations[j].Timestamp) {
			return batch.Observations[i].ObservationID < batch.Observations[j].ObservationID
		}
		return batch.Observations[i].Timestamp.Before(batch.Observations[j].Timestamp)
	})
	batch.CompletedAt = time.Now().UTC()
	return batch
}

func appendIssue(batch *CollectionBatch, limits CollectionLimits, issue CollectorIssue) {
	if len(batch.Issues) >= limits.MaxIssues {
		batch.IssuesTruncated = true
		return
	}
	batch.Issues = append(batch.Issues, issue)
}

func validateObservation(value Observation) error {
	if strings.TrimSpace(value.ObservationID) == "" || strings.TrimSpace(value.Source) == "" ||
		strings.TrimSpace(value.SourceType) == "" || value.Timestamp.IsZero() ||
		strings.TrimSpace(value.Host) == "" || strings.TrimSpace(value.ResourceType) == "" ||
		strings.TrimSpace(value.ResourceID) == "" || strings.TrimSpace(value.MetricName) == "" ||
		strings.TrimSpace(value.Unit) == "" || strings.TrimSpace(value.Severity) == "" ||
		strings.TrimSpace(value.RawReference) == "" {
		return fmt.Errorf("collector %q returned an observation with missing required fields", value.Source)
	}
	return nil
}

func hostName() string {
	return platform.Hostname()
}

func observation(source, sourceType, resourceType, resourceID, metric string, value any, unit, severity, rawReference string, labels map[string]string, timestamp time.Time) Observation {
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	} else {
		timestamp = timestamp.UTC()
	}
	identity := strings.Join([]string{source, sourceType, resourceType, resourceID, metric, timestamp.Format(time.RFC3339Nano)}, "\x00")
	digest := sha256.Sum256([]byte(identity))
	return Observation{
		ObservationID: hex.EncodeToString(digest[:16]),
		Source:        source, SourceType: sourceType, Timestamp: timestamp, Host: hostName(),
		ResourceType: resourceType, ResourceID: resourceID, MetricName: metric,
		Value: value, Unit: unit, Severity: severity, Labels: labels, RawReference: rawReference,
	}
}

type ProcessReader interface {
	Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error)
}

type ProcfsCollector struct {
	Platform     platform.Platform
	Processes    ProcessReader
	ProcessLimit int
}

func (c ProcfsCollector) Name() string { return "procfs" }

func (c ProcfsCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("procfs platform is required")
	}
	now := time.Now().UTC()
	host := hostName()
	makeHost := func(metric string, value any, unit, ref string) Observation {
		return observation(c.Name(), "linux_procfs", "host", host, metric, value, unit, "info", ref, nil, now)
	}
	var out []Observation
	var issues []error
	if cpu, err := c.Platform.CPU(ctx); err != nil {
		issues = append(issues, fmt.Errorf("cpu: %w", err))
	} else {
		out = append(out,
			makeHost("cpu_busy_ticks", cpu.Busy, "ticks", "/proc/stat"),
			makeHost("cpu_total_ticks", cpu.Total, "ticks", "/proc/stat"),
		)
	}
	if mem, err := c.Platform.Memory(ctx); err != nil {
		issues = append(issues, fmt.Errorf("memory: %w", err))
	} else {
		out = append(out,
			makeHost("memory_used_bytes", mem.UsedBytes, "bytes", "/proc/meminfo"),
			makeHost("memory_available_bytes", mem.AvailableBytes, "bytes", "/proc/meminfo"),
			makeHost("swap_used_bytes", mem.SwapTotalBytes-mem.SwapFreeBytes, "bytes", "/proc/meminfo"),
		)
	}
	if load, err := c.Platform.Load(ctx); err != nil {
		issues = append(issues, fmt.Errorf("load: %w", err))
	} else {
		out = append(out,
			makeHost("load_1", load.One, "ratio", "/proc/loadavg"),
			makeHost("load_5", load.Five, "ratio", "/proc/loadavg"),
			makeHost("load_15", load.Fifteen, "ratio", "/proc/loadavg"),
			makeHost("processes_running", load.RunningProcesses, "count", "/proc/loadavg"),
		)
	}
	reader := c.Processes
	if reader == nil {
		reader, _ = c.Platform.(ProcessReader)
	}
	if reader != nil {
		limit := c.ProcessLimit
		if limit <= 0 || limit > 100 {
			limit = 25
		}
		processes, err := reader.Processes(ctx, platform.ProcessQuery{Limit: limit, SortBy: "cpu"})
		if err != nil {
			issues = append(issues, fmt.Errorf("processes: %w", err))
		} else {
			for _, process := range processes {
				resourceID := fmt.Sprintf("pid:%d:start:%d", process.PID, process.StartTicks)
				labels := map[string]string{"name": process.Name, "state": process.State, "uid": fmt.Sprint(process.UID)}
				out = append(out,
					observation(c.Name(), "linux_procfs", "process", resourceID, "process_cpu_ticks", process.CPUTicks, "ticks", "info", fmt.Sprintf("/proc/%d/stat", process.PID), labels, now),
					observation(c.Name(), "linux_procfs", "process", resourceID, "process_rss_bytes", process.RSSBytes, "bytes", "info", fmt.Sprintf("/proc/%d/stat", process.PID), labels, now),
				)
			}
		}
	}
	return out, errors.Join(issues...)
}
