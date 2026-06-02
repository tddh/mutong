package inspect

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type RunInspectOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdRunInspect(f *Factory, runF func(*RunInspectOptions) error) *cobra.Command {
	opts := &RunInspectOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Trigger an inspection",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return runInspectRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func runInspectRun(opts *RunInspectOptions) error {
	api := api.NewInspectionAPI(opts.Client)
	result, err := api.RunInspection()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
