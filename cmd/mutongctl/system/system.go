package system

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdSystem(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "system",
		Short: "Manage system",
	}
	cmd.AddCommand(NewCmdGetSystemStatus(f, nil))
	return cmd
}
