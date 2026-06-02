package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// Factory mirrors the main Factory for dependency injection (avoids circular imports).
type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdList(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List K8s resources",
	}
	cmd.AddCommand(NewCmdListPods(f, nil))
	cmd.AddCommand(NewCmdListNodes(f, nil))
	cmd.AddCommand(NewCmdListDeployments(f, nil))
	cmd.AddCommand(NewCmdListStatefulSets(f, nil))
	cmd.AddCommand(NewCmdListDaemonSets(f, nil))
	cmd.AddCommand(NewCmdListServices(f, nil))
	return cmd
}
