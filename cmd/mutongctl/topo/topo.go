package topo

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdTopo(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topo",
		Short: "Query resource topology",
	}
	cmd.AddCommand(NewCmdTopoGet(f, nil))
	cmd.AddCommand(NewCmdTopoSearch(f, nil))
	return cmd
}
