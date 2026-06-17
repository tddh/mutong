package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/services/httpclient"
)

type GitHubClient struct {
	logger     interfaces.Logger
	token      string
	endpoint   string
	maxResults int
	httpClient *http.Client
}

type githubIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type githubSearchResponse struct {
	TotalCount int           `json:"total_count"`
	Items      []githubIssue `json:"items"`
}

func NewGitHubClient(logger interfaces.Logger, cfg config.GitHubConfig) *GitHubClient {
	if cfg.MaxResults == 0 {
		cfg.MaxResults = 3
	}
	if cfg.TimeoutSec == 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.github.com"
	}
	return &GitHubClient{
		logger:     logger,
		token:      cfg.Token,
		endpoint:   cfg.Endpoint,
		maxResults: cfg.MaxResults,
		httpClient: httpclient.New(time.Duration(cfg.TimeoutSec) * time.Second),
	}
}

func (c *GitHubClient) SearchIssues(ctx context.Context, query, repo, state string) ([]IssueResult, error) {
	// Token optional: unauthenticated requests allowed (60 req/hr)
	hasToken := c.token != ""

	// is:issue is required for Fine-grained PATs (GitHub returns 422 without it)
	searchQuery := fmt.Sprintf("%s is:issue", query)
	if repo != "" {
		searchQuery = fmt.Sprintf("repo:%s %s", repo, searchQuery)
	}
	if state != "" {
		searchQuery = fmt.Sprintf("%s state:%s", searchQuery, state)
	}

	params := url.Values{}
	params.Set("q", searchQuery)
	params.Set("per_page", fmt.Sprintf("%d", c.maxResults))
	params.Set("sort", "updated")
	params.Set("order", "desc")

	apiURL := fmt.Sprintf("%s/search/issues?%s", c.endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		c.logger.Info(
			"Failed to create GitHub request",
			zap.String("query", query),
			zap.String("repo", repo),
			zap.String("state", state),
			zap.String("api_url", apiURL),
			zap.Error(err),
		)
		return nil, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Mutong-AIOps/1.0")
	if hasToken {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Info(
			"GitHub API request failed",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.String("repo", repo),
			zap.String("state", state),
			zap.Bool("has_token", hasToken),
			zap.Error(err),
		)
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.logger.Info(
			"GitHub API error response",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.String("repo", repo),
			zap.Int("status", resp.StatusCode),
			zap.String("status_text", resp.Status),
			zap.String("body", string(body)),
			zap.Bool("has_token", hasToken),
		)
		return nil, fmt.Errorf("GitHub API error %d: %s - %s", resp.StatusCode, resp.Status, string(body))
	}

	var gResp githubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
		c.logger.Info(
			"Failed to decode GitHub response",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.Int("http_status", resp.StatusCode),
			zap.Error(err),
		)
		return nil, fmt.Errorf("decode GitHub response: %w", err)
	}

	results := make([]IssueResult, 0, len(gResp.Items))
	for _, item := range gResp.Items {
		var labels []string
		for _, l := range item.Labels {
			if l.Name != "" {
				labels = append(labels, l.Name)
			}
		}
		results = append(results, IssueResult{
			Number:  item.Number,
			Title:   item.Title,
			State:   item.State,
			URL:     item.HTMLURL,
			Created: item.CreatedAt.Format("2006-01-02"),
			Labels:  labels,
		})
	}

	c.logger.Info(
		"GitHub Issues search completed",
		zap.String("query", query),
		zap.String("repo", repo),
		zap.Int("results", len(results)),
		zap.Int("total_count", gResp.TotalCount),
		zap.Bool("has_token", hasToken),
	)

	return results, nil
}

type IssueResult struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	State   string   `json:"state"`
	URL     string   `json:"url"`
	Created string   `json:"created"`
	Labels  []string `json:"labels,omitempty"`
}
