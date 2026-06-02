package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type CompareInspectsOptions struct {
	IO      *iostreams.IOStreams
	Client  *client.Client
	Report1 string
	Report2 string
	Format  string
}

func NewCmdCompareInspects(f *Factory, runF func(*CompareInspectsOptions) error) *cobra.Command {
	opts := &CompareInspectsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "compare <id1> <id2>",
		Short: "Compare two inspection reports",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Report1 = args[0]
			opts.Report2 = args[1]
			if runF != nil {
				return runF(opts)
			}
			return compareInspectsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func compareInspectsRun(opts *CompareInspectsOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	result, err := api.CompareReports(opts.Report1, opts.Report2)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
