package diagnosis

import (
	"testing"

	alert_models "gitee.com/tddh/mutong/models/alert"
)

func TestBuildExpandFlags_EmptyAlert(t *testing.T) {
	alert := &alert_models.ProcessedAlert{}
	flags := buildExpandFlags(alert, nil)
	if flags.SameNode || flags.SameOwner || flags.SameService || flags.SameBizApp || flags.UpstreamBizApp || flags.DownstreamBizApp {
		t.Errorf("expected all flags false for empty alert, got %+v", flags)
	}
}

func TestBuildExpandFlags_NodeAlert(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Node",
			OwnerKind:    "",
			Alert: alert_models.Alert{
				Labels: map[string]string{"alertname": "NodeNotReady"},
			},
			BusinessContext: alert_models.BusinessContext{AppName: "test"},
		},
	}
	flags := buildExpandFlags(alert, nil)
	if !flags.SameNode {
		t.Error("expected SameNode=true for Node alert")
	}
	if flags.SameOwner {
		t.Error("expected SameOwner=false for Node without OwnerKind")
	}
}

func TestBuildExpandFlags_PodWithOwner(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			OwnerKind:    "Deployment",
			Alert: alert_models.Alert{
				Labels: map[string]string{"alertname": "OOMKilled"},
			},
			BusinessContext: alert_models.BusinessContext{AppName: "test"},
		},
	}
	flags := buildExpandFlags(alert, nil)
	if !flags.SameOwner {
		t.Error("expected SameOwner=true for Pod with Deployment owner")
	}
	if flags.SameService {
		t.Error("expected SameService=false for OOM alert")
	}
}

func TestBuildExpandFlags_TrafficAlert(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			OwnerKind:    "Deployment",
			Alert: alert_models.Alert{
				Labels: map[string]string{"alertname": "HighErrorRate"},
			},
			BusinessContext: alert_models.BusinessContext{AppName: "test"},
		},
	}
	flags := buildExpandFlags(alert, nil)
	if !flags.SameService {
		t.Error("expected SameService=true for traffic alert")
	}
}

func TestBuildExpandFlags_MiddlewareServiceType(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			OwnerKind:    "StatefulSet",
			Alert: alert_models.Alert{
				Labels: map[string]string{"alertname": "KafkaBrokerDown"},
			},
			BusinessContext: alert_models.BusinessContext{
				AppName:     "kafka",
				ServiceType: "middleware",
			},
		},
	}
	flags := buildExpandFlags(alert, nil)
	if !flags.UpstreamBizApp {
		t.Error("expected UpstreamBizApp=true for middleware alert")
	}
}

func TestBuildExpandFlags_NodeMetricsPressure(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType: "Pod",
			OwnerKind:    "Deployment",
			Alert: alert_models.Alert{
				Labels: map[string]string{"alertname": "OOMKilled"},
			},
			BusinessContext: alert_models.BusinessContext{AppName: "test"},
		},
	}
	metrics := &NodeMetrics{
		CPUUtilization:    45,
		MemoryUtilization: 92,
		DiskUtilization:   30,
	}
	flags := buildExpandFlags(alert, metrics)
	if !flags.SameNode {
		t.Error("expected SameNode=true when memory > 80%")
	}
}

func TestBuildExpandFlags_NilLabels(t *testing.T) {
	alert := &alert_models.ProcessedAlert{
		EnrichedAlert: alert_models.EnrichedAlert{
			ResourceType:    "Pod",
			OwnerKind:       "Deployment",
			Alert:           alert_models.Alert{},
			BusinessContext: alert_models.BusinessContext{AppName: "test"},
		},
	}
	flags := buildExpandFlags(alert, nil)
	if flags.SameNode {
		t.Error("expected SameNode=false when Labels is nil")
	}
	if flags.SameService {
		t.Error("expected SameService=false when Labels is nil")
	}
}

func TestNodeMetricsMaxUtilization(t *testing.T) {
	m := &NodeMetrics{
		CPUUtilization: 45, MemoryUtilization: 92, DiskUtilization: 30,
	}
	if m.MaxUtilization() != 0.92 {
		t.Errorf("expected 0.92, got %f", m.MaxUtilization())
	}
}

func TestIsServiceLevel(t *testing.T) {
	tests := []struct {
		rt       string
		expected bool
	}{
		{"Deployment", true},
		{"Service", true},
		{"StatefulSet", true},
		{"Pod", false},
		{"Node", false},
	}
	for _, tt := range tests {
		a := &alert_models.ProcessedAlert{EnrichedAlert: alert_models.EnrichedAlert{ResourceType: tt.rt}}
		if got := isServiceLevel(a); got != tt.expected {
			t.Errorf("isServiceLevel(%s) = %v, want %v", tt.rt, got, tt.expected)
		}
	}
}
