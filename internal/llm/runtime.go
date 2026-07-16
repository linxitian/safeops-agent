package llm

import (
	"context"
	"strings"
	"sync"
	"time"
)

type PublicConfig struct {
	Configured        bool       `json:"configured"`
	BaseURL           string     `json:"base_url,omitempty"`
	Model             string     `json:"model,omitempty"`
	APIKeyConfigured  bool       `json:"api_key_configured"`
	Source            string     `json:"source,omitempty"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
	LastConfiguration string     `json:"last_configuration,omitempty"`
}

type RuntimeProvider struct {
	mu       sync.RWMutex
	provider Provider
	config   Config
	public   PublicConfig
}

func NewRuntimeProvider() *RuntimeProvider { return &RuntimeProvider{} }

func (p *RuntimeProvider) Configure(config Config, source string, updatedAt time.Time) error {
	provider, err := NewOpenAICompatible(config)
	if err != nil {
		return err
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "runtime"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	updatedAt = updatedAt.UTC()
	p.provider = provider
	p.config = Config{BaseURL: strings.TrimSpace(config.BaseURL), APIKey: strings.TrimSpace(config.APIKey), Model: strings.TrimSpace(config.Model)}
	p.public = PublicConfig{Configured: true, BaseURL: p.config.BaseURL, Model: p.config.Model, APIKeyConfigured: p.config.APIKey != "", Source: source, UpdatedAt: &updatedAt, LastConfiguration: "configured"}
	return nil
}

func (p *RuntimeProvider) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.provider = nil
	p.config = Config{}
	p.public = PublicConfig{Configured: false, LastConfiguration: "not configured"}
}

func (p *RuntimeProvider) Status() PublicConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.public.Configured {
		return PublicConfig{Configured: false, LastConfiguration: "not configured"}
	}
	return p.public
}

func (p *RuntimeProvider) Config() Config {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.config
}

func (p *RuntimeProvider) Decide(ctx context.Context, input DecisionRequest) (Decision, error) {
	p.mu.RLock()
	provider := p.provider
	p.mu.RUnlock()
	if provider == nil {
		return Decision{}, ErrNotConfigured
	}
	return provider.Decide(ctx, input)
}
