package metrics

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ResourceOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Kind      string
	Name      string
	Namespace string
	Format    string
}

func NewCmdResource(f *Factory, runF func(*ResourceOptions) error) *cobra.Command {
	opts := &ResourceOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "resource <kind>",
		Short: "Get resource metrics (kind: pod/node/deployment/statefulset/daemonset/service)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Kind = args[0]
			if runF != nil {
				return runF(opts)
			}
			return resourceRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Name, "name", "", "", "Resource name")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func resourceRun(opts *ResourceOptions) error {
	metricsAPI := api.NewMetricsAPI(opts.Client)
	results, err := metricsAPI.GetResourceMetrics(opts.Kind, opts.Name, opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, results, opts.IO.ColorEnabled())
}
