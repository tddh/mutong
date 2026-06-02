package get

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetPodOptions struct {
	IO        *iostreams.IOStreams
	Client    *client.Client
	Name      string
	Namespace string
	Format    string
}

func NewCmdGetPod(f *Factory, runF func(*GetPodOptions) error) *cobra.Command {
	opts := &GetPodOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "pod <name>",
		Short: "Get a Pod by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Name = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getPodRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "default", "K8s namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getPodRun(opts *GetPodOptions) error {
	resAPI := api.NewResourcesAPI(opts.Client)
	pod, err := resAPI.GetPod(opts.Name, opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, pod, opts.IO.ColorEnabled())
}
