package cluster

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetClusterHealthOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Name   string
	Format string
}

func NewCmdGetClusterHealth(f *Factory, runF func(*GetClusterHealthOptions) error) *cobra.Command {
	opts := &GetClusterHealthOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "health <name>",
		Short: "Get cluster health",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Name = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getClusterHealthRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getClusterHealthRun(opts *GetClusterHealthOptions) error {
	clusterAPI := api.NewClusterAPI(opts.Client)
	health, err := clusterAPI.GetClusterHealth(opts.Name)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, health, opts.IO.ColorEnabled())
}
