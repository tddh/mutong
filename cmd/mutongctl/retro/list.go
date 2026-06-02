package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListOptions struct {
	IO       *iostreams.IOStreams
	Client   *client.Client
	Severity string
	Page     int
	Format   string
}

func NewCmdList(f *Factory, runF func(*ListOptions) error) *cobra.Command {
	opts := &ListOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List retrospective postmortems",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Severity, "severity", "S", "", "Filter by severity")
	cmd.Flags().IntVarP(&opts.Page, "page", "p", 0, "Page number")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listRun(opts *ListOptions) error {
	api := api.NewRetrospectiveAPI(opts.Client)
	result, err := api.List(opts.Severity, opts.Page)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
