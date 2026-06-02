package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListDeploymentsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Format    string
}

func NewCmdListDeployments(f *Factory, runF func(*ListDeploymentsOptions) error) *cobra.Command {
	opts := &ListDeploymentsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "List Deployments",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listDeploymentsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listDeploymentsRun(opts *ListDeploymentsOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	deps, err := resAPI.ListDeployments(opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, deps, opts.IO.ColorEnabled())
}
