package cluster

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListClustersOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdListClusters(f *Factory, runF func(*ListClustersOptions) error) *cobra.Command {
	opts := &ListClustersOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listClustersRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listClustersRun(opts *ListClustersOptions) error {
	clusterAPI := api.NewClusterAPI(opts.Client)
	clusters, err := clusterAPI.ListClusters()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, clusters, opts.IO.ColorEnabled())
}
