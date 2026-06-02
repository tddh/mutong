package exec

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdExec(f *Factory, confirm *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Manage K8s remediation executor",
	}
	cmd.AddCommand(NewCmdExecute(f, confirm, nil))
	cmd.AddCommand(NewCmdAudit(f, nil))
	cmd.AddCommand(NewCmdExecutorStatus(f, nil))
	return cmd
}
