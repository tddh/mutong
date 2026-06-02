package logs

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// PodLogsOptions holds the options for the logs pod command.
type PodLogsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Pod       string
	Namespace string
	Tail      int
	Format    string
}

// NewCmdLogsPod creates the logs pod subcommand.
func NewCmdLogsPod(f *Factory, runF func(*PodLogsOptions) error) *cobra.Command {
	opts := &PodLogsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "pod <name>",
		Short: "Get pod logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Pod = args[0]
			if runF != nil {
				return runF(opts)
			}
			return podLogsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().IntVar(&opts.Tail, "tail", 0, "Number of recent log lines")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func podLogsRun(opts *PodLogsOptions) error {
	logsAPI := api.NewLogsAPI(opts.Client)
	entries, err := logsAPI.GetPodLogs(opts.Pod, opts.Namespace, opts.Tail)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, entries, opts.IO.ColorEnabled())
}
