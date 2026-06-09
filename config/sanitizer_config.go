package config

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
