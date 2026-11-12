package config

import "fmt"

type SanitizerRule struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
	Enabled bool   `yaml:"enabled"`
}

type SanitizerConfig struct {
	Enabled                 bool            `yaml:"enabled"`
	HighPIIBlockThreshold   float64         `yaml:"high_pii_block_threshold"`
	BannedTerms             []string        `yaml:"banned_terms"`
	MaxQueryLength          int             `yaml:"max_query_length"`
	PromptInjectionPatterns []string        `yaml:"prompt_injection_patterns"`
	Rules                   []SanitizerRule `yaml:"rules"`
}

// ApplyDefaults 用默认值填补零值字段。YAML 缺省字段时若不回落，
// sanitizer 会以空注入模式/零长度上限运行。
func (c *SanitizerConfig) ApplyDefaults() {
	d := DefaultSanitizerConfig()
	if c.HighPIIBlockThreshold == 0 {
		c.HighPIIBlockThreshold = d.HighPIIBlockThreshold
	}
	if c.MaxQueryLength == 0 {
		c.MaxQueryLength = d.MaxQueryLength
	}
	if len(c.PromptInjectionPatterns) == 0 {
		c.PromptInjectionPatterns = d.PromptInjectionPatterns
	}
	if len(c.Rules) == 0 {
		c.Rules = d.Rules
	}
}

func DefaultSanitizerConfig() SanitizerConfig {
	return SanitizerConfig{
		Enabled:               true,
		HighPIIBlockThreshold: 0.5,
		MaxQueryLength:        200,
		PromptInjectionPatterns: []string{
			"ignore previous",
			"you are now",
			"system prompt",
			"<script>",
			"javascript:",
		},
		Rules: []SanitizerRule{
			{Name: "ip_v4", Pattern: `\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`, Enabled: true},
			{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Enabled: true},
			{Name: "jwt", Pattern: `eyJ[A-Za-z0-9_-]{20,}`, Enabled: true},
			{Name: "secret", Pattern: `(?i)(password|passwd|secret)\s*[:=]\s*\S+`, Enabled: true},
			{Name: "aws_key", Pattern: `(AKIA|ABIA|ACCA|ASIA)[A-Za-z0-9]{16,}`, Enabled: false},
			{Name: "ali_key", Pattern: `(LTAI)[A-Za-z0-9]{12,}`, Enabled: false},
		},
	}
}

func (c *SanitizerConfig) Validate() error {
	for _, r := range c.Rules {
		if r.Enabled && r.Pattern == "" {
			return fmt.Errorf("sanitizer rule %q has empty pattern", r.Name)
		}
	}
	return nil
}

type ExternalSearchConfig struct {
	Enabled      bool         `yaml:"enabled" json:"enabled"`
	AuditEnabled bool         `yaml:"auditEnabled" json:"auditEnabled"`
	Tavily       TavilyConfig `yaml:"tavily" json:"tavily"`
	GitHub       GitHubConfig `yaml:"github" json:"github"`
}

type TavilyConfig struct {
	APIKey     string `yaml:"apiKey" json:"apiKey"`
	Endpoint   string `yaml:"endpoint" json:"endpoint"`
	TimeoutSec int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxResults int    `yaml:"maxResults" json:"maxResults"`
}

type GitHubConfig struct {
	Token      string `yaml:"token" json:"token"`
	Endpoint   string `yaml:"endpoint" json:"endpoint"`
	TimeoutSec int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	MaxResults int    `yaml:"maxResults" json:"maxResults"`
}

func DefaultExternalSearchConfig() ExternalSearchConfig {
	return ExternalSearchConfig{
		Enabled:      false,
		AuditEnabled: true,
		Tavily: TavilyConfig{
			Endpoint:   "https://api.tavily.com/search",
			TimeoutSec: 15,
			MaxResults: 3,
		},
		GitHub: GitHubConfig{
			Endpoint:   "https://api.github.com",
			TimeoutSec: 10,
			MaxResults: 3,
		},
	}
}
