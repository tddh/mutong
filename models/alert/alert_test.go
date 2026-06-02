package alert_test

import (
	"testing"

	"gitee.com/tddh/mutong/models/alert"
)

func TestEnrichedAlert(t *testing.T) {
	ea := alert.EnrichedAlert{
		ResourceType: "Pod",
		Namespace:    "default",
		EnrichTags:   map[string]string{"key": "value"},
	}
	if ea.ResourceType != "Pod" {
		t.Error("ResourceType mismatch")
	}
}

func TestProcessedAlert(t *testing.T) {
	pa := alert.ProcessedAlert{
		RuleChainID: "test-chain",
	}
	if pa.RuleChainID != "test-chain" {
		t.Error("RuleChainID mismatch")
	}
}
