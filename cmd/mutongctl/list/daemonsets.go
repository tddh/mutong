package list

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ListDaemonSetsOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Namespace string
	Format    string
}

func NewCmdListDaemonSets(f *Factory, runF func(*ListDaemonSetsOptions) error) *cobra.Command {
	opts := &ListDaemonSetsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "daemonsets",
		Short: "List DaemonSets",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return listDaemonSetsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func listDaemonSetsRun(opts *ListDaemonSetsOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	ds, err := resAPI.ListDaemonSets(opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, ds, opts.IO.ColorEnabled())
}
