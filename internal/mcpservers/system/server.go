package system

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
)

const Version = "0.1.0"

type EmptyInput struct{}
type DiskInput struct {
	Path string `json:"path,omitempty" jsonschema:"absolute filesystem path; defaults to /"`
}

type CPUOutput struct {
	CPU            platform.CPUStat `json:"cpu"`
	UsagePercent   float64          `json:"usage_percent"`
	SampleWindowMS int64            `json:"sample_window_ms"`
}
type MemoryOutput struct {
	Memory       platform.MemoryStat `json:"memory"`
	UsagePercent float64             `json:"usage_percent"`
}
type LoadOutput struct {
	Load platform.LoadAverage `json:"load"`
}
type DiskOutput struct {
	Disk platform.DiskUsage `json:"disk"`
}
type MountsOutput struct {
	Mounts []platform.Mount `json:"mounts"`
}
type KernelOutput struct {
	Kernel platform.KernelInfo `json:"kernel"`
}
type UptimeOutput struct {
	Seconds float64 `json:"seconds"`
	Human   string  `json:"human"`
}
type OverviewOutput struct {
	CPU           platform.CPUStat     `json:"cpu"`
	Memory        platform.MemoryStat  `json:"memory"`
	Load          platform.LoadAverage `json:"load"`
	RootDisk      platform.DiskUsage   `json:"root_disk"`
	Kernel        platform.KernelInfo  `json:"kernel"`
	UptimeSeconds float64              `json:"uptime_seconds"`
}

func New(p platform.Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-system", Version: Version}, nil)
	mcp.AddTool(s, readTool("system.get_cpu_metrics", "读取真实 /proc/stat CPU 累计计数", "cpu"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, CPUOutput, error) {
		first, err := p.CPU(ctx)
		if err != nil {
			return nil, CPUOutput{}, err
		}
		const sampleWindow = 200 * time.Millisecond
		timer := time.NewTimer(sampleWindow)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, CPUOutput{}, ctx.Err()
		case <-timer.C:
		}
		second, err := p.CPU(ctx)
		if err != nil {
			return nil, CPUOutput{}, err
		}
		usage := 0.0
		if second.Total > first.Total && second.Busy >= first.Busy {
			usage = 100 * float64(second.Busy-first.Busy) / float64(second.Total-first.Total)
		}
		return &mcp.CallToolResult{}, CPUOutput{CPU: second, UsagePercent: usage, SampleWindowMS: sampleWindow.Milliseconds()}, nil
	})
	mcp.AddTool(s, readTool("system.get_memory_metrics", "读取真实 /proc/meminfo 内存数据", "memory"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, MemoryOutput, error) {
		v, err := p.Memory(ctx)
		usage := 0.0
		if v.TotalBytes > 0 {
			usage = 100 * float64(v.UsedBytes) / float64(v.TotalBytes)
		}
		return &mcp.CallToolResult{}, MemoryOutput{Memory: v, UsagePercent: usage}, err
	})
	mcp.AddTool(s, readTool("system.get_load_average", "读取真实 /proc/loadavg 负载数据", "load"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, LoadOutput, error) {
		v, err := p.Load(ctx)
		return &mcp.CallToolResult{}, LoadOutput{Load: v}, err
	})
	mcp.AddTool(s, readTool("system.get_disk_usage", "读取指定绝对路径所在文件系统容量", "disk"), func(ctx context.Context, _ *mcp.CallToolRequest, in DiskInput) (*mcp.CallToolResult, DiskOutput, error) {
		path := in.Path
		if path == "" {
			path = "/"
		}
		if !filepath.IsAbs(path) {
			return nil, DiskOutput{}, errors.New("path must be absolute")
		}
		v, err := p.Disk(ctx, path)
		return &mcp.CallToolResult{}, DiskOutput{Disk: v}, err
	})
	mcp.AddTool(s, readTool("system.get_mounts", "读取真实 /proc/self/mounts 挂载信息", "mount"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, MountsOutput, error) {
		v, err := p.Mounts(ctx)
		return &mcp.CallToolResult{}, MountsOutput{Mounts: v}, err
	})
	mcp.AddTool(s, readTool("system.get_kernel_info", "读取内核、架构、主机及 OS Release", "kernel"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, KernelOutput, error) {
		v, err := p.Kernel(ctx)
		return &mcp.CallToolResult{}, KernelOutput{Kernel: v}, err
	})
	mcp.AddTool(s, readTool("system.get_uptime", "读取真实 /proc/uptime", "uptime"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, UptimeOutput, error) {
		v, err := p.Uptime(ctx)
		return &mcp.CallToolResult{}, UptimeOutput{Seconds: v.Seconds(), Human: v.Round(time.Second).String()}, err
	})
	mcp.AddTool(s, readTool("system.get_overview", "一次读取 CPU、内存、负载、根文件系统、内核与运行时间", "overview"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, OverviewOutput, error) {
		cpu, err := p.CPU(ctx)
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("cpu: %w", err)
		}
		memory, err := p.Memory(ctx)
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("memory: %w", err)
		}
		load, err := p.Load(ctx)
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("load: %w", err)
		}
		disk, err := p.Disk(ctx, "/")
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("disk: %w", err)
		}
		kernel, err := p.Kernel(ctx)
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("kernel: %w", err)
		}
		uptime, err := p.Uptime(ctx)
		if err != nil {
			return nil, OverviewOutput{}, fmt.Errorf("uptime: %w", err)
		}
		return &mcp.CallToolResult{}, OverviewOutput{CPU: cpu, Memory: memory, Load: load, RootDisk: disk, Kernel: kernel, UptimeSeconds: uptime.Seconds()}, nil
	})
	return s
}

func readTool(name, description, resource string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "system", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.resource": resource, "safeops.timeout_seconds": 10}}
}
