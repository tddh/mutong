package exec

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ExecutorStatusOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdExecutorStatus(f *Factory, runF func(*ExecutorStatusOptions) error) *cobra.Command {
	opts := &ExecutorStatusOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get executor status",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return executorStatusRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func executorStatusRun(opts *ExecutorStatusOptions) error {
	eapi := api.NewExecutorAPI(opts.Client)
	status, err := eapi.GetStatus()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, status, opts.IO.ColorEnabled())
}
