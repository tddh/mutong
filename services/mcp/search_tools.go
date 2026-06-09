package mcp

import (
	"context"
	"fmt"
	"strings"

	"gitee.com/tddh/mutong/services/diagnosis"
	"gitee.com/tddh/mutong/services/search"
)

type AuditLogFunc func(ctx context.Context, toolName, engine string, query string, redactions, returned int, blocked bool, reason string)

func (s *Server) WithExternalSearch(tavily *search.TavilyClient, github *search.GitHubClient, sanitizer *diagnosis.Sanitizer, auditFunc AuditLogFunc) *Server {
	if tavily != nil && sanitizer != nil {
		s.tools["search_knowledge_base"] = func(ctx context.Context, args map[string]string) (string, error) {
			return s.handleSearchKnowledgeBase(ctx, args, tavily, sanitizer, auditFunc)
		}
	}
	if github != nil && sanitizer != nil {
		s.tools["search_github_issues"] = func(ctx context.Context, args map[string]string) (string, error) {
			return s.handleSearchGitHubIssues(ctx, args, github, sanitizer, auditFunc)
		}
	}
	return s
}

func (s *Server) handleSearchKnowledgeBase(ctx context.Context, args map[string]string, tavily *search.TavilyClient, sanitizer *diagnosis.Sanitizer, auditFunc AuditLogFunc) (string, error) {
	query, ok := args["query"]
	if !ok || query == "" {
		return "[错误: 缺少搜索词 query 参数]", nil
	}

	topics := map[string]bool{"general": true, "news": true, "security": true}
	topic := args["topic"]
	if topic == "" {
		topic = "general"
	}
	if !topics[topic] {
		topic = "general"
	}

	sanitized, count := sanitizer.Redact(query)
	if sanitizer.ShouldBlock(sanitized, count) {
		if auditFunc != nil {
			auditFunc(ctx, "search_knowledge_base", "tavily", sanitized, count, 0, true, "blocked: high PII or banned term")
		}
		return "[搜索被阻止: 查询包含过多敏感信息]", nil
	}

	fingerprint := sanitizer.ExtractSearchFingerprint(sanitized)

	results, answer, err := tavily.Search(ctx, fingerprint, topic)
	if err != nil {
		return fmt.Sprintf("[搜索失败: %v]", err), nil
	}

	filtered := sanitizer.FilterResults(toSearchResults(results))

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("搜索查询: %s\n\n", fingerprint))
	if answer != "" {
		sb.WriteString(fmt.Sprintf("AI 摘要: %s\n\n", answer))
	}
	for i, r := range filtered {
		content := r.Content
		if len(content) > 400 {
			content = content[:400] + "..."
		}
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("    来源: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("    内容: %s\n\n", content))
	}

	if len(filtered) == 0 {
		sb.WriteString("[未找到相关结果，请尝试其他搜索词]\n")
	}

	if auditFunc != nil {
		auditFunc(ctx, "search_knowledge_base", "tavily", sanitized, count, len(filtered), false, "")
	}

	return sb.String(), nil
}

func toSearchResults(in []search.Result) []diagnosis.SearchResult {
	out := make([]diagnosis.SearchResult, 0, len(in))
	for _, r := range in {
		out = append(out, diagnosis.SearchResult{
			Title:   r.Title,
			Content: r.Content,
			URL:     r.URL,
			Score:   r.Score,
		})
	}
	return out
}

func (s *Server) handleSearchGitHubIssues(ctx context.Context, args map[string]string, github *search.GitHubClient, sanitizer *diagnosis.Sanitizer, auditFunc AuditLogFunc) (string, error) {
	query, ok := args["query"]
	if !ok || query == "" {
		return "[错误: 缺少搜索词 query 参数]", nil
	}

	repo := args["repo"]
	state := args["state"]
	if state == "" {
		state = "open"
	}

	sanitized, count := sanitizer.Redact(query)
	if sanitizer.ShouldBlock(sanitized, count) {
		if auditFunc != nil {
			auditFunc(ctx, "search_github_issues", "github", sanitized, count, 0, true, "blocked: high PII or banned term")
		}
		return "[搜索被阻止: 查询包含过多敏感信息]", nil
	}

	fingerprint := sanitizer.ExtractSearchFingerprint(sanitized)

	issues, err := github.SearchIssues(ctx, fingerprint, repo, state)
	if err != nil {
		return fmt.Sprintf("[GitHub 搜索失败: %v]", err), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("GitHub Issues 搜索: %s\n\n", fingerprint))
	if repo != "" {
		sb.WriteString(fmt.Sprintf("仓库限制: %s\n", repo))
	}
	sb.WriteString(fmt.Sprintf("状态过滤: %s\n\n", state))

	for i, iss := range issues {
		sb.WriteString(fmt.Sprintf("[%d] #%d %s\n", i+1, iss.Number, iss.Title))
		sb.WriteString(fmt.Sprintf("    状态: %s | 创建: %s\n", iss.State, iss.Created))
		if len(iss.Labels) > 0 {
			sb.WriteString(fmt.Sprintf("    标签: %s\n", strings.Join(iss.Labels, ", ")))
		}
		sb.WriteString(fmt.Sprintf("    链接: %s\n\n", iss.URL))
	}

	if len(issues) == 0 {
		sb.WriteString("[未找到相关 Issues，请尝试其他关键词或仓库]\n")
	}

	if auditFunc != nil {
		auditFunc(ctx, "search_github_issues", "github", sanitized, count, len(issues), false, "")
	}

	return sb.String(), nil
}
