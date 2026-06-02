package alert

import (
	"testing"
)

func TestMapToAlert_BasicLabelMapping(t *testing.T) {
	source := &AlertSource{
		Name: "ceph-prod",
		Type: "ceph",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
			"severity":  "severity",
			"node":      "host",
		},
		EnrichmentStrategy: StrategyNodeAffinity,
		BusinessMapping: BusinessMappingConf{
			Team:        "storage-team",
			Criticality: "high",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "CephOSDNearFull",
		"severity":   "critical",
		"host":       "node-12",
		"osd_id":     "42",
		"pool":       "cephfs_data",
	}

	alert := source.MapToAlert(raw)

	if alert.Labels["alertname"] != "CephOSDNearFull" {
		t.Errorf("expected alertname=CephOSDNearFull, got %s", alert.Labels["alertname"])
	}
	if alert.Labels["node"] != "node-12" {
		t.Errorf("expected node=node-12, got %s", alert.Labels["node"])
	}
	if alert.Labels["team"] != "storage-team" {
		t.Errorf("expected team=storage-team, got %s", alert.Labels["team"])
	}
	if alert.Labels["alertSource"] != "ceph-prod" {
		t.Errorf("expected alertSource=ceph-prod, got %s", alert.Labels["alertSource"])
	}
	if alert.Labels["alertSourceType"] != "ceph" {
		t.Errorf("expected alertSourceType=ceph, got %s", alert.Labels["alertSourceType"])
	}
	if alert.Status != "firing" {
		t.Errorf("expected status=firing, got %s", alert.Status)
	}
	if alert.Fingerprint == "" {
		t.Error("expected non-empty fingerprint")
	}
}

func TestMapToAlert_ResolvedStatus(t *testing.T) {
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "TestAlert",
		"status":     "resolved",
	}

	alert := source.MapToAlert(raw)
	if alert.Status != "resolved" {
		t.Errorf("expected status=resolved, got %s", alert.Status)
	}
}

func TestMapToAlert_BusinessMappingFallback(t *testing.T) {
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
		},
		BusinessMapping: BusinessMappingConf{
			Team:         "默认团队",
			BusinessUnit: "基础设施",
			Criticality:  "medium",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "TestAlert",
	}

	alert := source.MapToAlert(raw)
	if alert.Labels["team"] != "默认团队" {
		t.Errorf("expected team=默认团队, got %s", alert.Labels["team"])
	}
	if alert.Labels["businessUnit"] != "基础设施" {
		t.Errorf("expected businessUnit=基础设施, got %s", alert.Labels["businessUnit"])
	}
}

func TestMapToAlert_BusinessMappingDoesNotOverrideLabels(t *testing.T) {
	// 如果标签中已经有 team 值，businessMapping 不应该覆盖
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
			"team":      "team",
		},
		BusinessMapping: BusinessMappingConf{
			Team: "默认团队",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "TestAlert",
		"team":       "my-team",
	}

	alert := source.MapToAlert(raw)
	if alert.Labels["team"] != "my-team" {
		t.Errorf("expected team=my-team (from raw), got %s", alert.Labels["team"])
	}
}

func TestGenerateFingerprint_Deterministic(t *testing.T) {
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
		},
	}

	raw := map[string]interface{}{
		"alert_name": "TestAlert",
		"instance":   "10.0.0.1:9090",
	}

	fp1 := source.GenerateFingerprint(raw)
	fp2 := source.GenerateFingerprint(raw)

	if fp1 != fp2 {
		t.Errorf("fingerprints should be deterministic: %s vs %s", fp1, fp2)
	}
	if len(fp1) == 0 {
		t.Error("fingerprint should not be empty")
	}
}

func TestGenerateFingerprint_WithCustomKeys(t *testing.T) {
	source := &AlertSource{
		Name: "test",
		LabelMapping: map[string]string{
			"alertname": "alert_name",
		},
		FingerprintKeys: []string{"alert_name", "host"},
	}

	raw1 := map[string]interface{}{
		"alert_name": "TestAlert",
		"host":       "node-1",
	}
	raw2 := map[string]interface{}{
		"alert_name": "TestAlert",
		"host":       "node-2",
	}

	fp1 := source.GenerateFingerprint(raw1)
	fp2 := source.GenerateFingerprint(raw2)

	if fp1 == fp2 {
		t.Errorf("fingerprints should differ for different hosts: %s", fp1)
	}
}

func TestGenerateFingerprint_FallbackToPayloadHash(t *testing.T) {
	source := &AlertSource{
		Name:            "test",
		FingerprintKeys: []string{"nonexistent"},
	}

	raw := map[string]interface{}{
		"some_field": "some_value",
	}

	fp := source.GenerateFingerprint(raw)
	if len(fp) == 0 {
		t.Error("fingerprint should not be empty even with no matching keys")
	}
}

func TestExtractBusinessLabels_DirectMatch(t *testing.T) {
	source := &AlertSource{
		Name:               "kafka-broker",
		Type:               "middleware",
		EnrichmentStrategy: StrategyBusinessLabels,
		BusinessMapping: BusinessMappingConf{
			ServiceType: "middleware",
		},
	}

	labels := map[string]string{
		"alertname":    "KafkaBrokerDown",
		"team":         "data-platform-team",
		"businessUnit": "数据基础设施",
		"criticality":  "P0",
		"appName":      "kafka-cluster",
	}

	bc := source.ExtractBusinessLabels(labels)
	if bc == nil {
		t.Fatal("expected non-nil BusinessContext")
	}
	if bc.AppName != "kafka-cluster" {
		t.Errorf("expected appName=kafka-cluster, got %s", bc.AppName)
	}
	if bc.Team != "data-platform-team" {
		t.Errorf("expected team=data-platform-team, got %s", bc.Team)
	}
	if bc.BusinessUnit != "数据基础设施" {
		t.Errorf("expected businessUnit=数据基础设施, got %s", bc.BusinessUnit)
	}
	if bc.ServiceType != "middleware" {
		t.Errorf("expected serviceType=middleware, got %s", bc.ServiceType)
	}
}

func TestExtractBusinessLabels_WithAlternativeKeys(t *testing.T) {
	source := &AlertSource{Name: "test"}

	labels := map[string]string{
		"alertname": "TestAlert",
		"app_name":  "my-app",
		"team":      "my-team",
		"env":       "production",
	}

	bc := source.ExtractBusinessLabels(labels)
	if bc == nil {
		t.Fatal("expected non-nil BusinessContext")
	}
	if bc.AppName != "my-app" {
		t.Errorf("expected appName=my-app, got %s", bc.AppName)
	}
	if bc.Team != "my-team" {
		t.Errorf("expected team=my-team, got %s", bc.Team)
	}
	if bc.Environment != "production" {
		t.Errorf("expected environment=production, got %s", bc.Environment)
	}
}

func TestExtractBusinessLabels_NilWhenEmpty(t *testing.T) {
	source := &AlertSource{Name: "test"}
	labels := map[string]string{
		"alertname": "SomeAlert",
		"severity":  "warning",
	}
	bc := source.ExtractBusinessLabels(labels)
	if bc != nil {
		t.Error("expected nil BusinessContext when no business labels present")
	}
}

func TestExtractBusinessLabels_TeamOnly(t *testing.T) {
	source := &AlertSource{Name: "test"}
	labels := map[string]string{
		"team": "my-team",
	}
	bc := source.ExtractBusinessLabels(labels)
	if bc == nil {
		t.Fatal("expected non-nil BusinessContext when team is present")
	}
	if bc.Team != "my-team" {
		t.Errorf("expected team=my-team, got %s", bc.Team)
	}
}

func TestDeepGet_SimpleKey(t *testing.T) {
	data := map[string]interface{}{
		"alertname": "TestAlert",
	}
	result := deepGet(data, "alertname")
	if result != "TestAlert" {
		t.Errorf("expected TestAlert, got %s", result)
	}
}

func TestDeepGet_NestedPath(t *testing.T) {
	data := map[string]interface{}{
		"labels": map[string]interface{}{
			"alertname": "TestAlert",
		},
	}
	result := deepGet(data, "labels.alertname")
	if result != "TestAlert" {
		t.Errorf("expected TestAlert, got %s", result)
	}
}

func TestDeepGet_NonexistentKey(t *testing.T) {
	data := map[string]interface{}{
		"alertname": "TestAlert",
	}
	result := deepGet(data, "nonexistent")
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestDeepGet_NestedNonexistent(t *testing.T) {
	data := map[string]interface{}{
		"labels": map[string]interface{}{
			"alertname": "TestAlert",
		},
	}
	result := deepGet(data, "labels.nonexistent")
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestDeepGet_FloatValue(t *testing.T) {
	data := map[string]interface{}{
		"value": float64(42.5),
	}
	result := deepGet(data, "value")
	if result != "42.5" {
		t.Errorf("expected 42.5, got %s", result)
	}
}

func TestDeepGet_BoolValue(t *testing.T) {
	data := map[string]interface{}{
		"enabled": true,
	}
	result := deepGet(data, "enabled")
	if result != "true" {
		t.Errorf("expected true, got %s", result)
	}
}
