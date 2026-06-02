package system

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetSystemStatusOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Format string
}

func NewCmdGetSystemStatus(f *Factory, runF func(*GetSystemStatusOptions) error) *cobra.Command {
	opts := &GetSystemStatusOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get system status",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return getSystemStatusRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getSystemStatusRun(opts *GetSystemStatusOptions) error {
	systemAPI := api.NewSystemAPI(opts.Client)
	status, err := systemAPI.GetSystemStatus()
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, status, opts.IO.ColorEnabled())
}
