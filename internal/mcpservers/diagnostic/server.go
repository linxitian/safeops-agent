package diagnostic

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
	"safeops-agent/internal/rca"
	"safeops-agent/internal/retrieval"
)

const Version = "0.1.0"

type Provider interface {
	Service(context.Context, string) (platform.ServiceStatus, error)
	Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error)
	Sockets(context.Context, bool, int) ([]platform.SocketInfo, error)
	ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error)
	Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error)
	Load(context.Context) (platform.LoadAverage, error)
	Memory(context.Context) (platform.MemoryStat, error)
	Disk(context.Context, string) (platform.DiskUsage, error)
	Kernel(context.Context) (platform.KernelInfo, error)
}
type CombinedProvider struct {
	Linux    *platform.LinuxPlatform
	Commands *platform.CommandPlatform
}

func (p CombinedProvider) Service(ctx context.Context, unit string) (platform.ServiceStatus, error) {
	return p.Commands.Service(ctx, unit)
}
func (p CombinedProvider) Journal(ctx context.Context, q platform.JournalQuery) ([]platform.JournalEvent, error) {
	return p.Commands.Journal(ctx, q)
}
func (p CombinedProvider) Sockets(ctx context.Context, l bool, n int) ([]platform.SocketInfo, error) {
	return p.Linux.Sockets(ctx, l, n)
}
func (p CombinedProvider) ProcessesByPort(ctx context.Context, port int) ([]platform.ProcessInfo, error) {
	return p.Linux.ProcessesByPort(ctx, port)
}
func (p CombinedProvider) Processes(ctx context.Context, q platform.ProcessQuery) ([]platform.ProcessInfo, error) {
	return p.Linux.Processes(ctx, q)
}
func (p CombinedProvider) Load(ctx context.Context) (platform.LoadAverage, error) {
	return p.Linux.Load(ctx)
}
func (p CombinedProvider) Memory(ctx context.Context) (platform.MemoryStat, error) {
	return p.Linux.Memory(ctx)
}
func (p CombinedProvider) Disk(ctx context.Context, path string) (platform.DiskUsage, error) {
	return p.Linux.Disk(ctx, path)
}
func (p CombinedProvider) Kernel(ctx context.Context) (platform.KernelInfo, error) {
	return p.Linux.Kernel(ctx)
}

type PortInput struct {
	Unit string `json:"unit" jsonschema:"target systemd service"`
	Port int    `json:"port" jsonschema:"expected listen port from 1 to 65535"`
}
type HighCPUInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"top process count from 1 to 50"`
}
type DiskInput struct {
	Path         string  `json:"path,omitempty" jsonschema:"absolute path, defaults to /"`
	WarningRatio float64 `json:"warning_ratio,omitempty" jsonschema:"ratio between 0.5 and 1.0"`
}
type SnapshotInput struct {
	Unit string `json:"unit,omitempty"`
	Port int    `json:"port,omitempty"`
	Path string `json:"path,omitempty"`
}
type PortOutput struct {
	Diagnosis rca.PortConflictOutput  `json:"diagnosis"`
	Knowledge []retrieval.Result      `json:"knowledge"`
	Service   platform.ServiceStatus  `json:"service"`
	Logs      []platform.JournalEvent `json:"logs"`
	Sockets   []platform.SocketInfo   `json:"sockets"`
	Processes []platform.ProcessInfo  `json:"processes"`
}
type HighCPUOutput struct {
	Load      platform.LoadAverage   `json:"load"`
	Processes []platform.ProcessInfo `json:"processes"`
	RCA       rca.Result             `json:"rca"`
}
type DiskOutput struct {
	Disk    platform.DiskUsage `json:"disk"`
	Warning bool               `json:"warning"`
	RCA     rca.Result         `json:"rca"`
}
type SnapshotOutput struct {
	Kernel        platform.KernelInfo     `json:"kernel"`
	Load          platform.LoadAverage    `json:"load"`
	Memory        platform.MemoryStat     `json:"memory"`
	Disk          platform.DiskUsage      `json:"disk"`
	Service       *platform.ServiceStatus `json:"service,omitempty"`
	PortSockets   []platform.SocketInfo   `json:"port_sockets,omitempty"`
	PortProcesses []platform.ProcessInfo  `json:"port_processes,omitempty"`
}

func New(p Provider, retrievers ...retrieval.Retriever) *mcp.Server {
	var retriever retrieval.Retriever
	if len(retrievers) > 0 {
		retriever = retrievers[0]
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-diagnostic", Version: Version}, nil)
	mcp.AddTool(s, readTool("diagnostic.port_conflict", "关联服务状态、EADDRINUSE 日志、监听 socket 与占用进程，生成 D1-D3 RCA"), func(ctx context.Context, _ *mcp.CallToolRequest, in PortInput) (*mcp.CallToolResult, PortOutput, error) {
		if in.Unit == "" || in.Port < 1 || in.Port > 65535 {
			return nil, PortOutput{}, errors.New("valid unit and port are required")
		}
		service, err := p.Service(ctx, in.Unit)
		if err != nil {
			return nil, PortOutput{}, err
		}
		logs, err := p.Journal(ctx, platform.JournalQuery{Unit: in.Unit, Lines: 200, Priority: -1})
		if err != nil {
			return nil, PortOutput{}, err
		}
		all, err := p.Sockets(ctx, false, 5000)
		if err != nil {
			return nil, PortOutput{}, err
		}
		var sockets []platform.SocketInfo
		for _, socket := range all {
			if socket.LocalPort == in.Port {
				sockets = append(sockets, socket)
			}
		}
		processes, err := p.ProcessesByPort(ctx, in.Port)
		if err != nil {
			return nil, PortOutput{}, err
		}
		var knowledge []retrieval.Result
		caseSimilarity := 0.0
		if retriever != nil {
			queryParts := []string{in.Unit, fmt.Sprintf("port %d 端口冲突", in.Port)}
			for _, event := range logs {
				queryParts = append(queryParts, event.Message)
			}
			knowledge, err = retriever.Search(ctx, strings.Join(queryParts, " "), 3)
			if err != nil {
				return nil, PortOutput{}, fmt.Errorf("retrieve diagnostic knowledge: %w", err)
			}
			if len(knowledge) > 0 {
				// Convert an unbounded BM25 score to a deterministic [0,1) component.
				caseSimilarity = math.Round((1-math.Exp(-knowledge[0].Score))*1000) / 1000
			}
		}
		diagnosis := rca.DiagnosePortConflict(rca.PortConflictInput{Service: service, Port: in.Port, Logs: logs, Sockets: sockets, Processes: processes, CaseSimilarity: caseSimilarity})
		return &mcp.CallToolResult{}, PortOutput{Diagnosis: diagnosis, Knowledge: knowledge, Service: service, Logs: logs, Sockets: sockets, Processes: processes}, nil
	})
	mcp.AddTool(s, readTool("diagnostic.high_cpu", "关联负载与按 CPU ticks 排序的进程，保守输出候选原因"), func(ctx context.Context, _ *mcp.CallToolRequest, in HighCPUInput) (*mcp.CallToolResult, HighCPUOutput, error) {
		limit := in.Limit
		if limit == 0 {
			limit = 10
		}
		if limit < 1 || limit > 50 {
			return nil, HighCPUOutput{}, errors.New("limit must be between 1 and 50")
		}
		load, err := p.Load(ctx)
		if err != nil {
			return nil, HighCPUOutput{}, err
		}
		processes, err := p.Processes(ctx, platform.ProcessQuery{Limit: limit, SortBy: "cpu"})
		if err != nil {
			return nil, HighCPUOutput{}, err
		}
		components := rca.ConfidenceComponents{SignalMatch: 0.5, GraphConsistency: 0.5}
		result := rca.Result{DiagnosisLevel: rca.D2, Confidence: components.Score(), ConfidenceComponents: components, EvidenceRefs: []string{"/proc/loadavg", "/proc/<pid>/stat"}, MissingEvidence: []string{"进程采样窗口 CPU 百分比", "相关服务日志"}, Remediation: []string{"继续采集进程采样窗口和关联服务日志"}}
		if len(processes) > 0 {
			result.Culprit = fmt.Sprintf("pid:%d:start:%d", processes[0].PID, processes[0].StartTicks)
			result.CandidateCauses = []rca.CandidateCause{{Cause: "累计 CPU ticks 较高的进程是候选对象，尚不能仅凭累计值确认根因", Score: result.Confidence, EvidenceRefs: result.EvidenceRefs}}
		}
		return &mcp.CallToolResult{}, HighCPUOutput{Load: load, Processes: processes, RCA: result}, nil
	})
	mcp.AddTool(s, readTool("diagnostic.disk_pressure", "读取真实 statfs 并判断容量压力，根因未知时不伪造"), func(ctx context.Context, _ *mcp.CallToolRequest, in DiskInput) (*mcp.CallToolResult, DiskOutput, error) {
		path := in.Path
		if path == "" {
			path = "/"
		}
		if !filepath.IsAbs(path) {
			return nil, DiskOutput{}, errors.New("path must be absolute")
		}
		threshold := in.WarningRatio
		if threshold == 0 {
			threshold = .85
		}
		if threshold < .5 || threshold > 1 {
			return nil, DiskOutput{}, errors.New("warning_ratio must be between 0.5 and 1.0")
		}
		disk, err := p.Disk(ctx, path)
		if err != nil {
			return nil, DiskOutput{}, err
		}
		warning := disk.UsedRatio >= threshold
		signal := 0.0
		if warning {
			signal = 1
		}
		components := rca.ConfidenceComponents{SignalMatch: signal}
		result := rca.Result{DiagnosisLevel: rca.D3, Confidence: components.Score(), ConfidenceComponents: components, EvidenceRefs: []string{"statfs:" + path}, MissingEvidence: []string{"大型文件列表", "文件写入进程", "日志增长模式"}, Remediation: []string{"在 SafeOps Lab allowlist 内继续定位大型文件和写入进程"}}
		if warning {
			result.DiagnosisLevel = rca.D2
			result.CandidateCauses = []rca.CandidateCause{{Cause: "文件系统容量压力已确认，但异常增长来源尚未确认", Score: result.Confidence, EvidenceRefs: result.EvidenceRefs}}
		}
		return &mcp.CallToolResult{}, DiskOutput{Disk: disk, Warning: warning, RCA: result}, nil
	})
	mcp.AddTool(s, readTool("diagnostic.build_snapshot", "构建系统、可选服务/端口的只读结构化快照"), func(ctx context.Context, _ *mcp.CallToolRequest, in SnapshotInput) (*mcp.CallToolResult, SnapshotOutput, error) {
		kernel, err := p.Kernel(ctx)
		if err != nil {
			return nil, SnapshotOutput{}, err
		}
		load, err := p.Load(ctx)
		if err != nil {
			return nil, SnapshotOutput{}, err
		}
		memory, err := p.Memory(ctx)
		if err != nil {
			return nil, SnapshotOutput{}, err
		}
		path := in.Path
		if path == "" {
			path = "/"
		}
		disk, err := p.Disk(ctx, path)
		if err != nil {
			return nil, SnapshotOutput{}, err
		}
		out := SnapshotOutput{Kernel: kernel, Load: load, Memory: memory, Disk: disk}
		if in.Unit != "" {
			service, err := p.Service(ctx, in.Unit)
			if err != nil {
				return nil, SnapshotOutput{}, err
			}
			out.Service = &service
		}
		if in.Port != 0 {
			if in.Port < 1 || in.Port > 65535 {
				return nil, SnapshotOutput{}, errors.New("port must be between 1 and 65535")
			}
			all, err := p.Sockets(ctx, false, 5000)
			if err != nil {
				return nil, SnapshotOutput{}, err
			}
			for _, socket := range all {
				if socket.LocalPort == in.Port {
					out.PortSockets = append(out.PortSockets, socket)
				}
			}
			out.PortProcesses, err = p.ProcessesByPort(ctx, in.Port)
			if err != nil {
				return nil, SnapshotOutput{}, err
			}
		}
		return &mcp.CallToolResult{}, out, nil
	})
	return s
}
func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "diagnostic", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.timeout_seconds": 10}}
}
