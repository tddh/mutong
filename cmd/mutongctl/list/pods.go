package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListPodsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Format    string
}

func NewCmdListPods(f *Factory, runF func(*ListPodsOptions) error) *cobra.Command {
	opts := &ListPodsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "pods",
		Short: "List Pods",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listPodsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listPodsRun(opts *ListPodsOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	pods, err := resAPI.ListPods(opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, pods, opts.IO.ColorEnabled())
}
