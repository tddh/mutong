package diagnosis

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type SanitizerRule struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
	Enabled bool   `yaml:"enabled"`
}

type SanitizerConfig struct {
	Enabled                 bool            `yaml:"enabled"`
	HighPIIBlockThreshold   float64         `yaml:"highPIIBlockThreshold"`
	BannedTerms             []string        `yaml:"bannedTerms"`
	MaxQueryLength          int             `yaml:"maxQueryLength"`
	PromptInjectionPatterns []string        `yaml:"promptInjectionPatterns"`
	Rules                   []SanitizerRule `yaml:"rules"`
}

func DefaultSanitizerConfig() SanitizerConfig {
	return SanitizerConfig{
		Enabled:               true,
		HighPIIBlockThreshold: 0.5,
		MaxQueryLength:        200,
		PromptInjectionPatterns: []string{
			"ignore previous",
			"you are now",
			"system prompt",
			"<script>",
			"javascript:",
		},
		Rules: []SanitizerRule{
			{Name: "ip_v4", Pattern: `\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b`, Enabled: true},
			{Name: "email", Pattern: `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`, Enabled: true},
			{Name: "jwt", Pattern: `eyJ[A-Za-z0-9_-]{20,}`, Enabled: true},
			{Name: "secret", Pattern: `(?i)(password|passwd|secret)\s*[:=]\s*\S+`, Enabled: true},
			{Name: "aws_key", Pattern: `(AKIA|ABIA|ACCA|ASIA)[A-Za-z0-9]{16,}`, Enabled: false},
			{Name: "ali_key", Pattern: `(LTAI)[A-Za-z0-9]{12,}`, Enabled: false},
		},
	}
}

type Sanitizer struct {
	mu             sync.Mutex
	replacementMap map[string]string
	counterMap     map[string]int
	rules          []compiledRule
	injectionRe    []*regexp.Regexp
	bannedTerms    []string
	maxQueryLength int
	highPIIThresh  float64
	sessionID      string
}

type compiledRule struct {
	name        string
	regex       *regexp.Regexp
	placeholder string
	enabled     bool
}

type SearchResult struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	URL     string  `json:"url"`
	Score   float64 `json:"score"`
}

func NewSanitizer(sessionID string, cfg SanitizerConfig) (*Sanitizer, error) {
	s := &Sanitizer{
		replacementMap: make(map[string]string),
		counterMap:     make(map[string]int),
		highPIIThresh:  cfg.HighPIIBlockThreshold,
		maxQueryLength: cfg.MaxQueryLength,
		bannedTerms:    cfg.BannedTerms,
		sessionID:      sessionID,
	}

	if s.highPIIThresh == 0 {
		s.highPIIThresh = 0.5
	}
	if s.maxQueryLength == 0 {
		s.maxQueryLength = 200
	}

	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compile sanitizer rule %q: %w", r.Name, err)
		}
		s.rules = append(s.rules, compiledRule{
			name:        r.Name,
			regex:       re,
			placeholder: fmt.Sprintf("[REDACTED_%s]", strings.ToUpper(r.Name)),
			enabled:     true,
		})
	}

	for _, p := range cfg.PromptInjectionPatterns {
		re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(p))
		if err != nil {
			continue
		}
		s.injectionRe = append(s.injectionRe, re)
	}

	return s, nil
}

func (s *Sanitizer) Redact(text string) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for i := range s.rules {
		rule := &s.rules[i]
		if !rule.enabled {
			continue
		}
		text = rule.regex.ReplaceAllStringFunc(text, func(match string) string {
			count++
			s.counterMap[rule.name]++
			id := s.counterMap[rule.name]
			placeholder := fmt.Sprintf("[REDACTED_%s_%d]", strings.ToUpper(rule.name), id)
			s.replacementMap[placeholder] = match
			return placeholder
		})
	}
	return text, count
}

func (s *Sanitizer) Restore(text string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	restored := text
	for placeholder, original := range s.replacementMap {
		restored = strings.ReplaceAll(restored, placeholder, original)
	}
	return restored
}

func (s *Sanitizer) Genericize(text string) string {
	re := regexp.MustCompile(`\[REDACTED_([A-Z0-9_]+)_\d+\]`)
	return re.ReplaceAllString(text, "<$1>")
}

func (s *Sanitizer) ExtractSearchFingerprint(redactedText string) string {
	if s.maxQueryLength > 0 && len(redactedText) > s.maxQueryLength {
		return redactedText[:s.maxQueryLength] + "..."
	}
	return redactedText
}

func (s *Sanitizer) ShouldBlock(redactedText string, redactionCount int) bool {
	for _, term := range s.bannedTerms {
		if strings.Contains(strings.ToLower(redactedText), strings.ToLower(term)) {
			return true
		}
	}

	if redactionCount == 0 {
		return false
	}

	if s.highPIIThresh > 0 {
		words := strings.Fields(redactedText)
		if len(words) == 0 {
			return false
		}
		redactedWords := 0
		for _, w := range words {
			if strings.Contains(w, "[REDACTED_") {
				redactedWords++
			}
		}
		if float64(redactedWords)/float64(len(words)) > s.highPIIThresh {
			return true
		}
	}
	return false
}

func (s *Sanitizer) FilterResults(results []SearchResult) []SearchResult {
	filtered := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if s.containsInjection(r.Title) || s.containsInjection(r.Content) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func (s *Sanitizer) containsInjection(text string) bool {
	for _, re := range s.injectionRe {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
