package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/llm"
)

type Registry struct {
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	manifests   map[string]ServerManifest
	states      map[string]ServerState
	clients     map[string]*mcp.Client
	sessions    map[string]*mcp.ClientSession
}

func New(cfg Config) *Registry {
	r := &Registry{manifests: map[string]ServerManifest{}, states: map[string]ServerState{}, clients: map[string]*mcp.Client{}, sessions: map[string]*mcp.ClientSession{}}
	for _, m := range cfg.Servers {
		r.manifests[m.ID] = m
		state := ServerState{Manifest: m, Status: StatusDisabled}
		r.states[m.ID] = state
	}
	return r
}

func (r *Registry) Start(ctx context.Context) error {
	r.mu.RLock()
	ids := make([]string, 0, len(r.manifests))
	for id, m := range r.manifests {
		if m.Enabled {
			ids = append(ids, id)
		}
	}
	r.mu.RUnlock()
	sort.Strings(ids)
	var errs []error
	for _, id := range ids {
		if err := r.Discover(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) Discover(ctx context.Context, id string) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	m, ok := r.manifests[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("unknown MCP server %q", id)
	}
	if !m.Enabled {
		r.mu.Unlock()
		return fmt.Errorf("MCP server %s is disabled", id)
	}
	old := r.sessions[id]
	state := r.states[id]
	state.Status = StatusStarting
	state.Error = ""
	r.states[id] = state
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	r.mu.Lock()
	delete(r.clients, id)
	delete(r.sessions, id)
	r.mu.Unlock()
	// The child process lifetime must not be bound to the short initialize
	// context. CommandTransport owns termination when the session is closed.
	cmd := exec.Command(m.Command, m.Arguments...)
	cmd.Env = os.Environ()
	for key, value := range m.Environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "safeops-registry-" + id, Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd, TerminateDuration: 2 * time.Second}, nil)
	if err != nil {
		r.recordFailure(id, err)
		return fmt.Errorf("connect MCP server %s: %w", id, err)
	}
	tools, err := listTools(ctx, session, id, m.Version)
	if err != nil {
		_ = session.Close()
		r.recordFailure(id, err)
		return fmt.Errorf("discover MCP server %s: %w", id, err)
	}
	r.mu.Lock()
	r.clients[id] = client
	r.sessions[id] = session
	state = r.states[id]
	newHash, err := toolSetHash(tools)
	if err != nil {
		r.mu.Unlock()
		_ = session.Close()
		r.recordFailure(id, err)
		return fmt.Errorf("fingerprint MCP server %s tool set: %w", id, err)
	}
	state.PreviousToolSetHash = state.ToolSetHash
	state.ToolsChanged = state.ToolSetHash != "" && state.ToolSetHash != newHash
	state.ToolSetHash = newHash
	state.Status = StatusHealthy
	state.Tools = tools
	state.LastChecked = time.Now().UTC()
	r.states[id] = state
	r.mu.Unlock()
	return nil
}

func toolSetHash(records []ToolRecord) (string, error) {
	stable := make([]struct {
		Name          string `json:"name"`
		SchemaHash    string `json:"schema_hash"`
		ServerVersion string `json:"server_version"`
	}, 0, len(records))
	for _, record := range records {
		stable = append(stable, struct {
			Name          string `json:"name"`
			SchemaHash    string `json:"schema_hash"`
			ServerVersion string `json:"server_version"`
		}{record.Name, record.SchemaHash, record.ServerVersion})
	}
	sort.Slice(stable, func(i, j int) bool { return stable[i].Name < stable[j].Name })
	b, err := json.Marshal(stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Registry) SetEnabled(ctx context.Context, id string, enabled bool) error {
	r.mu.Lock()
	manifest, ok := r.manifests[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("unknown MCP server %q", id)
	}
	manifest.Enabled = enabled
	r.manifests[id] = manifest
	state := r.states[id]
	state.Manifest = manifest
	r.states[id] = state
	r.mu.Unlock()
	if enabled {
		return r.Discover(ctx, id)
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	session := r.sessions[id]
	delete(r.sessions, id)
	delete(r.clients, id)
	state = r.states[id]
	state.Status = StatusDisabled
	state.Error = ""
	state.Tools = nil
	state.PreviousToolSetHash = state.ToolSetHash
	state.ToolsChanged = false
	state.LastChecked = time.Now().UTC()
	r.states[id] = state
	r.mu.Unlock()
	if session != nil {
		return session.Close()
	}
	return nil
}

func listTools(ctx context.Context, session *mcp.ClientSession, serverID, version string) ([]ToolRecord, error) {
	var records []ToolRecord
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}
		fingerprint := struct {
			Name         string               `json:"name"`
			Title        string               `json:"title"`
			Description  string               `json:"description"`
			InputSchema  any                  `json:"input_schema"`
			OutputSchema any                  `json:"output_schema"`
			Annotations  *mcp.ToolAnnotations `json:"annotations"`
		}{tool.Name, tool.Title, tool.Description, tool.InputSchema, tool.OutputSchema, tool.Annotations}
		schema, err := json.Marshal(fingerprint)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(schema)
		inputSchema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return nil, err
		}
		records = append(records, ToolRecord{ServerID: serverID, Name: tool.Name, Description: tool.Description, InputSchema: inputSchema, SchemaHash: hex.EncodeToString(sum[:]), DiscoveredAt: time.Now().UTC(), ServerVersion: version})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	return records, nil
}

func (r *Registry) AvailableTools() []llm.ToolCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []llm.ToolCapability
	for _, state := range r.states {
		if state.Status != StatusHealthy || !state.Manifest.Enabled {
			continue
		}
		for _, tool := range state.Tools {
			out = append(out, llm.ToolCapability{ServerID: state.Manifest.ID, Name: tool.Name, Description: tool.Description, InputSchema: append(json.RawMessage(nil), tool.InputSchema...)})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServerID == out[j].ServerID {
			return out[i].Name < out[j].Name
		}
		return out[i].ServerID < out[j].ServerID
	})
	return out
}

func (r *Registry) recordFailure(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[id]
	state.Status = StatusUnhealthy
	state.Error = err.Error()
	state.LastChecked = time.Now().UTC()
	r.states[id] = state
}

func (r *Registry) CallTool(ctx context.Context, serverID, name string, arguments any) (*mcp.CallToolResult, error) {
	r.mu.RLock()
	session := r.sessions[serverID]
	state := r.states[serverID]
	r.mu.RUnlock()
	if session == nil || state.Status != StatusHealthy {
		return nil, fmt.Errorf("MCP server %s is not healthy", serverID)
	}
	known := false
	for _, tool := range state.Tools {
		if tool.Name == name {
			known = true
			break
		}
	}
	if !known {
		return nil, fmt.Errorf("tool %q is not registered for server %s", name, serverID)
	}
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
}

func (r *Registry) Health(ctx context.Context, id string) error {
	r.mu.RLock()
	session := r.sessions[id]
	r.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("MCP server %s is not connected", id)
	}
	if err := session.Ping(ctx, nil); err != nil {
		r.recordFailure(id, err)
		return err
	}
	r.mu.Lock()
	state := r.states[id]
	state.Status = StatusHealthy
	state.Error = ""
	state.LastChecked = time.Now().UTC()
	r.states[id] = state
	r.mu.Unlock()
	return nil
}

func (r *Registry) States() []ServerState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServerState, 0, len(r.states))
	for _, state := range r.states {
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.ID < out[j].Manifest.ID })
	return out
}

func (r *Registry) Close() error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	var errs []error
	for id, session := range r.sessions {
		if err := session.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}
	r.sessions = map[string]*mcp.ClientSession{}
	r.clients = map[string]*mcp.Client{}
	return errors.Join(errs...)
}
