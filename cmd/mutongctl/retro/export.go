package retro

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type ExportOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
}

func NewCmdExport(f *Factory, runF func(*ExportOptions) error) *cobra.Command {
	opts := &ExportOptions{}
	cmd := &cobra.Command{
		Use:   "export <fingerprint>",
		Short: "Export postmortem as Markdown text",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return exportRun(opts)
		},
	}
	return cmd
}

func exportRun(opts *ExportOptions) error {
	api := api.NewRetrospectiveAPI(opts.Client)
	text, err := api.Export(opts.Fingerprint)
	if err != nil {
		return err
	}
	fmt.Fprint(opts.IO.Out, text)
	return nil
}
