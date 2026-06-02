package topo

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type TopoSearchOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Kind   string
	Name   string
	Format string
}

func NewCmdTopoSearch(f *Factory, runF func(*TopoSearchOptions) error) *cobra.Command {
	opts := &TopoSearchOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search topology by kind and name",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return topoSearchRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Kind, "kind", "k", "", "Resource kind (e.g. Pod, Deployment)")
	cmd.Flags().StringVarP(&opts.Name, "name", "n", "", "Resource name")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func topoSearchRun(opts *TopoSearchOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	graph, err := resAPI.TopoSearch(opts.Kind, opts.Name)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, graph, opts.IO.ColorEnabled())
}
