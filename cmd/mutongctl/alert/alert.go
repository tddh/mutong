package alert

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdAlert(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Manage alerts",
	}
	cmd.AddCommand(NewCmdListAlerts(f, nil))
	cmd.AddCommand(NewCmdGetAlert(f, nil))
	cmd.AddCommand(NewCmdSuppression(f, nil))
	return cmd
}
