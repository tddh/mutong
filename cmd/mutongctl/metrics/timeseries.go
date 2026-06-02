package metrics

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type TimeseriesOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Expr   string
	Start  string
	End    string
	Step   string
	Format string
}

func NewCmdTimeseries(f *Factory, runF func(*TimeseriesOptions) error) *cobra.Command {
	opts := &TimeseriesOptions{Format: "json", Step: "60"}
	cmd := &cobra.Command{
		Use:   "timeseries",
		Short: "Query metric timeseries",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return timeseriesRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Expr, "expr", "e", "", "PromQL expression")
	cmd.Flags().StringVar(&opts.Start, "start", "", "Start time (Unix timestamp)")
	cmd.Flags().StringVar(&opts.End, "end", "", "End time (Unix timestamp)")
	cmd.Flags().StringVar(&opts.Step, "step", "60", "Step interval in seconds")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func timeseriesRun(opts *TimeseriesOptions) error {
	metricsAPI := api.NewMetricsAPI(opts.Client)
	results, err := metricsAPI.GetTimeseries(opts.Expr, opts.Start, opts.End, opts.Step)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, results, opts.IO.ColorEnabled())
}
