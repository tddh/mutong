package exec

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ExecuteOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Confirm   *bool
	Action    string
	Target    string
	Namespace string
	Reason    string
	Risk      string
	Replicas  int32
	Format    string
}

func NewCmdExecute(f *Factory, confirm *bool, runF func(*ExecuteOptions) error) *cobra.Command {
	opts := &ExecuteOptions{Format: "json", Confirm: confirm}
	cmd := &cobra.Command{
		Use:   "execute",
		Short: "Execute a remediation action (requires --confirm)",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if !*opts.Confirm {
				return fmt.Errorf("[ERROR] destructive operation requires --confirm flag (exit_code=2)\n[HINT] add --confirm to proceed")
			}
			if runF != nil {
				return runF(opts)
			}
			return executeRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Action, "action", "a", "", "Action type: restart_pod, scale_deployment, create_hpa, update_hpa")
	cmd.Flags().StringVar(&opts.Target, "target", "", "Target resource (namespace/name)")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Reason, "reason", "r", "", "Reason for execution")
	cmd.Flags().StringVar(&opts.Risk, "risk", "low", "Risk level: low/medium/high")
	cmd.Flags().Int32Var(&opts.Replicas, "replicas", 0, "Desired replicas for scale_deployment / hpa")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	_ = cmd.MarkFlagRequired("action")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func executeRun(opts *ExecuteOptions) error {
	plan := api.ExecutionPlan{
		Action:    opts.Action,
		Target:    opts.Target,
		Namespace: opts.Namespace,
		Reason:    opts.Reason,
		Risk:      opts.Risk,
		Replicas:  opts.Replicas,
	}
	eapi := api.NewExecutorAPI(opts.Client)
	result, err := eapi.Execute(plan)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
