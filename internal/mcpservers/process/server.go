package process

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
)

const Version = "0.1.0"

type Platform interface {
	Processes(context.Context, platform.ProcessQuery) ([]platform.ProcessInfo, error)
	Process(context.Context, int) (platform.ProcessInfo, error)
	ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error)
}
type ListInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of processes, from 1 to 100"`
	SortBy string `json:"sort_by,omitempty" jsonschema:"cpu, memory, or pid"`
}
type SearchInput struct {
	Query string `json:"query" jsonschema:"case-insensitive process name or command search"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum results, from 1 to 100"`
}
type PIDInput struct {
	PID int `json:"pid" jsonschema:"positive Linux process ID"`
}
type PortInput struct {
	Port int `json:"port" jsonschema:"TCP or UDP port, from 1 to 65535"`
}
type ProcessesOutput struct {
	Processes []platform.ProcessInfo `json:"processes"`
	Count     int                    `json:"count"`
	Partial   bool                   `json:"partial"`
	Note      string                 `json:"note,omitempty"`
}
type ProcessOutput struct {
	Process platform.ProcessInfo `json:"process"`
}
type ResourceOutput struct {
	PID        int    `json:"pid"`
	StartTicks uint64 `json:"start_ticks"`
	CPUTicks   uint64 `json:"cpu_ticks"`
	RSSBytes   uint64 `json:"rss_bytes"`
	State      string `json:"state"`
}

func New(p Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-process", Version: Version}, nil)
	mcp.AddTool(s, readTool("process.list_top", "按 CPU 累计 ticks、内存或 PID 返回真实进程快照"), func(ctx context.Context, _ *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ProcessesOutput, error) {
		limit, err := validLimit(in.Limit)
		if err != nil {
			return nil, ProcessesOutput{}, err
		}
		sortBy := in.SortBy
		if sortBy == "" {
			sortBy = "cpu"
		}
		if sortBy != "cpu" && sortBy != "memory" && sortBy != "pid" {
			return nil, ProcessesOutput{}, errors.New("sort_by must be cpu, memory, or pid")
		}
		values, err := p.Processes(ctx, platform.ProcessQuery{Limit: limit, SortBy: sortBy})
		return &mcp.CallToolResult{}, ProcessesOutput{Processes: values, Count: len(values), Partial: true, Note: "无权限或采集期间退出的进程可能不在结果中"}, err
	})
	mcp.AddTool(s, readTool("process.search", "按名称、命令或可执行路径搜索真实 /proc 进程"), func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, ProcessesOutput, error) {
		if strings.TrimSpace(in.Query) == "" {
			return nil, ProcessesOutput{}, errors.New("query is required")
		}
		limit, err := validLimit(in.Limit)
		if err != nil {
			return nil, ProcessesOutput{}, err
		}
		values, err := p.Processes(ctx, platform.ProcessQuery{Search: in.Query, Limit: limit, SortBy: "pid"})
		return &mcp.CallToolResult{}, ProcessesOutput{Processes: values, Count: len(values), Partial: true, Note: "命令参数已截断并对常见密钥字段脱敏"}, err
	})
	mcp.AddTool(s, readTool("process.get_details", "读取 PID、start ticks、UID、命令、状态和资源累计值"), func(ctx context.Context, _ *mcp.CallToolRequest, in PIDInput) (*mcp.CallToolResult, ProcessOutput, error) {
		if in.PID <= 0 {
			return nil, ProcessOutput{}, errors.New("pid must be positive")
		}
		value, err := p.Process(ctx, in.PID)
		return &mcp.CallToolResult{}, ProcessOutput{Process: value}, err
	})
	mcp.AddTool(s, readTool("process.get_resource_usage", "读取带 start ticks 身份的进程 CPU 累计 ticks 与 RSS"), func(ctx context.Context, _ *mcp.CallToolRequest, in PIDInput) (*mcp.CallToolResult, ResourceOutput, error) {
		if in.PID <= 0 {
			return nil, ResourceOutput{}, errors.New("pid must be positive")
		}
		value, err := p.Process(ctx, in.PID)
		return &mcp.CallToolResult{}, ResourceOutput{PID: value.PID, StartTicks: value.StartTicks, CPUTicks: value.CPUTicks, RSSBytes: value.RSSBytes, State: value.State}, err
	})
	mcp.AddTool(s, readTool("process.find_by_port", "通过 /proc socket inode 与进程 FD 关联端口占用者"), func(ctx context.Context, _ *mcp.CallToolRequest, in PortInput) (*mcp.CallToolResult, ProcessesOutput, error) {
		if in.Port < 1 || in.Port > 65535 {
			return nil, ProcessesOutput{}, errors.New("port must be between 1 and 65535")
		}
		values, err := p.ProcessesByPort(ctx, in.Port)
		return &mcp.CallToolResult{}, ProcessesOutput{Processes: values, Count: len(values), Partial: true, Note: "非 root 用户可能无法读取其他用户的 FD，owner 结果可能不完整"}, err
	})
	return s
}
func validLimit(value int) (int, error) {
	if value == 0 {
		return 20, nil
	}
	if value < 1 || value > 100 {
		return 0, errors.New("limit must be between 1 and 100")
	}
	return value, nil
}
func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "process", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.timeout_seconds": 10}}
}
