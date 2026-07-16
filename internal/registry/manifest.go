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

type ServerState struct {
	Manifest            ServerManifest `json:"manifest"`
	Status              Status         `json:"status"`
	Error               string         `json:"error,omitempty"`
	Tools               []ToolRecord   `json:"tools"`
	ToolSetHash         string         `json:"tool_set_hash,omitempty"`
	PreviousToolSetHash string         `json:"previous_tool_set_hash,omitempty"`
	ToolsChanged        bool           `json:"tools_changed"`
	LastChecked         time.Time      `json:"last_checked"`
}
