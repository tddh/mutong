package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config CLI runtime configuration.
type Config struct {
	Server    string
	Namespace string
	AdminKey  string
}

type configFile struct {
	CurrentContext string             `mapstructure:"current-context"`
	Contexts       map[string]context `mapstructure:"contexts"`
}

type context struct {
	Server    string `mapstructure:"server"`
	Namespace string `mapstructure:"namespace"`
	AdminKey  string `mapstructure:"admin_key"`
}

// Load reads config file, env vars override. path="" uses ~/.mutong/config.yaml.
func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: "http://localhost:8888",
	}
	v := viper.New()
	v.SetConfigType("yaml")
	if path != "" {
		v.SetConfigFile(path)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("finding home directory: %w", err)
		}
		v.SetConfigFile(filepath.Join(home, ".mutong", "config.yaml"))
	}
	if err := v.ReadInConfig(); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	} else {
		var f configFile
		if err := v.Unmarshal(&f); err != nil {
			return nil, fmt.Errorf("unmarshaling config: %w", err)
		}
		if ctx, ok := f.Contexts[f.CurrentContext]; ok {
			if ctx.Server != "" {
				cfg.Server = ctx.Server
			}
			if ctx.Namespace != "" {
				cfg.Namespace = ctx.Namespace
			}
			if ctx.AdminKey != "" {
				cfg.AdminKey = ctx.AdminKey
			}
		}
	}
	if env := os.Getenv("MUTONG_SERVER"); env != "" {
		cfg.Server = env
	}
	if env := os.Getenv("MUTONG_NAMESPACE"); env != "" {
		cfg.Namespace = env
	}
	if env := os.Getenv("MUTONG_ADMIN_KEY"); env != "" {
		cfg.AdminKey = env
	}
	return cfg, nil
}
