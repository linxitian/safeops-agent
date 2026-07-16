package guard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"safeops-agent/contracts"
	"strings"
)

type ToolPolicy struct {
	Name               string              `yaml:"name"`
	Effect             contracts.Effect    `yaml:"effect"`
	BaseRisk           contracts.RiskLevel `yaml:"base_risk"`
	Approval           bool                `yaml:"approval"`
	Reversible         bool                `yaml:"reversible"`
	AllowedTargetTypes []string            `yaml:"allowed_target_types"`
	RuleID             string              `yaml:"rule_id"`
}
type catalogFile struct {
	SchemaVersion int          `yaml:"schema_version"`
	Tools         []ToolPolicy `yaml:"tools"`
}
type Catalog struct {
	SchemaVersion int
	Version       string
	tools         map[string]ToolPolicy
}

func LoadCatalog(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tool policy: %w", err)
	}
	var raw catalogFile
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("parse tool policy: %w", err)
	}
	if raw.SchemaVersion != 1 {
		return nil, fmt.Errorf("unsupported policy schema version %d", raw.SchemaVersion)
	}
	catalog := &Catalog{SchemaVersion: raw.SchemaVersion, tools: map[string]ToolPolicy{}}
	for _, policy := range raw.Tools {
		if policy.Name == "" || policy.RuleID == "" {
			return nil, errors.New("tool policy requires name and rule_id")
		}
		if policy.Effect != contracts.Read && policy.Effect != contracts.Write {
			return nil, fmt.Errorf("tool %s has invalid effect", policy.Name)
		}
		if _, exists := catalog.tools[policy.Name]; exists {
			return nil, fmt.Errorf("duplicate tool policy %s", policy.Name)
		}
		catalog.tools[policy.Name] = policy
	}
	sum := sha256.Sum256(b)
	catalog.Version = "sha256:" + hex.EncodeToString(sum[:])
	return catalog, nil
}
func (c *Catalog) Policy(tool string) (ToolPolicy, bool) {
	policy, ok := c.tools[strings.TrimSpace(tool)]
	return policy, ok
}
func (c *Catalog) VersionID() string {
	if c == nil {
		return ""
	}
	return c.Version
}
