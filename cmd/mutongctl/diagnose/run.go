package diagnose

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

// RunDiagnosisOptions holds the options for the diagnose run command.
type RunDiagnosisOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

// NewCmdRunDiagnosis creates the diagnose run command.
func NewCmdRunDiagnosis(f *Factory, runF func(*RunDiagnosisOptions) error) *cobra.Command {
	opts := &RunDiagnosisOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run AI diagnosis for an alert",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return runDiagnosisRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Fingerprint, "fingerprint", "f", "", "Alert fingerprint (required)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	_ = cmd.MarkFlagRequired("fingerprint")
	return cmd
}

func runDiagnosisRun(opts *RunDiagnosisOptions) error {
	api := api.NewDiagnosisAPI(opts.Client)
	result, err := api.RunDiagnosis(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
