package service

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
)

const Version = "0.1.0"

type Platform interface {
	Service(context.Context, string) (platform.ServiceStatus, error)
	FailedServices(context.Context) ([]platform.ServiceStatus, error)
	ServiceDependencies(context.Context, string) (platform.ServiceDependencies, error)
}
type UnitInput struct {
	Unit string `json:"unit" jsonschema:"systemd unit name; .service is added when omitted"`
}
type EmptyInput struct{}
type StatusOutput struct {
	Service platform.ServiceStatus `json:"service"`
}
type ServicesOutput struct {
	Services []platform.ServiceStatus `json:"services"`
	Count    int                      `json:"count"`
}
type DependenciesOutput struct {
	Dependencies platform.ServiceDependencies `json:"dependencies"`
}
type RestartOutput struct {
	Unit         string `json:"unit"`
	RestartCount uint64 `json:"restart_count"`
	MainPID      int    `json:"main_pid"`
	ActiveState  string `json:"active_state"`
}

func New(p Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-service", Version: Version}, nil)
	mcp.AddTool(s, readTool("service.get_status", "通过固定 systemctl show 属性读取服务状态"), func(ctx context.Context, _ *mcp.CallToolRequest, in UnitInput) (*mcp.CallToolResult, StatusOutput, error) {
		if in.Unit == "" {
			return nil, StatusOutput{}, errors.New("unit is required")
		}
		value, err := p.Service(ctx, in.Unit)
		return &mcp.CallToolResult{}, StatusOutput{Service: value}, err
	})
	mcp.AddTool(s, readTool("service.list_failed", "通过固定 systemctl list-units 查询失败服务"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, ServicesOutput, error) {
		values, err := p.FailedServices(ctx)
		return &mcp.CallToolResult{}, ServicesOutput{Services: values, Count: len(values)}, err
	})
	mcp.AddTool(s, readTool("service.get_dependencies", "读取 Requires、Wants、After 和 Before 属性"), func(ctx context.Context, _ *mcp.CallToolRequest, in UnitInput) (*mcp.CallToolResult, DependenciesOutput, error) {
		if in.Unit == "" {
			return nil, DependenciesOutput{}, errors.New("unit is required")
		}
		value, err := p.ServiceDependencies(ctx, in.Unit)
		return &mcp.CallToolResult{}, DependenciesOutput{Dependencies: value}, err
	})
	mcp.AddTool(s, readTool("service.get_restart_count", "读取 NRestarts、MainPID 和 ActiveState"), func(ctx context.Context, _ *mcp.CallToolRequest, in UnitInput) (*mcp.CallToolResult, RestartOutput, error) {
		if in.Unit == "" {
			return nil, RestartOutput{}, errors.New("unit is required")
		}
		value, err := p.Service(ctx, in.Unit)
		return &mcp.CallToolResult{}, RestartOutput{Unit: value.Name, RestartCount: value.RestartCount, MainPID: value.MainPID, ActiveState: value.ActiveState}, err
	})
	return s
}
func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "service", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.timeout_seconds": 10}}
}
