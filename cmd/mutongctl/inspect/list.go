package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListInspectsOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdListInspects(f *Factory, runF func(*ListInspectsOptions) error) *cobra.Command {
	opts := &ListInspectsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List inspection reports",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listInspectsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listInspectsRun(opts *ListInspectsOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	reports, err := api.ListReports()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, reports, opts.IO.ColorEnabled())
}
