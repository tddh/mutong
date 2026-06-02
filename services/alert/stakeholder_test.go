package alert

import (
	"testing"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestDetermineStakeholderStrategy_None(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			BusinessContext: alert_models.BusinessContext{AppName: ""},
		},
	}
	if got := determineStakeholderStrategy(a); got != "none" {
		t.Errorf("expected none, got %s", got)
	}
}

func TestDetermineStakeholderStrategy_Node(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType:    "Node",
			BusinessContext: alert_models.BusinessContext{AppName: "some-node"},
		},
	}
	if got := determineStakeholderStrategy(a); got != "node_pods" {
		t.Errorf("expected node_pods, got %s", got)
	}
}

func TestDetermineStakeholderStrategy_MiddlewareBroker(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			Alert:        alert_models.Alert{Labels: map[string]string{"alertname": "KafkaBrokerDown"}},
			BusinessContext: alert_models.BusinessContext{
				AppName: "kafka", ServiceType: "middleware",
			},
		},
	}
	if got := determineStakeholderStrategy(a); got != "upstream_all" {
		t.Errorf("expected upstream_all, got %s", got)
	}
}

func TestDetermineStakeholderStrategy_MiddlewareConsumerLag(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			Alert:        alert_models.Alert{Labels: map[string]string{"alertname": "ConsumerLag"}},
			BusinessContext: alert_models.BusinessContext{
				AppName: "kafka", ServiceType: "middleware",
			},
		},
	}
	if got := determineStakeholderStrategy(a); got != "upstream_consumer" {
		t.Errorf("expected upstream_consumer, got %s", got)
	}
}

func TestDetermineStakeholderStrategy_MiddlewareOther(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			Alert:        alert_models.Alert{Labels: map[string]string{"alertname": "DiskFull"}},
			BusinessContext: alert_models.BusinessContext{
				AppName: "kafka", ServiceType: "middleware",
			},
		},
	}
	if got := determineStakeholderStrategy(a); got != "none" {
		t.Errorf("expected none for non-consumer middleware alert, got %s", got)
	}
}

func TestDetermineStakeholderStrategy_BusinessService(t *testing.T) {
	a := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			Alert:        alert_models.Alert{Labels: map[string]string{"alertname": "OOMKilled"}},
			BusinessContext: alert_models.BusinessContext{
				AppName: "order-service", ServiceType: "",
			},
		},
	}
	if got := determineStakeholderStrategy(a); got != "upstream_callers" {
		t.Errorf("expected upstream_callers, got %s", got)
	}
}
