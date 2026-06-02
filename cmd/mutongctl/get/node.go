package get

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetNodeOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Name   string
	Format string
}

func NewCmdGetNode(f *Factory, runF func(*GetNodeOptions) error) *cobra.Command {
	opts := &GetNodeOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "node <name>",
		Short: "Get a Node by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Name = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getNodeRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getNodeRun(opts *GetNodeOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	node, err := resAPI.GetNode(opts.Name)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, node, opts.IO.ColorEnabled())
}
