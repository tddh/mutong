package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdInspect(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Manage inspections",
	}
	cmd.AddCommand(NewCmdRunInspect(f, nil))
	cmd.AddCommand(NewCmdInspectReport(f, nil))
	cmd.AddCommand(NewCmdListInspects(f, nil))
	cmd.AddCommand(NewCmdGetInspect(f, nil))
	cmd.AddCommand(NewCmdCompareInspects(f, nil))
	cmd.AddCommand(NewCmdInspectTrend(f, nil))
	return cmd
}
