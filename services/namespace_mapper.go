package services

import (
	"path/filepath"
	"sync"

	"gitee.com/tddh/mutong/config"
)

// NamespaceMapper maps Kubernetes namespaces to business attributes
type NamespaceMapper struct {
	entries []namespaceMappingRule
	mu      sync.RWMutex
}

type namespaceMappingRule struct {
	pattern string
	entry   config.NamespaceMappingEntry
}

// NewNamespaceMapper creates a NamespaceMapper from configuration
func NewNamespaceMapper(conf config.BusinessTopologyConf) *NamespaceMapper {
	m := &NamespaceMapper{}
	for pattern, entry := range conf.NamespaceMapping {
		m.entries = append(m.entries, namespaceMappingRule{
			pattern: pattern,
			entry:   entry,
		})
	}
	return m
}

// Resolve returns business attributes for the given namespace.
// It matches namespace against configured patterns (supports glob wildcards).
// Exact matches take priority over wildcard patterns.
func (m *NamespaceMapper) Resolve(namespace string) config.NamespaceMappingEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var bestMatch config.NamespaceMappingEntry
	var bestMatchLen int

	for _, rule := range m.entries {
		matched, err := filepath.Match(rule.pattern, namespace)
		if err != nil {
			continue
		}
		if matched {
			patternLen := len(rule.pattern)
			if patternLen > bestMatchLen {
				bestMatch = rule.entry
				bestMatchLen = patternLen
			}
		}
	}

	return bestMatch
}

func (m *NamespaceMapper) Update(conf config.BusinessTopologyConf) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = make([]namespaceMappingRule, 0, len(conf.NamespaceMapping))
	for pattern, entry := range conf.NamespaceMapping {
		m.entries = append(m.entries, namespaceMappingRule{
			pattern: pattern,
			entry:   entry,
		})
	}
}
