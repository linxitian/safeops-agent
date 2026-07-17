package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var sysctlKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+){1,7}$`)

type LinuxPlatform struct {
	procRoot                  string
	etcRoot                   string
	now                       func() time.Time
	processExecutableFallback ProcessExecutableFallback
}

type LinuxOption func(*LinuxPlatform)

func WithRoots(procRoot, etcRoot string) LinuxOption {
	return func(p *LinuxPlatform) { p.procRoot, p.etcRoot = procRoot, etcRoot }
}

// WithProcessExecutableFallback configures the privileged executor's narrow
// fallback for Linux /proc/PID/exe permission checks. Read-only MCP servers do
// not configure this option.
func WithProcessExecutableFallback(fallback ProcessExecutableFallback) LinuxOption {
	return func(p *LinuxPlatform) { p.processExecutableFallback = fallback }
}

func NewLinux(opts ...LinuxOption) *LinuxPlatform {
	p := &LinuxPlatform{procRoot: "/proc", etcRoot: "/etc", now: time.Now}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func Hostname() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "unknown"
	}
	return host
}

func (p *LinuxPlatform) readContext(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return b, nil
}

func (p *LinuxPlatform) CPU(ctx context.Context) (CPUStat, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "stat"))
	if err != nil {
		return CPUStat{}, err
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return CPUStat{}, errors.New("/proc/stat: aggregate cpu row missing")
	}
	values := make([]uint64, 8)
	for i := range values {
		if i+1 >= len(f) {
			break
		}
		values[i], err = strconv.ParseUint(f[i+1], 10, 64)
		if err != nil {
			return CPUStat{}, fmt.Errorf("/proc/stat cpu field %d: %w", i, err)
		}
	}
	total := uint64(0)
	for _, v := range values {
		total += v
	}
	idle := values[3] + values[4]
	return CPUStat{User: values[0], Nice: values[1], System: values[2], Idle: values[3], IOWait: values[4], IRQ: values[5], SoftIRQ: values[6], Steal: values[7], Total: total, Busy: total - idle, Collected: p.now().UTC()}, nil
}

func (p *LinuxPlatform) Memory(ctx context.Context) (MemoryStat, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "meminfo"))
	if err != nil {
		return MemoryStat{}, err
	}
	values := map[string]uint64{}
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 2 {
			continue
		}
		v, parseErr := strconv.ParseUint(f[1], 10, 64)
		if parseErr != nil {
			continue
		}
		values[strings.TrimSuffix(f[0], ":")] = v * 1024
	}
	if err := s.Err(); err != nil {
		return MemoryStat{}, err
	}
	total := values["MemTotal"]
	if total == 0 {
		return MemoryStat{}, errors.New("/proc/meminfo: MemTotal missing")
	}
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if available > total {
		available = total
	}
	return MemoryStat{TotalBytes: total, AvailableBytes: available, UsedBytes: total - available, FreeBytes: values["MemFree"], BuffersBytes: values["Buffers"], CachedBytes: values["Cached"], SwapTotalBytes: values["SwapTotal"], SwapFreeBytes: values["SwapFree"], Collected: p.now().UTC()}, nil
}

func (p *LinuxPlatform) Load(ctx context.Context) (LoadAverage, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "loadavg"))
	if err != nil {
		return LoadAverage{}, err
	}
	f := strings.Fields(string(b))
	if len(f) < 5 {
		return LoadAverage{}, errors.New("/proc/loadavg: invalid field count")
	}
	one, e1 := strconv.ParseFloat(f[0], 64)
	five, e2 := strconv.ParseFloat(f[1], 64)
	fifteen, e3 := strconv.ParseFloat(f[2], 64)
	if err := errors.Join(e1, e2, e3); err != nil {
		return LoadAverage{}, fmt.Errorf("/proc/loadavg: %w", err)
	}
	run, totalText, ok := strings.Cut(f[3], "/")
	if !ok {
		return LoadAverage{}, errors.New("/proc/loadavg: invalid process counts")
	}
	running, e4 := strconv.ParseUint(run, 10, 64)
	total, e5 := strconv.ParseUint(totalText, 10, 64)
	lastPID, e6 := strconv.ParseUint(f[4], 10, 64)
	if err := errors.Join(e4, e5, e6); err != nil {
		return LoadAverage{}, fmt.Errorf("/proc/loadavg: %w", err)
	}
	return LoadAverage{One: one, Five: five, Fifteen: fifteen, RunningProcesses: running, TotalProcesses: total, LastPID: lastPID, Collected: p.now().UTC()}, nil
}

func (p *LinuxPlatform) Disk(ctx context.Context, path string) (DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return DiskUsage{}, err
	}
	clean := filepath.Clean(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(clean, &st); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", clean, err)
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - free
	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
	}
	return DiskUsage{Path: clean, TotalBytes: total, FreeBytes: free, UsedBytes: used, UsedRatio: ratio}, nil
}

func (p *LinuxPlatform) Mounts(ctx context.Context) ([]Mount, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "self", "mounts"))
	if err != nil {
		return nil, err
	}
	var out []Mount
	s := bufio.NewScanner(strings.NewReader(string(b)))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 4 {
			continue
		}
		out = append(out, Mount{Source: unescapeMount(f[0]), Target: unescapeMount(f[1]), Filesystem: f[2], Options: f[3]})
	}
	return out, s.Err()
}

func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func (p *LinuxPlatform) Kernel(ctx context.Context) (KernelInfo, error) {
	if err := ctx.Err(); err != nil {
		return KernelInfo{}, err
	}
	host, err := os.Hostname()
	if err != nil {
		return KernelInfo{}, err
	}
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil {
		return KernelInfo{}, fmt.Errorf("uname: %w", err)
	}
	info := KernelInfo{OS: runtime.GOOS, Architecture: runtime.GOARCH, Kernel: chars(u.Release[:]), Hostname: host}
	if f, err := os.Open(filepath.Join(p.etcRoot, "os-release")); err == nil {
		defer f.Close()
		parseOSRelease(f, &info)
	}
	return info, nil
}

func chars(in []int8) string {
	b := make([]byte, 0, len(in))
	for _, c := range in {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func parseOSRelease(r io.Reader, out *KernelInfo) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		key, value, ok := strings.Cut(s.Text(), "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "PRETTY_NAME":
			out.OSName = value
		case "VERSION_ID":
			out.OSVersion = value
		}
	}
}

func (p *LinuxPlatform) Uptime(ctx context.Context) (time.Duration, error) {
	b, err := p.readContext(ctx, filepath.Join(p.procRoot, "uptime"))
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, errors.New("/proc/uptime: empty")
	}
	seconds, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/uptime: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// Sysctls reads only caller-selected, syntactically fixed /proc/sys keys.
// Collectors supply an explicit developer-authored allowlist; values are
// bounded and command execution is never involved.
func (p *LinuxPlatform) Sysctls(ctx context.Context, keys []string) ([]SysctlSetting, error) {
	if len(keys) > 64 {
		return nil, errors.New("selected sysctl keys must not exceed 64")
	}
	seen := map[string]bool{}
	settings := make([]SysctlSetting, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return settings, err
		}
		key = strings.TrimSpace(key)
		if !sysctlKeyPattern.MatchString(key) {
			return settings, fmt.Errorf("invalid selected sysctl key %q", key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		value, err := p.readContext(ctx, filepath.Join(p.procRoot, "sys", filepath.FromSlash(strings.ReplaceAll(key, ".", "/"))))
		if err != nil {
			return settings, fmt.Errorf("read selected sysctl %s: %w", key, err)
		}
		text := strings.TrimSpace(string(value))
		if len(text) > 4096 {
			return settings, fmt.Errorf("selected sysctl %s exceeds 4096 bytes", key)
		}
		settings = append(settings, SysctlSetting{Key: key, Value: text})
	}
	return settings, nil
}
