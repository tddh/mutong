package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdRetro(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retro",
		Short: "Manage retrospective postmortems",
	}
	cmd.AddCommand(NewCmdGenerate(f, nil))
	cmd.AddCommand(NewCmdList(f, nil))
	cmd.AddCommand(NewCmdGet(f, nil))
	cmd.AddCommand(NewCmdExport(f, nil))
	cmd.AddCommand(NewCmdSearch(f, nil))
	cmd.AddCommand(NewCmdTimeline(f, nil))
	cmd.AddCommand(NewCmdCausalChain(f, nil))
	return cmd
}
