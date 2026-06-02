package biz

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GraphOptions struct {
	IO           *iostreams.IOStreams
	Client       *client.Client
	BusinessUnit string
	Team         string
	Format       string
}

func NewCmdGraph(f *Factory, runF func(*GraphOptions) error) *cobra.Command {
	opts := &GraphOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Get business topology graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return graphRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.BusinessUnit, "business-unit", "b", "", "Filter by business unit")
	cmd.Flags().StringVarP(&opts.Team, "team", "T", "", "Filter by team")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func graphRun(opts *GraphOptions) error {
	bapi := api.NewBusinessAPI(opts.Client)
	graph, err := bapi.GetGraph(opts.BusinessUnit, opts.Team)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, graph, opts.IO.ColorEnabled())
}
