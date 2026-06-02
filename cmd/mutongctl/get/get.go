package get

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdGet(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get K8s resources by name",
	}
	cmd.AddCommand(NewCmdGetPod(f, nil))
	cmd.AddCommand(NewCmdGetNode(f, nil))
	return cmd
}
