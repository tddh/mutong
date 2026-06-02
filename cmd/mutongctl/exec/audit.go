package exec

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type AuditOptions struct {
	IO           *iostreams.IOStreams
	Client       *client.Client
	Action       string
	Namespace    string
	AutoExecuted string
	Format       string
}

func NewCmdAudit(f *Factory, runF func(*AuditOptions) error) *cobra.Command {
	opts := &AuditOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Get executor audit logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return auditRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Action, "action", "a", "", "Filter by action type")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().StringVar(&opts.AutoExecuted, "auto-executed", "", "Filter auto-executed: true/false")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func auditRun(opts *AuditOptions) error {
	eapi := api.NewExecutorAPI(opts.Client)
	logs, err := eapi.GetAuditLogs(opts.Action, opts.Namespace, opts.AutoExecuted)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, logs, opts.IO.ColorEnabled())
}
