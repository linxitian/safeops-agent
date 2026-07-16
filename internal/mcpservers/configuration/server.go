package configuration

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/safefs"
)

const Version = "0.1.0"

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type EmptyInput struct{}
type PathInput struct {
	Path string `json:"path" jsonschema:"absolute path under a managed configuration root"`
}
type SnapshotInput struct {
	Path          string `json:"path" jsonschema:"absolute directory under a managed configuration root"`
	Limit         int    `json:"limit,omitempty" jsonschema:"maximum files from 1 to 500"`
	MaxFileBytes  int64  `json:"max_file_bytes,omitempty" jsonschema:"per-file hash bound up to 16777216"`
	MaxTotalBytes int64  `json:"max_total_bytes,omitempty" jsonschema:"total hash bound up to 33554432"`
}
type DiffInput struct {
	Path          string                 `json:"path" jsonschema:"absolute directory under a managed configuration root"`
	Baseline      []safefs.SnapshotEntry `json:"baseline" jsonschema:"previous provenance-bearing configuration snapshot"`
	Limit         int                    `json:"limit,omitempty"`
	MaxFileBytes  int64                  `json:"max_file_bytes,omitempty"`
	MaxTotalBytes int64                  `json:"max_total_bytes,omitempty"`
}
type RootsOutput struct {
	Roots []string `json:"roots"`
}
type MetadataOutput struct {
	Metadata safefs.Metadata `json:"metadata"`
}
type SnapshotOutput struct {
	Snapshot safefs.Snapshot `json:"snapshot"`
}
type Change struct {
	RelativePath string `json:"relative_path"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterSHA256  string `json:"after_sha256,omitempty"`
}
type DiffOutput struct {
	Snapshot safefs.Snapshot `json:"snapshot"`
	Added    []Change        `json:"added"`
	Removed  []Change        `json:"removed"`
	Modified []Change        `json:"modified"`
}

func New(reader *safefs.Reader) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "safeops-mcp-config", Version: Version}, nil)
	mcp.AddTool(s, readTool("config.list_roots", "列出受管理配置的严格 allowlist 根目录"), func(context.Context, *mcp.CallToolRequest, EmptyInput) (*mcp.CallToolResult, RootsOutput, error) {
		return &mcp.CallToolResult{}, RootsOutput{Roots: reader.Roots()}, nil
	})
	mcp.AddTool(s, readTool("config.get_metadata", "读取 allowlist 内配置文件元数据，不返回可能含密钥的正文"), func(ctx context.Context, _ *mcp.CallToolRequest, in PathInput) (*mcp.CallToolResult, MetadataOutput, error) {
		value, err := reader.Metadata(ctx, in.Path)
		return &mcp.CallToolResult{}, MetadataOutput{Metadata: value}, err
	})
	mcp.AddTool(s, readTool("config.snapshot", "对受管理配置生成有界 SHA-256 快照与跳过原因"), func(ctx context.Context, _ *mcp.CallToolRequest, in SnapshotInput) (*mcp.CallToolResult, SnapshotOutput, error) {
		value, err := reader.Snapshot(ctx, in.Path, in.Limit, in.MaxFileBytes, in.MaxTotalBytes)
		return &mcp.CallToolResult{}, SnapshotOutput{Snapshot: value}, err
	})
	mcp.AddTool(s, readTool("config.diff_snapshot", "将当前受管理配置哈希与显式基线比较，不读取配置正文"), func(ctx context.Context, _ *mcp.CallToolRequest, in DiffInput) (*mcp.CallToolResult, DiffOutput, error) {
		if err := validateBaseline(in.Baseline); err != nil {
			return nil, DiffOutput{}, err
		}
		current, err := reader.Snapshot(ctx, in.Path, in.Limit, in.MaxFileBytes, in.MaxTotalBytes)
		if err != nil {
			return nil, DiffOutput{}, err
		}
		out := diff(in.Baseline, current.Entries)
		out.Snapshot = current
		return &mcp.CallToolResult{}, out, nil
	})
	return s
}

func validateBaseline(entries []safefs.SnapshotEntry) error {
	if len(entries) > 500 {
		return errors.New("baseline must not exceed 500 entries")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.RelativePath == "" || seen[entry.RelativePath] {
			return errors.New("baseline paths must be non-empty and unique")
		}
		if !sha256Pattern.MatchString(entry.SHA256) {
			return fmt.Errorf("invalid SHA-256 for %s", entry.RelativePath)
		}
		seen[entry.RelativePath] = true
	}
	return nil
}

func diff(before, after []safefs.SnapshotEntry) DiffOutput {
	old := map[string]string{}
	current := map[string]string{}
	for _, entry := range before {
		old[entry.RelativePath] = entry.SHA256
	}
	for _, entry := range after {
		current[entry.RelativePath] = entry.SHA256
	}
	var out DiffOutput
	for path, hash := range current {
		previous, exists := old[path]
		switch {
		case !exists:
			out.Added = append(out.Added, Change{RelativePath: path, AfterSHA256: hash})
		case previous != hash:
			out.Modified = append(out.Modified, Change{RelativePath: path, BeforeSHA256: previous, AfterSHA256: hash})
		}
	}
	for path, hash := range old {
		if _, exists := current[path]; !exists {
			out.Removed = append(out.Removed, Change{RelativePath: path, BeforeSHA256: hash})
		}
	}
	sortChanges := func(values []Change) {
		sort.Slice(values, func(i, j int) bool { return values[i].RelativePath < values[j].RelativePath })
	}
	sortChanges(out.Added)
	sortChanges(out.Removed)
	sortChanges(out.Modified)
	return out
}

func readTool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Meta: mcp.Meta{"safeops.domain": "config", "safeops.mode": "read", "safeops.base_risk": "L0", "safeops.scope": "allowlisted_roots", "safeops.timeout_seconds": 10}}
}
