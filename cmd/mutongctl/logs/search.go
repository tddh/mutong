package logs

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// SearchLogsOptions holds the options for the logs search command.
type SearchLogsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Keyword   string
	Namespace string
	Since     string
	Format    string
}

// NewCmdLogsSearch creates the logs search subcommand.
func NewCmdLogsSearch(f *Factory, runF func(*SearchLogsOptions) error) *cobra.Command {
	opts := &SearchLogsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search logs by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Keyword = args[0]
			if runF != nil {
				return runF(opts)
			}
			return searchLogsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Time range (e.g. 15m, 1h, 1d)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func searchLogsRun(opts *SearchLogsOptions) error {
	logsAPI := api.NewLogsAPI(opts.Client)
	entries, err := logsAPI.SearchLogs(opts.Keyword, opts.Namespace, opts.Since)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, entries, opts.IO.ColorEnabled())
}
