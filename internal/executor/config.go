package executor

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
)

type Config struct {
	SchemaVersion             int      `yaml:"schema_version"`
	AllowedServices           []string `yaml:"allowed_services"`
	AllowedFileRoots          []string `yaml:"allowed_file_roots"`
	LabFileRoots              []string `yaml:"lab_file_roots"`
	QuarantineRoot            string   `yaml:"quarantine_root"`
	AllowedProcessExecutables []string `yaml:"allowed_process_executables"`
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
	return config, nil
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
