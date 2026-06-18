package inspection

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/zap"

	"gitee.com/tddh/mutong/interfaces"
	"gitee.com/tddh/mutong/models/inspection"
)

type InspectionEngine struct {
	logger  interfaces.Logger
	graphDB interfaces.GraphDB
	rules   map[string]interfaces.InspectionRule
	mu      sync.RWMutex
}

func NewInspectionEngine(logger interfaces.Logger, graphDB interfaces.GraphDB) interfaces.InspectionEngine {
	return &InspectionEngine{
		logger:  logger,
		graphDB: graphDB,
		rules:   make(map[string]interfaces.InspectionRule),
	}
}

func (e *InspectionEngine) RegisterRule(rule interfaces.InspectionRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, exists := e.rules[rule.Name()]; exists {
		e.logger.Debug("Rule already registered, skipping",
			zap.String("name", rule.Name()))
		return
	}
	e.rules[rule.Name()] = rule
	e.logger.Debug("Registered inspection rule", zap.String("name", rule.Name()))
}

func (e *InspectionEngine) ExecuteAll(ctx context.Context) ([]inspection.InspectionResult, error) {
	e.mu.RLock()
	ruleNames := make([]string, 0, len(e.rules))
	for name := range e.rules {
		ruleNames = append(ruleNames, name)
	}
	sort.Strings(ruleNames)
	rules := make([]interfaces.InspectionRule, 0, len(ruleNames))
	for _, name := range ruleNames {
		rules = append(rules, e.rules[name])
	}
	e.mu.RUnlock()

	var (
		mu          sync.Mutex
		allResults  []inspection.InspectionResult
		failedRules []string
	)

	var wg sync.WaitGroup
	for _, rule := range rules {
		wg.Add(1)
		go func(rule interfaces.InspectionRule) {
			defer wg.Done()
			results, err := rule.Execute(ctx, e.graphDB)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				e.logger.Error("Rule execution failed",
					zap.String("rule", rule.Name()),
					zap.Error(err))
				failedRules = append(failedRules, rule.Name())
			} else {
				allResults = append(allResults, results...)
			}
		}(rule)
	}
	wg.Wait()

	var errs error
	if len(failedRules) > 0 {
		errs = fmt.Errorf("rule execution failed: %v", failedRules)
	}
	return allResults, errs
}

func (e *InspectionEngine) ExecuteRule(ctx context.Context, name string) ([]inspection.InspectionResult, error) {
	e.mu.RLock()
	rule, exists := e.rules[name]
	e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("rule not found: %s", name)
	}

	return rule.Execute(ctx, e.graphDB)
}

func (e *InspectionEngine) LoadRulesFromDB(store *RuleStore) error {
	dbRules, err := store.List(true)
	if err != nil {
		return fmt.Errorf("load rules from db: %w", err)
	}
	for _, dbRule := range dbRules {
		rule := NewDBBackedRule(dbRule, e.graphDB, e.logger)
		e.RegisterRule(rule)
	}
	return nil
}
