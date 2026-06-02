package auth

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO *iostreams.IOStreams
}

func NewCmdAuth(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with Mutong server",
	}
	cmd.AddCommand(newCmdLogin(f))
	cmd.AddCommand(newCmdStatus(f))
	cmd.AddCommand(newCmdLogout(f))
	return cmd
}
