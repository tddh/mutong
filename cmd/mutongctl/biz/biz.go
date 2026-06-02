package biz

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdBiz(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "biz",
		Short: "Manage business topology",
	}
	cmd.AddCommand(NewCmdApps(f, nil))
	cmd.AddCommand(NewCmdGraph(f, nil))
	return cmd
}
