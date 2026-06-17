package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/config"
	"gitee.com/tddh/mutong/interfaces"
)

type TavilyClient struct {
	logger     interfaces.Logger
	apiKey     string
	endpoint   string
	maxResults int
	httpClient *http.Client
}

type tavilyRequest struct {
	APIKey      string `json:"api_key"`
	Query       string `json:"query"`
	MaxResults  int    `json:"max_results"`
	SearchDepth string `json:"search_depth"`
	IncludeRaw  bool   `json:"include_raw_content"`
	IncludeAns  bool   `json:"include_answer"`
	Topic       string `json:"topic,omitempty"`
}

type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
	Answer  string         `json:"answer"`
	Query   string         `json:"query"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

func NewTavilyClient(logger interfaces.Logger, cfg config.TavilyConfig) *TavilyClient {
	if cfg.MaxResults == 0 {
		cfg.MaxResults = 3
	}
	if cfg.TimeoutSec == 0 {
		cfg.TimeoutSec = 15
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.tavily.com/search"
	}
	return &TavilyClient{
		logger:     logger,
		apiKey:     cfg.APIKey,
		endpoint:   cfg.Endpoint,
		maxResults: cfg.MaxResults,
		httpClient: &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second},
	}
}

func (c *TavilyClient) Search(ctx context.Context, query string, topic string) ([]Result, string, error) {
	if c.apiKey == "" {
		c.logger.Info("Tavily search skipped: API key not configured",
			zap.String("query", query),
			zap.String("endpoint", c.endpoint),
		)
		return nil, "", fmt.Errorf("tavily API key not configured")
	}

	reqBody := tavilyRequest{
		APIKey:      c.apiKey,
		Query:       query,
		MaxResults:  c.maxResults,
		SearchDepth: "advanced",
		IncludeRaw:  false,
		IncludeAns:  true,
		Topic:       topic,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		c.logger.Info("Failed to marshal Tavily request",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.String("topic", topic),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("marshal tavily request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		c.logger.Info("Failed to create Tavily request",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.String("topic", topic),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("create tavily request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Info("Tavily API request failed",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.String("topic", topic),
			zap.Int("max_results", c.maxResults),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("tavily request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		c.logger.Info("Failed to read Tavily response",
			zap.String("endpoint", c.endpoint),
			zap.Int("http_status", resp.StatusCode),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("read tavily response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Info("Tavily API error response",
			zap.String("endpoint", c.endpoint),
			zap.String("query", query),
			zap.Int("status", resp.StatusCode),
			zap.Int("response_bytes", len(data)),
		)
		return nil, "", fmt.Errorf("tavily API error %d: %s", resp.StatusCode, string(data))
	}

	var tResp tavilyResponse
	if err := json.Unmarshal(data, &tResp); err != nil {
		c.logger.Info("Failed to unmarshal Tavily response",
			zap.String("endpoint", c.endpoint),
			zap.Int("response_bytes", len(data)),
			zap.Error(err),
		)
		return nil, "", fmt.Errorf("unmarshal tavily response: %w", err)
	}

	results := make([]Result, 0, len(tResp.Results))
	for _, r := range tResp.Results {
		results = append(results, Result{
			Title:   r.Title,
			Content: r.Content,
			URL:     r.URL,
			Score:   r.Score,
		})
	}

	c.logger.Info(
		"Tavily search completed",
		zap.String("query", query),
		zap.String("topic", topic),
		zap.Int("results", len(results)),
	)

	return results, tResp.Answer, nil
}

type Result struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	URL     string  `json:"url"`
	Score   float64 `json:"score"`
}
