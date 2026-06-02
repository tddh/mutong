package alert

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type SuppressionOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdSuppression(f *Factory, runF func(*SuppressionOptions) error) *cobra.Command {
	opts := &SuppressionOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "suppression",
		Short: "Get alert suppression status",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return suppressionRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func suppressionRun(opts *SuppressionOptions) error {
	a := api.NewAlertAPI(opts.Client)
	status, err := a.GetSuppressionStatus()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, status, opts.IO.ColorEnabled())
}
