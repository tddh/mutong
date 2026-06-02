package diagnose

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// Factory provides shared dependencies to diagnose commands.
type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

// NewCmdDiagnose creates the diagnose parent command.
func NewCmdDiagnose(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Manage AI diagnoses",
	}
	cmd.AddCommand(NewCmdRunDiagnosis(f, nil))
	cmd.AddCommand(NewCmdDiagnosisStatus(f, nil))
	return cmd
}
