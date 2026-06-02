package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListStatefulSetsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Format    string
}

func NewCmdListStatefulSets(f *Factory, runF func(*ListStatefulSetsOptions) error) *cobra.Command {
	opts := &ListStatefulSetsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "statefulsets",
		Short: "List StatefulSets",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listStatefulSetsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listStatefulSetsRun(opts *ListStatefulSetsOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	sts, err := resAPI.ListStatefulSets(opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, sts, opts.IO.ColorEnabled())
}
