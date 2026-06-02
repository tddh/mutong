package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/alert"
	"gitee.com/tddh/mutong/cmd/mutongctl/auth"
	"gitee.com/tddh/mutong/cmd/mutongctl/biz"
	"gitee.com/tddh/mutong/cmd/mutongctl/cluster"
	"gitee.com/tddh/mutong/cmd/mutongctl/completion"
	cfgcmd "gitee.com/tddh/mutong/cmd/mutongctl/config"
	"gitee.com/tddh/mutong/cmd/mutongctl/debug"
	"gitee.com/tddh/mutong/cmd/mutongctl/diagnose"
	"gitee.com/tddh/mutong/cmd/mutongctl/exec"
	"gitee.com/tddh/mutong/cmd/mutongctl/get"
	"gitee.com/tddh/mutong/cmd/mutongctl/inspect"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/config"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
	"gitee.com/tddh/mutong/cmd/mutongctl/list"
	"gitee.com/tddh/mutong/cmd/mutongctl/logs"
	"gitee.com/tddh/mutong/cmd/mutongctl/metrics"
	"gitee.com/tddh/mutong/cmd/mutongctl/retro"
	"gitee.com/tddh/mutong/cmd/mutongctl/stats"
	"gitee.com/tddh/mutong/cmd/mutongctl/system"
	"gitee.com/tddh/mutong/cmd/mutongctl/terminal"
	"gitee.com/tddh/mutong/cmd/mutongctl/topo"
)

// Factory provides shared dependencies to all commands.
type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Config *config.Config
}

// NewFactory creates a production Factory.
func NewFactory() *Factory {
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return &Factory{
		IO:     iostreams.System(),
		Client: client.NewClientWithOAuth(cfg.Server, os.Getenv("MUTONG_TOKEN")),
		Config: cfg,
	}
}

// NewTestFactory creates a test Factory (mock HTTP server URL).
func NewTestFactory(mockServerURL string) *Factory {
	ios, _, _ := iostreams.Test()
	return &Factory{
		IO:     ios,
		Client: client.NewClient(mockServerURL, "test-token", "test"),
	}
}

// detectAuthSource returns the origin of the auth token.
func detectAuthSource() string {
	if os.Getenv("MUTONG_TOKEN") != "" {
		return "env:MUTONG_TOKEN"
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(home + "/.mutong/tokens.json"); err == nil {
		return "oauth:~/.mutong/tokens.json"
	}
	return "flag:--token"
}

var (
	serverFlag   string
	tokenFlag    string
	outputFlag   string
	noColorFlag  bool
	verboseFlag  int
	confirmFlag  bool
	adminKeyFlag string
)

var rootCmd = &cobra.Command{
	Use:     "mutongctl",
	Short:   "Mutong AIOps CLI",
	Long:    `mutongctl is the command-line client for the Mutong AIOps platform.`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Flags override config
		authSource := detectAuthSource()
		if serverFlag != "" {
			factory.Client = client.NewClient(serverFlag, tokenFlag, "flag:--server")
			authSource = "flag:--server"
		}
		if tokenFlag != "" {
			server := coalesce(serverFlag, factory.Config.Server)
			factory.Client = client.NewClient(server, tokenFlag, "flag:--token")
			authSource = "flag:--token"
		}
		if adminKeyFlag != "" {
			factory.Client.SetAdminKey(adminKeyFlag)
		} else if factory.Config.AdminKey != "" {
			factory.Client.SetAdminKey(factory.Config.AdminKey)
		}
		// Verbose output to stderr
		if verboseFlag > 0 {
			fmt.Fprintf(factory.IO.ErrOut, "[VERBOSE] server=%s auth=%s\n", factory.Client.BaseURL(), authSource)
		}
		if verboseFlag > 1 {
			factory.Client.SetVerbose(true)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var factory *Factory

func init() {
	factory = NewFactory()

	rootCmd.AddCommand(list.NewCmdList(&list.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(auth.NewCmdAuth(&auth.Factory{
		IO: factory.IO,
	}))
	rootCmd.AddCommand(alert.NewCmdAlert(&alert.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(inspect.NewCmdInspect(&inspect.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(diagnose.NewCmdDiagnose(&diagnose.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(logs.NewCmdLogs(&logs.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(metrics.NewCmdMetrics(&metrics.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(cluster.NewCmdCluster(&cluster.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(system.NewCmdSystem(&system.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(stats.NewCmdStats(&stats.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(topo.NewCmdTopo(&topo.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(retro.NewCmdRetro(&retro.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(get.NewCmdGet(&get.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(exec.NewCmdExec(&exec.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}, &confirmFlag))
	rootCmd.AddCommand(biz.NewCmdBiz(&biz.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(cfgcmd.NewCmdConfig(&cfgcmd.Factory{
		IO: factory.IO,
	}))
	rootCmd.AddCommand(terminal.NewCmdTerminal(&terminal.Factory{
		IO:     factory.IO,
		Client: factory.Client,
	}))
	rootCmd.AddCommand(completion.NewCmdCompletion(rootCmd))
	rootCmd.AddCommand(debug.NewCmdDebug(&debug.Factory{
		IO:     factory.IO,
		Client: factory.Client,
		Config: factory.Config,
	}, version))

	rootCmd.PersistentFlags().StringVarP(&serverFlag, "server", "s", "", "Mutong server URL (default http://localhost:8888)")
	rootCmd.PersistentFlags().StringVarP(&tokenFlag, "token", "t", "", "Auth token")
	rootCmd.PersistentFlags().StringVarP(&outputFlag, "output", "o", "", "Output format: json/table/md/yaml (default TTY=table, pipe=json)")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false, "Disable color output")
	rootCmd.PersistentFlags().CountVarP(&verboseFlag, "verbose", "v", "Verbose: -v env info, -vv request details (stderr)")
	rootCmd.PersistentFlags().BoolVar(&confirmFlag, "confirm", false, "Confirm destructive operations")
	rootCmd.PersistentFlags().StringVar(&adminKeyFlag, "admin-key", "", "Admin key for privileged operations (executor)")
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
