package perception

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/safefs"
)

type DiskPlatform interface {
	Disk(context.Context, string) (platform.DiskUsage, error)
	Mounts(context.Context) ([]platform.Mount, error)
}

type DiskCollector struct {
	Platform       DiskPlatform
	Reader         *safefs.Reader
	Paths          []string
	MaxMounts      int
	MaxDepth       int
	MaxEntries     int
	LargeFileMin   int64
	LargeFileLimit int
}

func (DiskCollector) Name() string { return "disk" }

func (c DiskCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("disk platform is required")
	}
	now := time.Now().UTC()
	maxMounts := c.MaxMounts
	if maxMounts <= 0 || maxMounts > 128 {
		maxMounts = 64
	}
	var out []Observation
	var issues []error
	mounts, err := c.Platform.Mounts(ctx)
	if err != nil {
		issues = append(issues, fmt.Errorf("mounts: %w", err))
	} else {
		if len(mounts) > maxMounts {
			mounts = mounts[:maxMounts]
			issues = append(issues, fmt.Errorf("mount list truncated at %d entries", maxMounts))
		}
		for _, mount := range mounts {
			labels := map[string]string{"source": mount.Source, "filesystem": mount.Filesystem, "options": mount.Options}
			out = append(out, observation(c.Name(), "linux_filesystem", "mount", mount.Target, "filesystem_mount", mount.Filesystem, "state", "info", "/proc/self/mounts", labels, now))
		}
	}
	targets := append([]string(nil), c.Paths...)
	if len(targets) == 0 {
		for _, mount := range mounts {
			targets = append(targets, mount.Target)
		}
	}
	targets = uniqueBounded(targets, maxMounts)
	for _, target := range targets {
		usage, err := c.Platform.Disk(ctx, target)
		if err != nil {
			issues = append(issues, fmt.Errorf("disk %s: %w", target, err))
			continue
		}
		out = append(out,
			observation(c.Name(), "linux_statfs", "filesystem", usage.Path, "filesystem_total_bytes", usage.TotalBytes, "bytes", diskSeverity(usage.UsedRatio), "statfs:"+usage.Path, nil, now),
			observation(c.Name(), "linux_statfs", "filesystem", usage.Path, "filesystem_used_bytes", usage.UsedBytes, "bytes", diskSeverity(usage.UsedRatio), "statfs:"+usage.Path, nil, now),
			observation(c.Name(), "linux_statfs", "filesystem", usage.Path, "filesystem_used_ratio", usage.UsedRatio, "ratio", diskSeverity(usage.UsedRatio), "statfs:"+usage.Path, nil, now),
		)
	}
	if c.Reader != nil {
		paths := c.Paths
		if len(paths) == 0 {
			paths = c.Reader.Roots()
		}
		depth := c.MaxDepth
		if depth <= 0 || depth > 16 {
			depth = 4
		}
		entries := c.MaxEntries
		if entries <= 0 || entries > 20000 {
			entries = 5000
		}
		minimum := c.LargeFileMin
		if minimum <= 0 {
			minimum = 10 << 20
		}
		largeLimit := c.LargeFileLimit
		if largeLimit <= 0 || largeLimit > 200 {
			largeLimit = 20
		}
		for _, root := range uniqueBounded(paths, 32) {
			usage, err := c.Reader.Usage(ctx, root, depth, entries)
			if err != nil {
				issues = append(issues, fmt.Errorf("directory usage %s: %w", root, err))
			} else {
				labels := map[string]string{"truncated": strconv.FormatBool(usage.Truncated), "skipped": strconv.Itoa(usage.Skipped)}
				out = append(out,
					observation(c.Name(), "safefs_metadata", "directory", root, "directory_size_bytes", usage.SizeBytes, "bytes", "info", "safefs:usage:"+root, labels, now),
					observation(c.Name(), "safefs_metadata", "directory", root, "directory_file_count", usage.Files, "count", "info", "safefs:usage:"+root, labels, now),
				)
			}
			large, truncated, err := c.Reader.FindLarge(ctx, root, minimum, depth, largeLimit)
			if err != nil {
				issues = append(issues, fmt.Errorf("large files %s: %w", root, err))
				continue
			}
			for _, file := range large {
				out = append(out, observation(c.Name(), "safefs_metadata", "file", file.Path, "large_file_size_bytes", file.SizeBytes, "bytes", "warning", "safefs:metadata:"+file.Path, map[string]string{"scan_truncated": strconv.FormatBool(truncated)}, now))
			}
		}
	}
	return out, errors.Join(issues...)
}

func diskSeverity(ratio float64) string {
	switch {
	case ratio >= 0.95:
		return "critical"
	case ratio >= 0.85:
		return "warning"
	default:
		return "info"
	}
}

type NetworkReader interface {
	Sockets(context.Context, bool, int) ([]platform.SocketInfo, error)
	Interfaces(context.Context) ([]platform.InterfaceInfo, error)
}

type NetworkCollector struct {
	Platform    NetworkReader
	SocketLimit int
}

func (NetworkCollector) Name() string { return "network" }

func (c NetworkCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("network platform is required")
	}
	now := time.Now().UTC()
	limit := c.SocketLimit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []Observation
	var issues []error
	sockets, err := c.Platform.Sockets(ctx, false, limit)
	if err != nil {
		issues = append(issues, fmt.Errorf("sockets: %w", err))
	} else {
		for _, socket := range sockets {
			resourceID := fmt.Sprintf("%s:%s:%d:%s", socket.Protocol, socket.LocalAddress, socket.LocalPort, socket.Inode)
			labels := map[string]string{"protocol": socket.Protocol, "local_address": socket.LocalAddress, "remote_address": socket.RemoteAddress, "remote_port": strconv.Itoa(socket.RemotePort), "state": socket.State, "inode": socket.Inode}
			out = append(out, observation(c.Name(), "linux_proc_net", "socket", resourceID, "connection_state", socket.State, "state", "info", "/proc/net/"+strings.TrimSuffix(socket.Protocol, "6"), labels, now))
			if socket.Listening {
				out = append(out, observation(c.Name(), "linux_proc_net", "port", fmt.Sprintf("%s:%d", socket.Protocol, socket.LocalPort), "listening_port", socket.LocalPort, "port", "info", "/proc/net/"+strings.TrimSuffix(socket.Protocol, "6"), labels, now))
			}
		}
	}
	interfaces, err := c.Platform.Interfaces(ctx)
	if err != nil {
		issues = append(issues, fmt.Errorf("interfaces: %w", err))
	} else {
		for _, value := range interfaces {
			labels := map[string]string{"addresses": strings.Join(value.Addresses, ","), "flags": strings.Join(value.Flags, ","), "mtu": strconv.Itoa(value.MTU)}
			out = append(out,
				observation(c.Name(), "linux_interface", "interface", value.Name, "network_receive_bytes", value.RXBytes, "bytes", "info", "/proc/net/dev", labels, now),
				observation(c.Name(), "linux_interface", "interface", value.Name, "network_transmit_bytes", value.TXBytes, "bytes", "info", "/proc/net/dev", labels, now),
			)
		}
	}
	return out, errors.Join(issues...)
}

type SystemdReader interface {
	Service(context.Context, string) (platform.ServiceStatus, error)
	FailedServices(context.Context) ([]platform.ServiceStatus, error)
	ServiceDependencies(context.Context, string) (platform.ServiceDependencies, error)
}

type SystemdCollector struct {
	Platform SystemdReader
	Units    []string
	MaxUnits int
}

func (SystemdCollector) Name() string { return "systemd" }

func (c SystemdCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("systemd platform is required")
	}
	now := time.Now().UTC()
	maxUnits := c.MaxUnits
	if maxUnits <= 0 || maxUnits > 100 {
		maxUnits = 32
	}
	var out []Observation
	var issues []error
	failed, err := c.Platform.FailedServices(ctx)
	if err != nil {
		issues = append(issues, fmt.Errorf("failed services: %w", err))
	}
	units := append([]string(nil), c.Units...)
	for _, service := range failed {
		units = append(units, service.Name)
	}
	units = uniqueBounded(units, maxUnits)
	failedNames := map[string]bool{}
	for _, service := range failed {
		failedNames[service.Name] = true
	}
	for _, unit := range units {
		status, err := c.Platform.Service(ctx, unit)
		if err != nil {
			issues = append(issues, fmt.Errorf("service %s: %w", unit, err))
			continue
		}
		severity := "info"
		if status.ActiveState == "failed" || failedNames[status.Name] {
			severity = "critical"
		} else if status.ActiveState != "active" {
			severity = "warning"
		}
		labels := map[string]string{"load_state": status.LoadState, "active_state": status.ActiveState, "sub_state": status.SubState, "description": status.Description}
		out = append(out,
			observation(c.Name(), "systemd_dbus_cli", "service", status.Name, "service_state", status.ActiveState+"/"+status.SubState, "state", severity, "systemctl:show:"+status.Name, labels, now),
			observation(c.Name(), "systemd_dbus_cli", "service", status.Name, "service_main_pid", status.MainPID, "pid", severity, "systemctl:show:"+status.Name, labels, now),
			observation(c.Name(), "systemd_dbus_cli", "service", status.Name, "service_restart_count", status.RestartCount, "count", severity, "systemctl:show:"+status.Name, labels, now),
		)
		dependencies, err := c.Platform.ServiceDependencies(ctx, status.Name)
		if err != nil {
			issues = append(issues, fmt.Errorf("dependencies %s: %w", status.Name, err))
			continue
		}
		for relation, values := range map[string][]string{"requires": dependencies.Requires, "wants": dependencies.Wants, "after": dependencies.After, "before": dependencies.Before} {
			out = append(out, observation(c.Name(), "systemd_dbus_cli", "service", status.Name, "service_dependency_"+relation, values, "units", "info", "systemctl:show:"+status.Name, nil, now))
		}
	}
	return out, errors.Join(issues...)
}

type JournalReader interface {
	Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error)
}

type JournalCollector struct {
	Platform JournalReader
	Queries  []platform.JournalQuery
}

func (JournalCollector) Name() string { return "journal" }

func (c JournalCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("journal platform is required")
	}
	queries := append([]platform.JournalQuery(nil), c.Queries...)
	if len(queries) == 0 {
		queries = []platform.JournalQuery{{Lines: 100, Priority: 3}}
	}
	if len(queries) > 16 {
		return nil, errors.New("journal queries must not exceed 16")
	}
	var out []Observation
	var issues []error
	for _, query := range queries {
		events, err := c.Platform.Journal(ctx, query)
		if err != nil {
			issues = append(issues, fmt.Errorf("journal unit %q: %w", query.Unit, err))
			continue
		}
		for index, event := range events {
			resourceID := event.Cursor
			if resourceID == "" {
				resourceID = fmt.Sprintf("%s:%d:%d", event.Unit, event.PID, index)
			}
			labels := map[string]string{"unit": event.Unit, "pid": strconv.Itoa(event.PID), "priority": strconv.Itoa(event.Priority), "boot_id": event.BootID, "redacted": strconv.FormatBool(event.Redacted)}
			out = append(out, observation(c.Name(), "systemd_journal", "log_event", resourceID, "journal_message", event.Message, "text", journalSeverity(event.Priority), "journal:"+event.Cursor, labels, event.Timestamp))
		}
	}
	return out, errors.Join(issues...)
}

func journalSeverity(priority int) string {
	switch {
	case priority <= 2:
		return "critical"
	case priority <= 4:
		return "warning"
	default:
		return "info"
	}
}

type SysctlReader interface {
	Sysctls(context.Context, []string) ([]platform.SysctlSetting, error)
}

type SystemConfigCollector struct {
	Platform        platform.Platform
	Sysctls         SysctlReader
	SelectedSysctls []string
	MaxMounts       int
}

func (SystemConfigCollector) Name() string { return "system_config" }

func (c SystemConfigCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Platform == nil {
		return nil, errors.New("system config platform is required")
	}
	now := time.Now().UTC()
	host := hostName()
	var out []Observation
	var issues []error
	kernel, err := c.Platform.Kernel(ctx)
	if err != nil {
		issues = append(issues, fmt.Errorf("kernel: %w", err))
	} else {
		resourceID := kernel.Hostname
		if resourceID == "" {
			resourceID = host
		}
		for metric, value := range map[string]string{"os_name": kernel.OSName, "os_version": kernel.OSVersion, "kernel_release": kernel.Kernel, "architecture": kernel.Architecture} {
			out = append(out, observation(c.Name(), "linux_system_config", "host", resourceID, metric, value, "text", "info", "/etc/os-release+/proc/sys/kernel/osrelease", nil, now))
		}
	}
	if uptime, err := c.Platform.Uptime(ctx); err != nil {
		issues = append(issues, fmt.Errorf("uptime: %w", err))
	} else {
		out = append(out, observation(c.Name(), "linux_system_config", "host", host, "uptime_seconds", uptime.Seconds(), "seconds", "info", "/proc/uptime", nil, now))
	}
	maxMounts := c.MaxMounts
	if maxMounts <= 0 || maxMounts > 128 {
		maxMounts = 64
	}
	if mounts, err := c.Platform.Mounts(ctx); err != nil {
		issues = append(issues, fmt.Errorf("mounts: %w", err))
	} else {
		if len(mounts) > maxMounts {
			mounts = mounts[:maxMounts]
			issues = append(issues, fmt.Errorf("system config mount list truncated at %d", maxMounts))
		}
		for _, mount := range mounts {
			out = append(out, observation(c.Name(), "linux_system_config", "mount", mount.Target, "mount_filesystem", mount.Filesystem, "text", "info", "/proc/self/mounts", map[string]string{"source": mount.Source, "options": mount.Options}, now))
		}
	}
	reader := c.Sysctls
	if reader == nil {
		reader, _ = c.Platform.(SysctlReader)
	}
	keys := append([]string(nil), c.SelectedSysctls...)
	if len(keys) == 0 {
		keys = []string{"kernel.pid_max", "kernel.threads-max", "net.core.somaxconn", "vm.swappiness"}
	}
	if reader != nil {
		settings, err := reader.Sysctls(ctx, keys)
		if err != nil {
			issues = append(issues, fmt.Errorf("selected sysctls: %w", err))
		} else {
			for _, setting := range settings {
				out = append(out, observation(c.Name(), "linux_sysctl", "sysctl", setting.Key, "sysctl_value", setting.Value, "text", "info", "/proc/sys/"+strings.ReplaceAll(setting.Key, ".", "/"), nil, now))
			}
		}
	}
	return out, errors.Join(issues...)
}

type ConfigFingerprint struct {
	Modified  time.Time `json:"modified"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
}

type ConfigChangeCollector struct {
	Reader        *safefs.Reader
	Paths         []string
	Baseline      map[string]ConfigFingerprint
	Limit         int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

func (ConfigChangeCollector) Name() string { return "config_change" }

func (c ConfigChangeCollector) Collect(ctx context.Context) ([]Observation, error) {
	if c.Reader == nil {
		return nil, errors.New("config change allowlisted reader is required")
	}
	paths := append([]string(nil), c.Paths...)
	if len(paths) == 0 {
		paths = c.Reader.Roots()
	}
	paths = uniqueBounded(paths, 32)
	now := time.Now().UTC()
	seen := map[string]bool{}
	var out []Observation
	var issues []error
	for _, root := range paths {
		snapshot, err := c.Reader.Snapshot(ctx, root, c.Limit, c.MaxFileBytes, c.MaxTotalBytes)
		if err != nil {
			issues = append(issues, fmt.Errorf("config snapshot %s: %w", root, err))
			continue
		}
		for _, entry := range snapshot.Entries {
			path := filepath.Join(root, entry.RelativePath)
			seen[path] = true
			state := "added"
			if before, exists := c.Baseline[path]; exists {
				state = "unchanged"
				if before.SHA256 != entry.SHA256 || before.SizeBytes != entry.SizeBytes || !before.Modified.Equal(entry.Modified) {
					state = "modified"
				}
			}
			labels := map[string]string{"change": state, "snapshot_truncated": strconv.FormatBool(snapshot.Truncated)}
			out = append(out,
				observation(c.Name(), "safefs_config_snapshot", "config_file", path, "config_mtime_unix_seconds", float64(entry.Modified.UnixNano())/float64(time.Second), "seconds", changeSeverity(state), "safefs:snapshot:"+path, labels, now),
				observation(c.Name(), "safefs_config_snapshot", "config_file", path, "config_size_bytes", entry.SizeBytes, "bytes", changeSeverity(state), "safefs:snapshot:"+path, labels, now),
				observation(c.Name(), "safefs_config_snapshot", "config_file", path, "config_sha256", entry.SHA256, "sha256", changeSeverity(state), "safefs:snapshot:"+path, labels, now),
			)
		}
		if len(snapshot.Skipped) > 0 {
			issues = append(issues, fmt.Errorf("config snapshot %s skipped %d entries without reading their contents", root, len(snapshot.Skipped)))
		}
	}
	baselinePaths := make([]string, 0, len(c.Baseline))
	for path := range c.Baseline {
		baselinePaths = append(baselinePaths, path)
	}
	sort.Strings(baselinePaths)
	for _, path := range baselinePaths {
		if seen[path] || !underAnyPath(path, paths) {
			continue
		}
		out = append(out, observation(c.Name(), "safefs_config_snapshot", "config_file", path, "config_change_state", "removed", "state", "warning", "safefs:snapshot:"+path, nil, now))
	}
	return out, errors.Join(issues...)
}

func changeSeverity(state string) string {
	if state == "unchanged" {
		return "info"
	}
	return "warning"
}

func underAnyPath(path string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func uniqueBounded(values []string, limit int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}
