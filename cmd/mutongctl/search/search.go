package search

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"gitee.com/tddh/mutong/cmd/mutongctl/internal/client"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/format"
	"gitee.com/tddh/mutong/cmd/mutongctl/internal/iostreams"
)

type Factory struct {
	IO     *iostreams.IOStreams
	Client *client.Client
}

func NewCmdSearch(f *Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search external knowledge bases",
	}
	cmd.AddCommand(NewCmdGithub(f, nil))
	cmd.AddCommand(NewCmdTavily(f, nil))
	return cmd
}

type searchOptions struct {
	IO     *iostreams.IOStreams
	Client *client.Client
	Query  string
	Format string
}

func mcpToolResult(ctx context.Context, opts *searchOptions, toolName string, body map[string]string) (string, error) {
	data, err := opts.Client.Do(ctx, "POST", "/api/v1/diagnosis/mcp/tool/"+toolName, body)
	if err != nil {
		return "", err
	}
	var resp struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("%s", resp.Error)
	}
	return resp.Result, nil
}

func NewCmdGithub(f *Factory, runF func(*searchOptions) error) *cobra.Command {
	opts := &searchOptions{Format: "json"}
	var repo, state string
	cmd := &cobra.Command{
		Use:   "github",
		Short: "Search GitHub Issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			body := map[string]string{"query": opts.Query}
			if repo != "" {
				body["repo"] = repo
			}
			if state != "" {
				body["state"] = state
			}
			result, err := mcpToolResult(cmd.Context(), opts, "search_github_issues", body)
			if err != nil {
				return err
			}
			return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
		},
	}
	cmd.Flags().StringVarP(&opts.Query, "query", "q", "", "Search query (required)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	cmd.Flags().StringVarP(&repo, "repo", "r", "", "GitHub repository (e.g. kubernetes/kubernetes)")
	cmd.Flags().StringVar(&state, "state", "open", "Issue state: open/closed/all")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}

func NewCmdTavily(f *Factory, runF func(*searchOptions) error) *cobra.Command {
	opts := &searchOptions{Format: "json"}
	var topic string
	cmd := &cobra.Command{
		Use:   "tavily",
		Short: "Search Tavily knowledge base",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.IO = f.IO
			opts.Client = f.Client
			if runF != nil {
				return runF(opts)
			}
			body := map[string]string{"query": opts.Query}
			if topic != "" {
				body["topic"] = topic
			}
			result, err := mcpToolResult(cmd.Context(), opts, "search_knowledge_base", body)
			if err != nil {
				return err
			}
			return format.Print(opts.IO.Out, opts.Format, result, opts.IO.ColorEnabled())
		},
	}
	cmd.Flags().StringVarP(&opts.Query, "query", "q", "", "Search query (required)")
	cmd.Flags().StringVarP(&opts.Format, "output", "o", "json", "Output format: json/table/md/yaml")
	cmd.Flags().StringVar(&topic, "topic", "general", "Topic: general/news/security")
	_ = cmd.MarkFlagRequired("query")
	return cmd
}
