package stats

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type SyncStatsOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdSyncStats(f *Factory, runF func(*SyncStatsOptions) error) *cobra.Command {
	opts := &SyncStatsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return syncStatsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func syncStatsRun(opts *SyncStatsOptions) error {
	statsAPI := api.NewStatsAPI(opts.Client)
	result, err := statsAPI.SyncStats()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
