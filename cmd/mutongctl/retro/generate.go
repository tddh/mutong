package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type GenerateOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

func NewCmdGenerate(f *Factory, runF func(*GenerateOptions) error) *cobra.Command {
	opts := &GenerateOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "generate <fingerprint>",
		Short: "Generate a postmortem report",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return generateRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func generateRun(opts *GenerateOptions) error {
	api := api.NewRetrospectiveAPI(opts.Client)
	pm, err := api.Generate(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, pm, opts.IO.ColorEnabled())
}
