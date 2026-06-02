package logs

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// ErrorLogsOptions holds the options for the logs errors command.
type ErrorLogsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Since     string
	Format    string
}

// NewCmdLogsErrors creates the logs errors subcommand.
func NewCmdLogsErrors(f *Factory, runF func(*ErrorLogsOptions) error) *cobra.Command {
	opts := &ErrorLogsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "errors",
		Short: "Get error-level logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return errorLogsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Time range (e.g. 15m, 1h, 1d)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func errorLogsRun(opts *ErrorLogsOptions) error {
	logsAPI := api.NewLogsAPI(opts.Client)
	entries, err := logsAPI.GetErrorLogs(opts.Namespace, opts.Since)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, entries, opts.IO.ColorEnabled())
}
