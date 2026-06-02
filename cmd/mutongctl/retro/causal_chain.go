package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type CausalChainOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

func NewCmdCausalChain(f *Factory, runF func(*CausalChainOptions) error) *cobra.Command {
	opts := &CausalChainOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "causal-chain <fingerprint>",
		Short: "Get causal chain analysis",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return causalChainRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func causalChainRun(opts *CausalChainOptions) error {
	a := api.NewRetrospectiveAPI(opts.Client)
	chain, err := a.GetCausalChain(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, chain, opts.IO.ColorEnabled())
}
