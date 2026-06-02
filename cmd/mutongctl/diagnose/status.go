package diagnose

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// DiagnosisStatusOptions holds the options for the diagnose status command.
type DiagnosisStatusOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

// NewCmdDiagnosisStatus creates the diagnose status command.
func NewCmdDiagnosisStatus(f *Factory, runF func(*DiagnosisStatusOptions) error) *cobra.Command {
	opts := &DiagnosisStatusOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show diagnosis service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return diagnosisStatusRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func diagnosisStatusRun(opts *DiagnosisStatusOptions) error {
	api := api.NewDiagnosisAPI(opts.Client)
	status, err := api.GetStatus()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, status, opts.IO.ColorEnabled())
}
