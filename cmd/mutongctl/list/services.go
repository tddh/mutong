package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListServicesOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Format    string
}

func NewCmdListServices(f *Factory, runF func(*ListServicesOptions) error) *cobra.Command {
	opts := &ListServicesOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "services",
		Short: "List Services",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listServicesRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listServicesRun(opts *ListServicesOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	svcs, err := resAPI.ListServices(opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, svcs, opts.IO.ColorEnabled())
}
