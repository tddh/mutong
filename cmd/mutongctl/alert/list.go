package alert

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListAlertsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Severity  string
	Namespace string
	Format    string
}

func NewCmdListAlerts(f *Factory, runF func(*ListAlertsOptions) error) *cobra.Command {
	opts := &ListAlertsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active alerts",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listAlertsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Severity, "severity", "S", "", "Alert severity (warning/critical)")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listAlertsRun(opts *ListAlertsOptions) error {
	alertAPI := api.NewAlertAPI(opts.Client)
	alerts, err := alertAPI.ListAlerts(opts.Severity, opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, alerts, opts.IO.ColorEnabled())
}
