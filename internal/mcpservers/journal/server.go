package journal

import (
	"context"
	"errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"safeops-agent/internal/platform"
	"strings"
)

const Version = "0.1.0"

type Platform interface {
	Journal(context.Context, platform.JournalQuery) ([]platform.JournalEvent, error)
}
type RecentInput struct {
	Lines    int  `json:"lines,omitempty" jsonschema:"maximum 500 events"`
	Priority *int `json:"priority,omitempty" jsonschema:"maximum syslog priority 0 through 7"`
}
type UnitInput struct {
	Unit     string `json:"unit" jsonschema:"systemd unit name"`
	Lines    int    `json:"lines,omitempty" jsonschema:"maximum 500 events"`
	Priority *int   `json:"priority,omitempty" jsonschema:"maximum syslog priority 0 through 7"`
}
type SearchInput struct {
	Query string `json:"query,omitempty" jsonschema:"optional case-insensitive literal text; defaults to common error signals"`
	Unit  string `json:"unit,omitempty" jsonschema:"optional systemd unit"`
	Lines int    `json:"lines,omitempty" jsonschema:"source window, maximum 500 events"`
}
type EventsOutput struct {
	Events  []platform.JournalEvent `json:"events"`
	Count   int                     `json:"count"`
	Partial bool                    `json:"partial"`
	Note    string                  `json:"note,omitempty"`
}

func New(p Platform) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-journal", Version: Version}, nil)
	mcp.AddTool(s, readTool("journal.get_recent", "读取有界的 journalctl JSON 事件"), func(ctx context.Context, _ *mcp.CallToolRequest, in RecentInput) (*mcp.CallToolResult, EventsOutput, error) {
		events, err := p.Journal(ctx, platform.JournalQuery{Lines: in.Lines, Priority: priority(in.Priority)})
		return result(events, err)
	})
	mcp.AddTool(s, readTool("journal.query_unit", "读取指定 systemd unit 的有界 JSON 日志"), func(ctx context.Context, _ *mcp.CallToolRequest, in UnitInput) (*mcp.CallToolResult, EventsOutput, error) {
		if in.Unit == "" {
			return nil, EventsOutput{}, errors.New("unit is required")
		}
		events, err := p.Journal(ctx, platform.JournalQuery{Unit: in.Unit, Lines: in.Lines, Priority: priority(in.Priority)})
		return result(events, err)
	})
	mcp.AddTool(s, readTool("journal.search_errors", "在有界日志窗口中过滤 error、fail、panic 等错误信号"), func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, EventsOutput, error) {
		events, err := p.Journal(ctx, platform.JournalQuery{Unit: in.Unit, Lines: in.Lines, Priority: -1})
		if err != nil {
			return nil, EventsOutput{}, err
		}
		needle := strings.ToLower(strings.TrimSpace(in.Query))
		filtered := events[:0]
		for _, event := range events {
			message := strings.ToLower(event.Message)
			if (needle != "" && strings.Contains(message, needle)) || (needle == "" && (event.Priority <= 3 || strings.Contains(message, "error") || strings.Contains(message, "fail") || strings.Contains(message, "panic"))) {
				filtered = append(filtered, event)
			}
		}
		return result(filtered, nil)
	})
	mcp.AddTool(s, readTool("journal.get_priority_events", "读取指定最高 syslog priority 的有界事件"), func(ctx context.Context, _ *mcp.CallToolRequest, in RecentInput) (*mcp.CallToolResult, EventsOutput, error) {
		maximum := 3
		if in.Priority != nil {
			maximum = *in.Priority
		}
		events, err := p.Journal(ctx, platform.JournalQuery{Lines: in.Lines, Priority: maximum})
		return result(events, err)
	})
	return s
}
func priority(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}
func result(events []platform.JournalEvent, err error) (*mcp.CallToolResult, EventsOutput, error) {
	return &mcp.CallToolResult{}, EventsOutput{Events: events, Count: len(events), Partial: true, Note: "日志受用户权限、500 条上限、截断和脱敏约束"}, err
}
func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "journal", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.timeout_seconds": 10}}
}
