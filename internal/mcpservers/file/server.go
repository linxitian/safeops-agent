package file

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/safefs"
)

const Version = "0.1.0"

type EmptyInput struct{}
type PathInput struct {
	Path string `json:"path" jsonschema:"absolute path under a configured read-only file root"`
}
type ListInput struct {
	Path  string `json:"path" jsonschema:"absolute directory under a configured read-only file root"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum entries from 1 to 500"`
}
type HashInput struct {
	Path     string `json:"path" jsonschema:"absolute regular file under a configured read-only file root"`
	MaxBytes int64  `json:"max_bytes,omitempty" jsonschema:"read bound up to 16777216 bytes"`
}
type LargeInput struct {
	Path         string `json:"path" jsonschema:"absolute directory under a configured read-only file root"`
	MinimumBytes int64  `json:"minimum_bytes,omitempty"`
	MaxDepth     int    `json:"max_depth,omitempty" jsonschema:"directory depth from 1 to 16"`
	Limit        int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 200"`
}

type RootsOutput struct {
	Roots []string `json:"roots"`
}
type StatOutput struct {
	Metadata safefs.Metadata `json:"metadata"`
}
type ListOutput struct {
	Entries   []safefs.Metadata `json:"entries"`
	Truncated bool              `json:"truncated"`
}
type HashOutput struct {
	Hash safefs.FileHash `json:"hash"`
}
type LargeOutput struct {
	Files     []safefs.Metadata `json:"files"`
	Truncated bool              `json:"truncated"`
}

func New(reader *safefs.Reader) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-file", Version: Version}, nil)
	mcp.AddTool(s, readTool("file.list_roots", "列出文件感知严格允许的只读根目录"), func(context.Context, *mcp.CallToolRequest, EmptyInput) (*mcp.CallToolResult, RootsOutput, error) {
		return &mcp.CallToolResult{}, RootsOutput{Roots: reader.Roots()}, nil
	})
	mcp.AddTool(s, readTool("file.stat", "读取 allowlist 内文件或目录元数据，不返回正文"), func(ctx context.Context, _ *mcp.CallToolRequest, in PathInput) (*mcp.CallToolResult, StatOutput, error) {
		value, err := reader.Metadata(ctx, in.Path)
		return &mcp.CallToolResult{}, StatOutput{Metadata: value}, err
	})
	mcp.AddTool(s, readTool("file.list_directory", "列出 allowlist 内目录的有界元数据，不跟随目录项 symlink"), func(ctx context.Context, _ *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListOutput, error) {
		entries, truncated, err := reader.List(ctx, in.Path, in.Limit)
		return &mcp.CallToolResult{}, ListOutput{Entries: entries, Truncated: truncated}, err
	})
	mcp.AddTool(s, readTool("file.sha256", "在 16 MiB 硬上限内计算 allowlist 文件 SHA-256"), func(ctx context.Context, _ *mcp.CallToolRequest, in HashInput) (*mcp.CallToolResult, HashOutput, error) {
		value, err := reader.Hash(ctx, in.Path, in.MaxBytes)
		return &mcp.CallToolResult{}, HashOutput{Hash: value}, err
	})
	mcp.AddTool(s, readTool("file.find_large", "在限定深度和结果数内定位配置只读根内的大文件"), func(ctx context.Context, _ *mcp.CallToolRequest, in LargeInput) (*mcp.CallToolResult, LargeOutput, error) {
		files, truncated, err := reader.FindLarge(ctx, in.Path, in.MinimumBytes, in.MaxDepth, in.Limit)
		return &mcp.CallToolResult{}, LargeOutput{Files: files, Truncated: truncated}, err
	})
	return s
}

func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "file", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.scope": "allowlisted_roots", "safeops.timeout_seconds": 10}}
}
