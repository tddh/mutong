package biz

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type AppsOptions struct {
	IO           *iostreams.IOStreams
	Client       *client.Client
	BusinessUnit string
	Team         string
	Namespace    string
	Format       string
}

func NewCmdApps(f *Factory, runF func(*AppsOptions) error) *cobra.Command {
	opts := &AppsOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "List business applications",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			return appsRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.BusinessUnit, "business-unit", "b", "", "Filter by business unit")
	cmd.Flags().StringVarP(&opts.Team, "team", "T", "", "Filter by team")
	cmd.Flags().StringVarP(&opts.Namespace, "namespace", "n", "", "Filter by namespace")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func appsRun(opts *AppsOptions) error {
	bapi := api.NewBusinessAPI(opts.Client)
	apps, err := bapi.ListApps(opts.BusinessUnit, opts.Team, opts.Namespace)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, apps, opts.IO.ColorEnabled())
}
