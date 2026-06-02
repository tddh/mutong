package alert

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetAlertOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

func NewCmdGetAlert(f *Factory, runF func(*GetAlertOptions) error) *cobra.Command {
	opts := &GetAlertOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "get <fingerprint>",
		Short: "Get alert detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getAlertRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getAlertRun(opts *GetAlertOptions) error {
	alertAPI := api.NewAlertAPI(opts.Client)
	alert, err := alertAPI.GetAlert(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, alert, opts.IO.ColorEnabled())
}
