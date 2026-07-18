package executor

import (
	"errors"
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
	ReadFileRoots             []string `yaml:"read_file_roots"`
	AllowedFileRoots          []string `yaml:"allowed_file_roots"`
	LabFileRoots              []string `yaml:"lab_file_roots"`
	QuarantineRoot            string   `yaml:"quarantine_root"`
	AllowedProcessExecutables []string `yaml:"allowed_process_executables"`
}

type AllowlistStatus struct {
	ConfigPath              string    `json:"config_path"`
	ReadOnlyRoots           []string  `json:"read_only_roots"`
	ManagedRoots            []string  `json:"managed_roots"`
	CandidateRoots          []string  `json:"candidate_roots"`
	AllowedFileRoots        []string  `json:"allowed_file_roots"`
	QuarantineRoot          string    `json:"quarantine_root"`
	MissingRoots            []string  `json:"missing_roots"`
	RequiresExecutorRestart bool      `json:"requires_executor_restart"`
	WriteActionsEnabled     bool      `json:"write_actions_enabled"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
}

type PathBrowserEntry struct {
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	IsDir           bool      `json:"is_dir"`
	SizeBytes       int64     `json:"size_bytes"`
	Mode            string    `json:"mode"`
	Modified        time.Time `json:"modified"`
	SelectableRead  bool      `json:"selectable_read"`
	SelectableWrite bool      `json:"selectable_write"`
}

type PathBrowser struct {
	Path             string             `json:"path"`
	Parent           string             `json:"parent,omitempty"`
	Mode             string             `json:"mode"`
	ReadOnlyRoots    []string           `json:"read_only_roots"`
	ManagedRoots     []string           `json:"managed_roots"`
	CandidateRoots   []string           `json:"candidate_roots"`
	Entries          []PathBrowserEntry `json:"entries"`
	Truncated        bool               `json:"truncated"`
	CanSelectRead    bool               `json:"can_select_read"`
	CanSelectWrite   bool               `json:"can_select_write"`
	CanCreateChild   bool               `json:"can_create_child"`
	WriteRootMissing bool               `json:"write_root_missing"`
}

type ConfigManager struct {
	mu           sync.Mutex
	path         string
	config       Config
	maximumRoots []string
	targets      *MutableTargets
}

var ErrInvalidManagedRoots = errors.New("invalid managed roots")

type managedRootsConfig struct {
	SchemaVersion int      `yaml:"schema_version"`
	ManagedRoots  []string `yaml:"managed_roots"`
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
	if config.ReadFileRoots, err = normalizeReadOnlyRoots(config.ReadFileRoots); err != nil {
		return Config{}, fmt.Errorf("read_file_roots: %w", err)
	}
	if config.AllowedFileRoots, err = normalizeAllowlistRoots(config.AllowedFileRoots, ""); err != nil {
		return Config{}, fmt.Errorf("allowed_file_roots: %w", err)
	}
	if err := validateRootsWithin(config.LabFileRoots, config.AllowedFileRoots); err != nil {
		return Config{}, fmt.Errorf("lab_file_roots: %w", err)
	}
	if err := validateRootsWithin([]string{config.QuarantineRoot}, config.AllowedFileRoots); err != nil {
		return Config{}, fmt.Errorf("quarantine_root: %w", err)
	}
	return config, nil
}

func NewConfigManager(path string, config Config, targets *MutableTargets) (*ConfigManager, error) {
	path = filepath.Clean(path)
	maximumRoots := append([]string(nil), config.LabFileRoots...)
	managedRoots := append([]string(nil), maximumRoots...)
	if value, err := loadManagedRoots(path); err == nil {
		managedRoots = value
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	normalized, err := normalizeAllowlistRoots(managedRoots, config.QuarantineRoot)
	if err != nil {
		return nil, fmt.Errorf("load managed roots: %w", err)
	}
	if err := validateRootsWithin(normalized, maximumRoots); err != nil {
		return nil, fmt.Errorf("load managed roots: %w", err)
	}
	config.LabFileRoots = normalized
	config.AllowedFileRoots = appendUniquePaths(append([]string(nil), normalized...), config.QuarantineRoot)
	manager := &ConfigManager{path: path, config: config, maximumRoots: maximumRoots, targets: targets}
	if targets != nil {
		targets.UpdateAllowedFileRoots(config.AllowedFileRoots)
	}
	return manager, nil
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
		return AllowlistStatus{}, fmt.Errorf("%w: %v", ErrInvalidManagedRoots, err)
	}
	if err := validateRootsWithin(normalized, m.maximumRoots); err != nil {
		return AllowlistStatus{}, fmt.Errorf("%w: %v", ErrInvalidManagedRoots, err)
	}
	next := m.config
	next.LabFileRoots = normalized
	next.AllowedFileRoots = appendUniquePaths(append([]string(nil), normalized...), next.QuarantineRoot)
	if err := saveManagedRoots(m.path, normalized); err != nil {
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
		ReadOnlyRoots:           append([]string(nil), m.config.ReadFileRoots...),
		ManagedRoots:            append([]string(nil), m.config.LabFileRoots...),
		CandidateRoots:          candidateManagedRoots(m.maximumRoots),
		AllowedFileRoots:        append([]string(nil), m.config.AllowedFileRoots...),
		QuarantineRoot:          m.config.QuarantineRoot,
		RequiresExecutorRestart: false,
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

func (m *ConfigManager) BrowsePath(path, mode string, limit int) (PathBrowser, error) {
	m.mu.Lock()
	config := m.config
	maximumRoots := append([]string(nil), m.maximumRoots...)
	m.mu.Unlock()
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "read"
	}
	if mode != "read" && mode != "write" {
		return PathBrowser{}, fmt.Errorf("unsupported browser mode %q", mode)
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		return PathBrowser{}, fmt.Errorf("limit must not exceed 500")
	}
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "." || clean == "" {
		if mode == "write" && len(maximumRoots) > 0 {
			clean = maximumRoots[0]
		} else {
			clean = string(filepath.Separator)
		}
	}
	if !filepath.IsAbs(clean) {
		return PathBrowser{}, fmt.Errorf("path must be absolute")
	}
	roots := config.ReadFileRoots
	if mode == "write" {
		roots = maximumRoots
	}
	if !withinAny(clean, roots) {
		return PathBrowser{}, fmt.Errorf("path %s is outside %s roots", clean, mode)
	}
	browser := PathBrowser{
		Path:           clean,
		Mode:           mode,
		ReadOnlyRoots:  append([]string(nil), config.ReadFileRoots...),
		ManagedRoots:   append([]string(nil), config.LabFileRoots...),
		CandidateRoots: candidateManagedRoots(maximumRoots),
		CanSelectRead:  withinAny(clean, config.ReadFileRoots),
		CanSelectWrite: withinAny(clean, maximumRoots),
	}
	if parent := filepath.Dir(clean); parent != clean {
		browser.Parent = parent
	}
	info, err := os.Stat(clean)
	if err != nil {
		if mode == "write" && os.IsNotExist(err) {
			browser.WriteRootMissing = true
			return browser, nil
		}
		return browser, err
	}
	if !info.IsDir() {
		return browser, fmt.Errorf("path is not a directory")
	}
	browser.CanCreateChild = mode == "write" && browser.CanSelectWrite
	entries, err := os.ReadDir(clean)
	if err != nil {
		return browser, err
	}
	browser.Truncated = len(entries) > limit
	if len(entries) > limit {
		entries = entries[:limit]
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.Join(clean, entry.Name())
		browser.Entries = append(browser.Entries, PathBrowserEntry{Name: entry.Name(), Path: entryPath, IsDir: true, SizeBytes: info.Size(), Mode: info.Mode().String(), Modified: info.ModTime().UTC(), SelectableRead: withinAny(entryPath, config.ReadFileRoots), SelectableWrite: withinAny(entryPath, maximumRoots)})
	}
	return browser, nil
}

func (m *ConfigManager) CreateDirectory(parent, name string) (PathBrowser, error) {
	m.mu.Lock()
	maximumRoots := append([]string(nil), m.maximumRoots...)
	m.mu.Unlock()
	parent = filepath.Clean(strings.TrimSpace(parent))
	name = strings.TrimSpace(name)
	if !filepath.IsAbs(parent) {
		return PathBrowser{}, fmt.Errorf("parent path must be absolute")
	}
	if !safeDirectoryName(name) {
		return PathBrowser{}, fmt.Errorf("directory name is invalid")
	}
	if !withinAny(parent, maximumRoots) {
		return PathBrowser{}, fmt.Errorf("parent %s is outside writable administrator-defined roots", parent)
	}
	path := filepath.Join(parent, name)
	if !withinAny(path, maximumRoots) {
		return PathBrowser{}, fmt.Errorf("directory path escapes writable roots")
	}
	if err := os.Mkdir(path, 0o750); err != nil {
		return PathBrowser{}, err
	}
	if err := syncConfigDirectory(parent); err != nil {
		return PathBrowser{}, err
	}
	return m.BrowsePath(path, "write", 200)
}

func candidateManagedRoots(maximumRoots []string) []string {
	out := make([]string, 0, len(maximumRoots))
	for _, root := range maximumRoots {
		root = filepath.Clean(root)
		out = appendUniquePaths(out, root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if len(out) >= 200 {
				return out
			}
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			out = appendUniquePaths(out, filepath.Join(root, name))
		}
	}
	return out
}

func loadManagedRoots(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stored managedRootsConfig
	if err := yaml.Unmarshal(b, &stored); err != nil {
		return nil, fmt.Errorf("parse managed roots config: %w", err)
	}
	if stored.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported managed roots schema %d", stored.SchemaVersion)
	}
	return stored.ManagedRoots, nil
}

func saveManagedRoots(path string, roots []string) error {
	b, err := yaml.Marshal(managedRootsConfig{SchemaVersion: 1, ManagedRoots: roots})
	if err != nil {
		return err
	}
	return saveConfigBytes(path, b, 0o640)
}

func SaveConfig(path string, config Config) error {
	config.SchemaVersion = 1
	b, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return saveConfigBytes(path, b, 0o644)
}

func saveConfigBytes(path string, b []byte, defaultMode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	mode := defaultMode
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
		if quarantineRoot != "" && (sameOrInside(root, quarantineRoot) || sameOrInside(quarantineRoot, root)) {
			return nil, fmt.Errorf("managed root %s must not overlap quarantine root %s", root, quarantineRoot)
		}
		out = appendUniquePaths(out, root)
	}
	return out, nil
}

func normalizeReadOnlyRoots(values []string) ([]string, error) {
	if len(values) == 0 {
		values = []string{string(filepath.Separator)}
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("path is required")
		}
		if !filepath.IsAbs(value) {
			return nil, fmt.Errorf("path %q must be absolute", value)
		}
		clean := filepath.Clean(value)
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			clean = resolved
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		out = appendUniquePaths(out, clean)
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
	if clean == string(filepath.Separator) {
		return "", fmt.Errorf("root filesystem is too broad for allowlist")
	}
	return clean, nil
}

func validateRootsWithin(roots, maximumRoots []string) error {
	for _, root := range roots {
		allowed := false
		for _, maximum := range maximumRoots {
			if sameOrInside(root, maximum) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("managed root %s is outside administrator-defined roots", root)
		}
	}
	return nil
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

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if sameOrInside(path, root) {
			return true
		}
	}
	return false
}

func safeDirectoryName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 128 {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
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
