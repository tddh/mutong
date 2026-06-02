package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GetOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

func NewCmdGet(f *Factory, runF func(*GetOptions) error) *cobra.Command {
	opts := &GetOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "get <fingerprint>",
		Short: "Get retrospective postmortem detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return getRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func getRun(opts *GetOptions) error {
	api := api.NewRetrospectiveAPI(opts.Client)
	pm, err := api.Get(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, pm, opts.IO.ColorEnabled())
}
