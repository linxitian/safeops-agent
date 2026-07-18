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
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"safeops-agent/internal/llm"
)

type Registry struct {
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	cycleMu     sync.Mutex
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
	started := time.Now()
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
	now := time.Now().UTC()
	dependencyChecks, dependenciesHealthy := inspectDependencies(m.Dependencies, now)
	if dependencyErr := dependencyFailure(dependencyChecks); dependencyErr != nil {
		r.recordDiscoveryFailure(id, started, dependencyChecks, "", "", "", dependencyErr)
		return dependencyErr
	}
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
		r.recordDiscoveryFailure(id, started, dependencyChecks, "", "", "", err)
		return fmt.Errorf("connect MCP server %s: %w", id, err)
	}
	initialized := session.InitializeResult()
	if initialized == nil || initialized.ServerInfo == nil || strings.TrimSpace(initialized.ServerInfo.Name) == "" || strings.TrimSpace(initialized.ServerInfo.Version) == "" || strings.TrimSpace(initialized.ProtocolVersion) == "" {
		_ = session.Close()
		err := errors.New("initialize result omitted actual server name, version, or protocol version")
		r.recordDiscoveryFailure(id, started, dependencyChecks, "", "", "", err)
		return fmt.Errorf("connect MCP server %s: %w", id, err)
	}
	serverName := initialized.ServerInfo.Name
	serverVersion := initialized.ServerInfo.Version
	protocolVersion := initialized.ProtocolVersion
	tools, err := listTools(ctx, session, id, serverVersion)
	if err != nil {
		_ = session.Close()
		r.recordDiscoveryFailure(id, started, dependencyChecks, serverName, serverVersion, protocolVersion, err)
		return fmt.Errorf("discover MCP server %s: %w", id, err)
	}
	newHash, err := toolSetHash(tools)
	if err != nil {
		_ = session.Close()
		r.recordDiscoveryFailure(id, started, dependencyChecks, serverName, serverVersion, protocolVersion, err)
		return fmt.Errorf("fingerprint MCP server %s tool set: %w", id, err)
	}
	now = time.Now().UTC()
	r.mu.Lock()
	r.clients[id] = client
	r.sessions[id] = session
	state = r.states[id]
	state.PreviousToolSetHash = state.ToolSetHash
	state.ToolsChanged = state.ToolSetHash != "" && state.ToolSetHash != newHash
	state.ToolSetHash = newHash
	state.ActualServerName = serverName
	state.ActualServerVersion = serverVersion
	state.ProtocolVersion = protocolVersion
	state.DependenciesChecked = true
	state.DependenciesHealthy = dependenciesHealthy
	state.DependencyChecks = dependencyChecks
	state.Status = StatusHealthy
	state.Error = ""
	state.Tools = tools
	state.LastChecked = now
	state.DiscoveryHistory = appendBounded(state.DiscoveryHistory, DiscoveryRecord{
		DiscoveredAt:        now,
		Status:              StatusHealthy,
		ServerName:          serverName,
		ServerVersion:       serverVersion,
		ProtocolVersion:     protocolVersion,
		ToolSetHash:         newHash,
		ToolCount:           len(tools),
		ToolsChanged:        state.ToolsChanged,
		DependenciesHealthy: dependenciesHealthy,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
	state.HealthHistory = appendBounded(state.HealthHistory, HealthRecord{
		CheckedAt:           now,
		Status:              state.Status,
		Error:               state.Error,
		DependenciesHealthy: dependenciesHealthy,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
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
	state.HealthHistory = appendBounded(state.HealthHistory, HealthRecord{CheckedAt: state.LastChecked, Status: StatusDisabled, DependenciesHealthy: state.DependenciesHealthy})
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

func (r *Registry) recordDiscoveryFailure(id string, started time.Time, checks []DependencyState, serverName, serverVersion, protocolVersion string, err error) {
	now := time.Now().UTC()
	detail := boundedRegistryDetail(err.Error())
	dependenciesHealthy := dependencyFailure(checks) == nil
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[id]
	if !ok {
		return
	}
	state.Status = StatusUnhealthy
	state.Error = detail
	state.DependenciesChecked = true
	state.DependenciesHealthy = dependenciesHealthy
	state.DependencyChecks = append([]DependencyState(nil), checks...)
	state.LastChecked = now
	state.HealthHistory = appendBounded(state.HealthHistory, HealthRecord{
		CheckedAt:           now,
		Status:              StatusUnhealthy,
		Error:               detail,
		DependenciesHealthy: dependenciesHealthy,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
	state.DiscoveryHistory = appendBounded(state.DiscoveryHistory, DiscoveryRecord{
		DiscoveredAt:        now,
		Status:              StatusUnhealthy,
		Error:               detail,
		ServerName:          serverName,
		ServerVersion:       serverVersion,
		ProtocolVersion:     protocolVersion,
		DependenciesHealthy: dependenciesHealthy,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
	r.states[id] = state
}

func (r *Registry) recordHealthFailure(id string, started time.Time, checks []DependencyState, err error) {
	now := time.Now().UTC()
	detail := boundedRegistryDetail(err.Error())
	dependenciesHealthy := dependencyFailure(checks) == nil
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.states[id]
	if !ok {
		return
	}
	state.Status = StatusUnhealthy
	state.Error = detail
	state.DependenciesChecked = true
	state.DependenciesHealthy = dependenciesHealthy
	state.DependencyChecks = append([]DependencyState(nil), checks...)
	state.LastChecked = now
	state.HealthHistory = appendBounded(state.HealthHistory, HealthRecord{
		CheckedAt:           now,
		Status:              StatusUnhealthy,
		Error:               detail,
		DependenciesHealthy: dependenciesHealthy,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
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
	started := time.Now()
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.RLock()
	manifest, ok := r.manifests[id]
	session := r.sessions[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown MCP server %q", id)
	}
	if !manifest.Enabled {
		return fmt.Errorf("MCP server %s is disabled", id)
	}
	now := time.Now().UTC()
	dependencyChecks, _ := inspectDependencies(manifest.Dependencies, now)
	if dependencyErr := dependencyFailure(dependencyChecks); dependencyErr != nil {
		r.recordHealthFailure(id, started, dependencyChecks, dependencyErr)
		return dependencyErr
	}
	if session == nil {
		err := fmt.Errorf("MCP server %s is not connected", id)
		r.recordHealthFailure(id, started, dependencyChecks, err)
		return err
	}
	if err := session.Ping(ctx, nil); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}
		_ = session.Close()
		r.mu.Lock()
		if r.sessions[id] == session {
			delete(r.sessions, id)
			delete(r.clients, id)
		}
		r.mu.Unlock()
		r.recordHealthFailure(id, started, dependencyChecks, err)
		return err
	}
	now = time.Now().UTC()
	r.mu.Lock()
	state := r.states[id]
	state.Status = StatusHealthy
	state.Error = ""
	state.DependenciesChecked = true
	state.DependenciesHealthy = true
	state.DependencyChecks = dependencyChecks
	state.LastChecked = now
	state.HealthHistory = appendBounded(state.HealthHistory, HealthRecord{
		CheckedAt:           now,
		Status:              state.Status,
		Error:               state.Error,
		DependenciesHealthy: true,
		DurationMillis:      time.Since(started).Milliseconds(),
	})
	r.states[id] = state
	r.mu.Unlock()
	return nil
}

func (r *Registry) States() []ServerState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ServerState, 0, len(r.states))
	for _, state := range r.states {
		state.Manifest.Arguments = append([]string(nil), state.Manifest.Arguments...)
		state.Manifest.Capabilities = append([]string(nil), state.Manifest.Capabilities...)
		state.Manifest.Dependencies = append([]string(nil), state.Manifest.Dependencies...)
		state.Tools = append([]ToolRecord(nil), state.Tools...)
		state.DependencyChecks = append([]DependencyState(nil), state.DependencyChecks...)
		state.HealthHistory = append([]HealthRecord(nil), state.HealthHistory...)
		state.DiscoveryHistory = append([]DiscoveryRecord(nil), state.DiscoveryHistory...)
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
