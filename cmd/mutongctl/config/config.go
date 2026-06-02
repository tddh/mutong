package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type configFile struct {
	CurrentContext string             `yaml:"current-context"`
	Contexts       map[string]context `yaml:"contexts"`
}

type context struct {
	Server    string `yaml:"server"`
	Namespace string `yaml:"namespace"`
}

type Factory struct {
	IO *iostreams.IOStreams
}

func NewCmdConfig(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage mutongctl config file (~/.mutong/config.yaml)",
	}
	cmd.AddCommand(newCmdConfigGet(f))
	cmd.AddCommand(newCmdConfigSet(f))
	cmd.AddCommand(newCmdConfigContexts(f))
	cmd.AddCommand(newCmdConfigUseContext(f))
	return cmd
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".mutong", "config.yaml"), nil
}

func loadConfigFile() (*configFile, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if os.IsNotExist(err) {
			return &configFile{Contexts: map[string]context{}}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var f configFile
	if err := v.Unmarshal(&f); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}
	if f.Contexts == nil {
		f.Contexts = map[string]context{}
	}
	return &f, nil
}

func saveConfigFile(cfg *configFile) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func newCmdConfigGet(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show current context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFile()
			if err != nil {
				return err
			}
			if cfg.CurrentContext == "" {
				fmt.Fprintln(f.IO.Out, "No current context set. Use 'mutongctl config use-context <name>'")
				return nil
			}
			ctx, ok := cfg.Contexts[cfg.CurrentContext]
			if !ok {
				fmt.Fprintf(f.IO.Out, "Current context '%s' not found in config\n", cfg.CurrentContext)
				return nil
			}
			fmt.Fprintf(f.IO.Out, "Context: %s\n", cfg.CurrentContext)
			fmt.Fprintf(f.IO.Out, "  server:    %s\n", ctx.Server)
			if ctx.Namespace != "" {
				fmt.Fprintf(f.IO.Out, "  namespace: %s\n", ctx.Namespace)
			}
			return nil
		},
	}
}

func newCmdConfigSet(f *Factory) *cobra.Command {
	var server, namespace string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set config values for current context",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFile()
			if err != nil {
				return err
			}
			if cfg.CurrentContext == "" {
				cfg.CurrentContext = "default"
			}
			ctx := cfg.Contexts[cfg.CurrentContext]
			if server != "" {
				ctx.Server = server
			}
			if namespace != "" {
				ctx.Namespace = namespace
			}
			cfg.Contexts[cfg.CurrentContext] = ctx
			if err := saveConfigFile(cfg); err != nil {
				return err
			}
			fmt.Fprintf(f.IO.Out, "Updated context '%s'\n", cfg.CurrentContext)
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Server URL")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Default namespace")
	return cmd
}

func newCmdConfigContexts(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "contexts",
		Short: "List all contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFile()
			if err != nil {
				return err
			}
			if len(cfg.Contexts) == 0 {
				fmt.Fprintln(f.IO.Out, "No contexts configured")
				return nil
			}
			for name, ctx := range cfg.Contexts {
				current := " "
				if name == cfg.CurrentContext {
					current = "*"
				}
				fmt.Fprintf(f.IO.Out, "%s %s\t%s\n", current, name, ctx.Server)
			}
			return nil
		},
	}
}

func newCmdConfigUseContext(f *Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "use-context <name>",
		Short: "Switch to a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfigFile()
			if err != nil {
				return err
			}
			name := args[0]
			if _, ok := cfg.Contexts[name]; !ok {
				return fmt.Errorf("context '%s' not found", name)
			}
			cfg.CurrentContext = name
			if err := saveConfigFile(cfg); err != nil {
				return err
			}
			fmt.Fprintf(f.IO.Out, "Switched to context '%s'\n", name)
			return nil
		},
	}
}
