package topo

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type TopoGetOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	UID    string
	Depth  int
	Format string
}

func NewCmdTopoGet(f *Factory, runF func(*TopoGetOptions) error) *cobra.Command {
	opts := &TopoGetOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "get <uid>",
		Short: "Get topology around a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.UID = args[0]
			if runF != nil {
				return runF(opts)
			}
			return topoGetRun(opts)
		},
	}
	cmd.Flags().IntVarP(&opts.Depth, "depth", "d", 2, "Traversal depth (1-10)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func topoGetRun(opts *TopoGetOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	graph, err := resAPI.TopoGet(opts.UID, opts.Depth)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, graph, opts.IO.ColorEnabled())
}
