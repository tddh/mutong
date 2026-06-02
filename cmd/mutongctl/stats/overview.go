package stats

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetOverviewOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdGetOverview(f *Factory, runF func(*GetOverviewOptions) error) *cobra.Command {
	opts := &GetOverviewOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "overview",
		Short: "Get stats overview",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return getOverviewRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getOverviewRun(opts *GetOverviewOptions) error {
	statsAPI := api.NewStatsAPI(opts.Client)
	overview, err := statsAPI.GetOverview()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, overview, opts.IO.ColorEnabled())
}
