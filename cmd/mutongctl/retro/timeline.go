package retro

import (
	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/api"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type TimelineOptions struct {
	IO          *iostreams.IOStreams
	Client      *client.Client
	Fingerprint string
	Format      string
}

func NewCmdTimeline(f *Factory, runF func(*TimelineOptions) error) *cobra.Command {
	opts := &TimelineOptions{Format: "json"}
	cmd := &cobra.Command{
		Use:   "timeline <fingerprint>",
		Short: "Get incident timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			opts.Fingerprint = args[0]
			if runF != nil {
				return runF(opts)
			}
			return timelineRun(opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	return cmd
}

func timelineRun(opts *TimelineOptions) error {
	a := api.NewRetrospectiveAPI(opts.Client)
	events, err := a.GetTimeline(opts.Fingerprint)
	if err != nil {
		return err
	}
	return format.Print(opts.IO.Out, opts.Format, events, opts.IO.ColorEnabled())
}
