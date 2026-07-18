package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerManifest struct {
	ID           string            `yaml:"id" json:"id"`
	Name         string            `yaml:"name" json:"name"`
	DisplayName  string            `yaml:"display_name" json:"display_name"`
	Version      string            `yaml:"version" json:"version"`
	Description  string            `yaml:"description" json:"description"`
	Transport    string            `yaml:"transport" json:"transport"`
	Command      string            `yaml:"command" json:"command"`
	Arguments    []string          `yaml:"arguments" json:"arguments"`
	Enabled      bool              `yaml:"enabled" json:"enabled"`
	HealthCheck  string            `yaml:"health_check" json:"health_check"`
	Source       string            `yaml:"source" json:"source"`
	Capabilities []string          `yaml:"capabilities" json:"capabilities"`
	Dependencies []string          `yaml:"dependencies" json:"dependencies"`
	Environment  map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
}

type Config struct {
	Servers []ServerManifest `yaml:"servers" json:"servers"`
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read registry config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse registry config: %w", err)
	}
	seen := map[string]bool{}
	for i := range cfg.Servers {
		m := &cfg.Servers[i]
		if m.ID == "" || m.Name == "" {
			return Config{}, fmt.Errorf("server %d: id and name are required", i)
		}
		if seen[m.ID] {
			return Config{}, fmt.Errorf("duplicate server id %q", m.ID)
		}
		seen[m.ID] = true
		if m.Transport != "stdio" {
			return Config{}, fmt.Errorf("server %s: unsupported transport %q", m.ID, m.Transport)
		}
		if m.Command == "" {
			return Config{}, fmt.Errorf("server %s: command required", m.ID)
		}
	}
	return cfg, nil
}

type Status string

const (
	StatusDisabled  Status = "DISABLED"
	StatusStarting  Status = "STARTING"
	StatusHealthy   Status = "HEALTHY"
	StatusUnhealthy Status = "UNHEALTHY"
)

type ToolRecord struct {
	ServerID      string          `json:"server_id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	InputSchema   json.RawMessage `json:"input_schema"`
	SchemaHash    string          `json:"schema_hash"`
	DiscoveredAt  time.Time       `json:"discovered_at"`
	ServerVersion string          `json:"server_version"`
}

type DependencyState struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Available bool      `json:"available"`
	Resolved  string    `json:"resolved,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type HealthRecord struct {
	CheckedAt           time.Time `json:"checked_at"`
	Status              Status    `json:"status"`
	Error               string    `json:"error,omitempty"`
	DependenciesHealthy bool      `json:"dependencies_healthy"`
	DurationMillis      int64     `json:"duration_millis"`
}

type DiscoveryRecord struct {
	DiscoveredAt  time.Time `json:"discovered_at"`
	ServerName    string    `json:"server_name"`
	ServerVersion string    `json:"server_version"`
	ToolSetHash   string    `json:"tool_set_hash"`
	ToolCount     int       `json:"tool_count"`
	ToolsChanged  bool      `json:"tools_changed"`
}

type ServerState struct {
	Manifest            ServerManifest    `json:"manifest"`
	Status              Status            `json:"status"`
	Error               string            `json:"error,omitempty"`
	ActualServerName    string            `json:"actual_server_name,omitempty"`
	ActualServerVersion string            `json:"actual_server_version,omitempty"`
	Tools               []ToolRecord      `json:"tools"`
	ToolSetHash         string            `json:"tool_set_hash,omitempty"`
	PreviousToolSetHash string            `json:"previous_tool_set_hash,omitempty"`
	ToolsChanged        bool              `json:"tools_changed"`
	DependenciesChecked bool              `json:"dependencies_checked"`
	DependenciesHealthy bool              `json:"dependencies_healthy"`
	DependencyChecks    []DependencyState `json:"dependency_checks"`
	HealthHistory       []HealthRecord    `json:"health_history"`
	DiscoveryHistory    []DiscoveryRecord `json:"discovery_history"`
	LastChecked         time.Time         `json:"last_checked"`
}
