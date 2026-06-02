package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"gitee.com/tddh/mutong/interfaces"
)

type OTelQueryService struct {
	collectorURL string
	client       *http.Client
}

func NewOTelQueryService(collectorURL string) *OTelQueryService {
	return &OTelQueryService{
		collectorURL: collectorURL,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *OTelQueryService) QueryTraces(serviceName, operationName, traceId string, limit int) ([]interfaces.Trace, error) {
	if s.collectorURL == "" {
		return []interfaces.Trace{}, nil
	}

	params := url.Values{}
	if serviceName != "" {
		params.Set("query", fmt.Sprintf(`{"resourceFilters": [{"serviceName": "%s"}]}`, serviceName))
	}
	if traceId != "" {
		params.Set("traceId", traceId)
	}
	params.Set("limit", fmt.Sprintf("%d", limit))

	queryURL := fmt.Sprintf("%s/api/traces?%s", s.collectorURL, params.Encode())

	resp, err := s.client.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query traces: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Data []struct {
			TraceID string `json:"traceID"`
			Spans   []struct {
				OperationName string `json:"operationName"`
				StartTime     int64  `json:"startTime"`
				Duration      int64  `json:"duration"`
				Tags          []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"tags"`
			} `json:"spans"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var traces []interfaces.Trace
	for _, t := range result.Data {
		var startTime time.Time
		var duration int64
		if len(t.Spans) > 0 {
			startTime = time.Unix(0, t.Spans[0].StartTime*int64(time.Microsecond))
			duration = t.Spans[0].Duration / 1000
		}
		traces = append(traces, interfaces.Trace{
			TraceID:   t.TraceID,
			Duration:  duration,
			StartTime: startTime,
		})
	}

	return traces, nil
}

func (s *OTelQueryService) GetSpans(traceId string) ([]interfaces.Span, error) {
	if s.collectorURL == "" || traceId == "" {
		return []interfaces.Span{}, nil
	}

	queryURL := fmt.Sprintf("%s/api/traces/%s", s.collectorURL, traceId)

	resp, err := s.client.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query spans: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Data []struct {
			TraceID string `json:"traceID"`
			Spans   []struct {
				SpanID        string `json:"spanID"`
				OperationName string `json:"operationName"`
				StartTime     int64  `json:"startTime"`
				Duration      int64  `json:"duration"`
				References    []struct {
					TraceID string `json:"traceID"`
					SpanID  string `json:"spanID"`
				} `json:"references"`
				Tags []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"tags"`
			} `json:"spans"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var spans []interfaces.Span
	for _, t := range result.Data {
		for _, sp := range t.Spans {
			parentSpanID := ""
			if len(sp.References) > 0 {
				parentSpanID = sp.References[0].SpanID
			}

			status := "OK"
			for _, tag := range sp.Tags {
				if tag.Key == "error" && tag.Value == "true" {
					status = "ERROR"
					break
				}
			}

			tags := make(map[string]string)
			for _, tag := range sp.Tags {
				tags[tag.Key] = tag.Value
			}

			spans = append(spans, interfaces.Span{
				SpanID:        sp.SpanID,
				TraceID:       t.TraceID,
				ParentSpanID:  parentSpanID,
				OperationName: sp.OperationName,
				StartTime:     time.Unix(0, sp.StartTime*int64(time.Microsecond)),
				Duration:      sp.Duration / 1000,
				Status:        status,
				Tags:          tags,
			})
		}
	}

	return spans, nil
}

func (s *OTelQueryService) GetServices() ([]string, error) {
	if s.collectorURL == "" {
		return []string{}, nil
	}

	queryURL := fmt.Sprintf("%s/api/services", s.collectorURL)

	resp, err := s.client.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query services: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result struct {
		Data []string `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Data, nil
}
