package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetInspectOptions struct {
	IO       *iostreams.IOStreams
	Client   *client.Client
	ReportID string
	Format   string
}

func NewCmdGetInspect(f *Factory, runF func(*GetInspectOptions) error) *cobra.Command {
	opts := &GetInspectOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get inspection report by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.ReportID = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getInspectRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getInspectRun(opts *GetInspectOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	report, err := api.GetReport(opts.ReportID)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, report, opts.IO.ColorEnabled())
}
