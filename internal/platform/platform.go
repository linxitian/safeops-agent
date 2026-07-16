// Package platform is the only package allowed to access operating-system
// primitives. Callers consume structured values rather than command output.
package platform

import (
	"context"
	"time"
)

type CPUStat struct {
	User      uint64    `json:"user_ticks"`
	Nice      uint64    `json:"nice_ticks"`
	System    uint64    `json:"system_ticks"`
	Idle      uint64    `json:"idle_ticks"`
	IOWait    uint64    `json:"iowait_ticks"`
	IRQ       uint64    `json:"irq_ticks"`
	SoftIRQ   uint64    `json:"softirq_ticks"`
	Steal     uint64    `json:"steal_ticks"`
	Total     uint64    `json:"total_ticks"`
	Busy      uint64    `json:"busy_ticks"`
	Collected time.Time `json:"collected_at"`
}

type MemoryStat struct {
	TotalBytes     uint64    `json:"total_bytes"`
	AvailableBytes uint64    `json:"available_bytes"`
	UsedBytes      uint64    `json:"used_bytes"`
	FreeBytes      uint64    `json:"free_bytes"`
	BuffersBytes   uint64    `json:"buffers_bytes"`
	CachedBytes    uint64    `json:"cached_bytes"`
	SwapTotalBytes uint64    `json:"swap_total_bytes"`
	SwapFreeBytes  uint64    `json:"swap_free_bytes"`
	Collected      time.Time `json:"collected_at"`
}

type LoadAverage struct {
	One              float64   `json:"load_1"`
	Five             float64   `json:"load_5"`
	Fifteen          float64   `json:"load_15"`
	RunningProcesses uint64    `json:"running_processes"`
	TotalProcesses   uint64    `json:"total_processes"`
	LastPID          uint64    `json:"last_pid"`
	Collected        time.Time `json:"collected_at"`
}

type DiskUsage struct {
	Path       string  `json:"path"`
	TotalBytes uint64  `json:"total_bytes"`
	FreeBytes  uint64  `json:"free_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	UsedRatio  float64 `json:"used_ratio"`
}

type Mount struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Filesystem string `json:"filesystem"`
	Options    string `json:"options"`
}

type KernelInfo struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Kernel       string `json:"kernel"`
	Hostname     string `json:"hostname"`
	OSName       string `json:"os_name,omitempty"`
	OSVersion    string `json:"os_version,omitempty"`
}

type SysctlSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Platform centralizes all OS reads used by collectors and MCP servers.
type Platform interface {
	CPU(context.Context) (CPUStat, error)
	Memory(context.Context) (MemoryStat, error)
	Load(context.Context) (LoadAverage, error)
	Disk(context.Context, string) (DiskUsage, error)
	Mounts(context.Context) ([]Mount, error)
	Kernel(context.Context) (KernelInfo, error)
	Uptime(context.Context) (time.Duration, error)
}
