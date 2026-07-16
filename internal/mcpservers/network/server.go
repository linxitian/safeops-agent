package network

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/platform"
)

const Version = "0.1.0"

type Platform interface {
	Sockets(context.Context, bool, int) ([]platform.SocketInfo, error)
	Interfaces(context.Context) ([]platform.InterfaceInfo, error)
	ProcessesByPort(context.Context, int) ([]platform.ProcessInfo, error)
}
type LimitInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum rows, from 1 to 1000"`
}
type EmptyInput struct{}
type PortInput struct {
	Port     int    `json:"port" jsonschema:"port from 1 to 65535"`
	Protocol string `json:"protocol,omitempty" jsonschema:"optional tcp or udp family filter"`
}
type SocketsOutput struct {
	Sockets   []platform.SocketInfo `json:"sockets"`
	Count     int                   `json:"count"`
	Truncated bool                  `json:"truncated"`
}
type InterfacesOutput struct {
	Interfaces []platform.InterfaceInfo `json:"interfaces"`
	Count      int                      `json:"count"`
}
type InterfaceStat struct {
	Name    string `json:"name"`
	Index   int    `json:"index"`
	RXBytes uint64 `json:"rx_bytes"`
	TXBytes uint64 `json:"tx_bytes"`
}
type InterfaceStatsOutput struct {
	Interfaces []InterfaceStat `json:"interfaces"`
}
type PortOutput struct {
	Port         int                    `json:"port"`
	Occupied     bool                   `json:"occupied"`
	Sockets      []platform.SocketInfo  `json:"sockets"`
	Processes    []platform.ProcessInfo `json:"processes"`
	OwnerPartial bool                   `json:"owner_partial"`
}

func New(p Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-network", Version: Version}, nil)
	mcp.AddTool(s, readTool("network.list_listeners", "从 /proc/net 读取监听 socket"), func(ctx context.Context, _ *mcp.CallToolRequest, in LimitInput) (*mcp.CallToolResult, SocketsOutput, error) {
		limit, err := limit(in.Limit)
		if err != nil {
			return nil, SocketsOutput{}, err
		}
		values, err := p.Sockets(ctx, true, limit)
		return &mcp.CallToolResult{}, SocketsOutput{Sockets: values, Count: len(values), Truncated: len(values) == limit}, err
	})
	mcp.AddTool(s, readTool("network.list_connections", "从 /proc/net 读取连接与监听 socket"), func(ctx context.Context, _ *mcp.CallToolRequest, in LimitInput) (*mcp.CallToolResult, SocketsOutput, error) {
		limit, err := limit(in.Limit)
		if err != nil {
			return nil, SocketsOutput{}, err
		}
		values, err := p.Sockets(ctx, false, limit)
		return &mcp.CallToolResult{}, SocketsOutput{Sockets: values, Count: len(values), Truncated: len(values) == limit}, err
	})
	mcp.AddTool(s, readTool("network.check_port", "检查端口 socket 并通过 inode 关联可见进程"), func(ctx context.Context, _ *mcp.CallToolRequest, in PortInput) (*mcp.CallToolResult, PortOutput, error) {
		if in.Port < 1 || in.Port > 65535 {
			return nil, PortOutput{}, errors.New("port must be between 1 and 65535")
		}
		protocol := strings.ToLower(in.Protocol)
		if protocol != "" && protocol != "tcp" && protocol != "udp" {
			return nil, PortOutput{}, errors.New("protocol must be tcp or udp")
		}
		all, err := p.Sockets(ctx, false, 5000)
		if err != nil {
			return nil, PortOutput{}, err
		}
		var matches []platform.SocketInfo
		for _, socket := range all {
			if socket.LocalPort == in.Port && (protocol == "" || strings.HasPrefix(socket.Protocol, protocol)) {
				matches = append(matches, socket)
			}
		}
		owners, err := p.ProcessesByPort(ctx, in.Port)
		if err != nil {
			return nil, PortOutput{}, err
		}
		return &mcp.CallToolResult{}, PortOutput{Port: in.Port, Occupied: len(matches) > 0, Sockets: matches, Processes: owners, OwnerPartial: true}, nil
	})
	mcp.AddTool(s, readTool("network.get_interfaces", "读取接口身份、地址、MTU、flags 和流量累计值"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, InterfacesOutput, error) {
		values, err := p.Interfaces(ctx)
		return &mcp.CallToolResult{}, InterfacesOutput{Interfaces: values, Count: len(values)}, err
	})
	mcp.AddTool(s, readTool("network.get_interface_stats", "读取接口 RX/TX bytes 累计值"), func(ctx context.Context, _ *mcp.CallToolRequest, _ EmptyInput) (*mcp.CallToolResult, InterfaceStatsOutput, error) {
		values, err := p.Interfaces(ctx)
		stats := make([]InterfaceStat, 0, len(values))
		for _, value := range values {
			stats = append(stats, InterfaceStat{Name: value.Name, Index: value.Index, RXBytes: value.RXBytes, TXBytes: value.TXBytes})
		}
		return &mcp.CallToolResult{}, InterfaceStatsOutput{Interfaces: stats}, err
	})
	return s
}
func limit(value int) (int, error) {
	if value == 0 {
		return 200, nil
	}
	if value < 1 || value > 1000 {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return value, nil
}
func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "network", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.timeout_seconds": 10}}
}
