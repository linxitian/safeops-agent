package executor

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	SchemaVersion             int      `yaml:"schema_version"`
	AllowedServices           []string `yaml:"allowed_services"`
	AllowedFileRoots          []string `yaml:"allowed_file_roots"`
	LabFileRoots              []string `yaml:"lab_file_roots"`
	QuarantineRoot            string   `yaml:"quarantine_root"`
	AllowedProcessExecutables []string `yaml:"allowed_process_executables"`
}

type AllowlistStatus struct {
	ConfigPath              string    `json:"config_path"`
	ManagedRoots            []string  `json:"managed_roots"`
	AllowedFileRoots        []string  `json:"allowed_file_roots"`
	QuarantineRoot          string    `json:"quarantine_root"`
	MissingRoots            []string  `json:"missing_roots"`
	RequiresExecutorRestart bool      `json:"requires_executor_restart"`
	WriteActionsEnabled     bool      `json:"write_actions_enabled"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
}

type ConfigManager struct {
	mu      sync.Mutex
	path    string
	config  Config
	targets *MutableTargets
}

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read executor config: %w", err)
	}
	var config Config
	if err := yaml.Unmarshal(b, &config); err != nil {
		return Config{}, fmt.Errorf("parse executor config: %w", err)
	}
	if config.SchemaVersion != 1 {
		return Config{}, fmt.Errorf("unsupported executor config schema %d", config.SchemaVersion)
	}
	for i, value := range config.AllowedServices {
		config.AllowedServices[i] = strings.ToLower(strings.TrimSpace(value))
	}
	if len(config.LabFileRoots) == 0 || config.QuarantineRoot == "" {
		return Config{}, fmt.Errorf("lab_file_roots and quarantine_root are required")
	}
	config.QuarantineRoot, err = normalizeAllowlistRoot(config.QuarantineRoot)
	if err != nil {
		return Config{}, fmt.Errorf("quarantine_root: %w", err)
	}
	if config.LabFileRoots, err = normalizeAllowlistRoots(config.LabFileRoots, config.QuarantineRoot); err != nil {
		return Config{}, fmt.Errorf("lab_file_roots: %w", err)
	}
	if config.AllowedFileRoots, err = normalizeAllowlistRoots(config.AllowedFileRoots, ""); err != nil {
		return Config{}, fmt.Errorf("allowed_file_roots: %w", err)
	}
	return config, nil
}

func NewConfigManager(path string, config Config, targets *MutableTargets) *ConfigManager {
	return &ConfigManager{path: filepath.Clean(path), config: config, targets: targets}
}

func (m *ConfigManager) Status() AllowlistStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *ConfigManager) UpdateManagedRoots(roots []string) (AllowlistStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	normalized, err := normalizeAllowlistRoots(roots, m.config.QuarantineRoot)
	if err != nil {
		return AllowlistStatus{}, err
	}
	next := m.config
	next.LabFileRoots = normalized
	next.AllowedFileRoots = appendUniquePaths(append([]string(nil), normalized...), next.QuarantineRoot)
	if err := SaveConfig(m.path, next); err != nil {
		return AllowlistStatus{}, err
	}
	m.config = next
	if m.targets != nil {
		m.targets.UpdateAllowedFileRoots(next.AllowedFileRoots)
	}
	return m.statusLocked(), nil
}

func (m *ConfigManager) statusLocked() AllowlistStatus {
	status := AllowlistStatus{
		ConfigPath:              m.path,
		ManagedRoots:            append([]string(nil), m.config.LabFileRoots...),
		AllowedFileRoots:        append([]string(nil), m.config.AllowedFileRoots...),
		QuarantineRoot:          m.config.QuarantineRoot,
		RequiresExecutorRestart: true,
	}
	if info, err := os.Stat(m.path); err == nil {
		status.UpdatedAt = info.ModTime().UTC()
	}
	for _, root := range status.ManagedRoots {
		if _, err := os.Stat(root); err != nil {
			status.MissingRoots = append(status.MissingRoots, root)
		}
	}
	return status
}

func SaveConfig(path string, config Config) error {
	config.SchemaVersion = 1
	b, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(dir, ".executor-config-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(b); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncConfigDirectory(dir)
}

func (c Config) Scope() FixedScope {
	services := map[string]bool{}
	for _, service := range c.AllowedServices {
		if service != "" {
			services[service] = true
		}
	}
	return FixedScope{AllowedServices: services, AllowedFileRoots: c.AllowedFileRoots, AllowedProcessExecutables: c.AllowedProcessExecutables}
}

func normalizeAllowlistRoots(values []string, quarantineRoot string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one managed root is required")
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		root, err := normalizeAllowlistRoot(value)
		if err != nil {
			return nil, err
		}
		if quarantineRoot != "" && sameOrInside(root, quarantineRoot) {
			return nil, fmt.Errorf("managed root %s must not be inside quarantine root %s", root, quarantineRoot)
		}
		out = appendUniquePaths(out, root)
	}
	return out, nil
}

func normalizeAllowlistRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("path %q must be absolute", value)
	}
	clean := filepath.Clean(value)
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("root filesystem is too broad for allowlist")
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = resolved
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return clean, nil
}

func appendUniquePaths(values []string, next string) []string {
	next = filepath.Clean(next)
	for _, value := range values {
		if filepath.Clean(value) == next {
			return values
		}
	}
	return append(values, next)
}

func sameOrInside(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func syncConfigDirectory(directory string) error {
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
