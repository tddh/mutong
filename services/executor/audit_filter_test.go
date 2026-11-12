package executor

import (
	"testing"

	ex "gitee.com/tddh/mutong/models/executor"
)

func TestMatchesAuditLogFingerprint(t *testing.T) {
	a := ex.AuditLog{Plan: ex.ExecutionPlan{Fingerprint: "fp-1"}}
	if !matchesAuditLog(a, map[string]string{"fingerprint": "fp-1"}) {
		t.Fatal("matching fingerprint must pass")
	}
	if matchesAuditLog(a, map[string]string{"fingerprint": "fp-2"}) {
		t.Fatal("non-matching fingerprint must be filtered out")
	}
	if !matchesAuditLog(a, nil) {
		t.Fatal("empty filters must pass")
	}
}
