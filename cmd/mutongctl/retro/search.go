package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type SearchOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Query  string
	Format string
}

func NewCmdSearch(f *Factory, runF func(*SearchOptions) error) *cobra.Command {
	opts := &SearchOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search retrospective knowledge base",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Query = args[0]
			if runF != nil {
				return runF(opts)
			}
			return searchRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func searchRun(opts *SearchOptions) error {
	api := api.NewRetrospectiveAPI(opts.Client)
	result, err := api.Search(opts.Query)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
}
