package cluster

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdCluster(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Manage clusters",
	}
	cmd.AddCommand(NewCmdListClusters(f, nil))
	cmd.AddCommand(NewCmdGetClusterHealth(f, nil))
	cmd.AddCommand(NewCmdGetClusterStats(f, nil))
	return cmd
}
