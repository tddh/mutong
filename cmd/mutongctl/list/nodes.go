package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListNodesOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdListNodes(f *Factory, runF func(*ListNodesOptions) error) *cobra.Command {
	opts := &ListNodesOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List Nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listNodesRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listNodesRun(opts *ListNodesOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	nodes, err := resAPI.ListNodes()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, nodes, opts.IO.ColorEnabled())
}
