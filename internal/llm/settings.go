package llm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type StoredConfig struct {
	SchemaVersion int       `json:"schema_version"`
	BaseURL       string    `json:"base_url"`
	APIKey        string    `json:"api_key"`
	Model         string    `json:"model"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type SettingsStore struct {
	path string
}

func NewSettingsStore(path string) *SettingsStore {
	return &SettingsStore{path: filepath.Clean(path)}
}

func (s *SettingsStore) Load() (StoredConfig, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return StoredConfig{}, ErrNotConfigured
	}
	if err != nil {
		return StoredConfig{}, err
	}
	var config StoredConfig
	if err := json.Unmarshal(b, &config); err != nil {
		return StoredConfig{}, err
	}
	if config.SchemaVersion != 1 {
		return StoredConfig{}, errors.New("unsupported LLM settings schema")
	}
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.APIKey = strings.TrimSpace(config.APIKey)
	config.Model = strings.TrimSpace(config.Model)
	if config.BaseURL == "" || config.APIKey == "" || config.Model == "" {
		return StoredConfig{}, errors.New("stored LLM settings are incomplete")
	}
	return config, nil
}

func (s *SettingsStore) Save(config Config, updatedAt time.Time) (StoredConfig, error) {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	stored := StoredConfig{SchemaVersion: 1, BaseURL: strings.TrimSpace(config.BaseURL), APIKey: strings.TrimSpace(config.APIKey), Model: strings.TrimSpace(config.Model), UpdatedAt: updatedAt.UTC()}
	if stored.BaseURL == "" || stored.APIKey == "" || stored.Model == "" {
		return StoredConfig{}, errors.New("base_url, api_key, and model are required")
	}
	b, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return StoredConfig{}, err
	}
	b = append(b, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return StoredConfig{}, err
	}
	temp, err := os.CreateTemp(dir, ".llm-settings-*.tmp")
	if err != nil {
		return StoredConfig{}, err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return StoredConfig{}, err
	}
	if _, err := temp.Write(b); err != nil {
		temp.Close()
		return StoredConfig{}, err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return StoredConfig{}, err
	}
	if err := temp.Close(); err != nil {
		return StoredConfig{}, err
	}
	if err := os.Rename(name, s.path); err != nil {
		return StoredConfig{}, err
	}
	if err := syncDirectory(dir); err != nil {
		return StoredConfig{}, err
	}
	return stored, nil
}

func (s *SettingsStore) Delete() error {
	if err := os.Remove(s.path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(s.path))
}

func (c StoredConfig) Config() Config {
	return Config{BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model}
}

func syncDirectory(directory string) error {
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
