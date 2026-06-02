package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type InspectTrendOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Days   int
	Format string
}

func NewCmdInspectTrend(f *Factory, runF func(*InspectTrendOptions) error) *cobra.Command {
	opts := &InspectTrendOptions{Format: "json", Days: 7}
	cmd := &cobra.Command{
		Use:   "trend",
		Short: "Get inspection trend",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return inspectTrendRun(opts)
		},
	}
	cmd.Flags().IntVarP(&opts.Days, "days", "d", 7, "Number of days for trend analysis")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func inspectTrendRun(opts *InspectTrendOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	items, err := api.GetTrend(opts.Days)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, items, opts.IO.ColorEnabled())
}
