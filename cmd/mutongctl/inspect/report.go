package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type InspectReportOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdInspectReport(f *Factory, runF func(*InspectReportOptions) error) *cobra.Command {
	opts := &InspectReportOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Get latest inspection report",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return inspectReportRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func inspectReportRun(opts *InspectReportOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	report, err := api.GetLatestReport()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, report, opts.IO.ColorEnabled())
}
