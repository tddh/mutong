package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"gitee.com/tddh/mutong/config"
)

type GitHubClient struct {
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

func NewGitHubClient(cfg config.GitHubConfig) *GitHubClient {
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
		token:      cfg.Token,
		endpoint:   cfg.Endpoint,
		maxResults: cfg.MaxResults,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second},
	}
}

func (c *GitHubClient) SearchIssues(ctx context.Context, query, repo, state string) ([]IssueResult, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GitHub token not configured")
	}

	searchQuery := query
	if repo != "" {
		searchQuery = fmt.Sprintf("repo:%s %s", repo, query)
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
		return nil, fmt.Errorf("create GitHub request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, resp.Status)
	}

	var gResp githubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
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
