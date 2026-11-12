package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"gitee.com/tddh/mutong/config"
	maudit "gitee.com/tddh/mutong/models/audit"
	ex "gitee.com/tddh/mutong/models/executor"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func seedAuditLog(t *testing.T, e *K8sExecutor, id, action, ns string, auto bool, approver string) {
	t.Helper()
	err := e.RecordAudit(context.Background(), ex.AuditLog{
		ID: id,
		Plan: ex.ExecutionPlan{
			ID: "plan-" + id, Action: ex.ActionType(action), Namespace: ns,
			ResourceName: "res-" + id, Target: "Pod", ApprovedBy: approver,
		},
		Result:       ex.ExecutionResult{Success: true, Message: "ok", Timestamp: time.Now()},
		AutoExecuted: auto,
		Timestamp:    time.Now(),
	})
	if err != nil {
		t.Fatalf("record audit: %v", err)
	}
}

func TestExecutorAuditQueryFilters(t *testing.T) {
	cfg := &config.Config{}
	e := NewK8sExecutor(zap.NewNop(), nil, cfg)
	defer e.Stop()

	seedAuditLog(t, e, "a1", "restart_pod", "team-a", true, "")
	seedAuditLog(t, e, "a2", "scale_deployment", "team-a", false, "alice")
	seedAuditLog(t, e, "a3", "restart_pod", "team-b", false, "bob")

	tests := []struct {
		name    string
		filters map[string]string
		wantIDs []string
	}{
		{"no filters", map[string]string{}, []string{"a1", "a2", "a3"}},
		{"action case-insensitive", map[string]string{"action": "RESTART_POD"}, []string{"a1", "a3"}},
		{"namespace", map[string]string{"namespace": "team-b"}, []string{"a3"}},
		{"autoExecuted true", map[string]string{"autoExecuted": "true"}, []string{"a1"}},
		{"autoExecuted false", map[string]string{"autoExecuted": "false"}, []string{"a2", "a3"}},
		{"combined", map[string]string{"action": "restart_pod", "namespace": "team-a"}, []string{"a1"}},
		{"no match", map[string]string{"namespace": "ghost"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs, err := e.GetAuditLogs(context.Background(), tc.filters)
			if err != nil {
				t.Fatal(err)
			}
			if len(logs) != len(tc.wantIDs) {
				t.Fatalf("got %d logs, want %d: %+v", len(logs), len(tc.wantIDs), logs)
			}
			for i, want := range tc.wantIDs {
				if logs[i].ID != want {
					t.Errorf("log[%d].ID = %q, want %q", i, logs[i].ID, want)
				}
			}
		})
	}
}

func TestExecutorGetAuditLogByID(t *testing.T) {
	e := NewK8sExecutor(zap.NewNop(), nil, &config.Config{})
	defer e.Stop()
	seedAuditLog(t, e, "a1", "restart_pod", "ns", false, "alice")

	got, err := e.GetAuditLogByID("a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "a1" || got.Plan.ApprovedBy != "alice" {
		t.Fatalf("unexpected audit log: %+v", got)
	}
	// Returned value must be a copy: mutating it must not corrupt the store.
	got.Plan.ApprovedBy = "mallory"
	again, _ := e.GetAuditLogByID("a1")
	if again.Plan.ApprovedBy != "alice" {
		t.Fatal("GetByID must return a copy, not an alias into the store")
	}
	if _, err := e.GetAuditLogByID("missing"); err == nil {
		t.Fatal("missing id must return an error")
	}
}

// Audit wiring: with a DB configured, every execution must leave both an
// execution audit row and an operation audit row, with a sane operator.
func TestExecutorAuditWiringWithDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DB: db}
	cfg.Executor.AuditLog.Type = "postgres"
	cfg.Executor.AuditLog.Table = "exec_audit_test"
	cfg.Executor.AuditLog.OperationTable = "op_audit_test"

	e := NewK8sExecutor(zap.NewNop(), nil, cfg)
	defer e.Stop()

	seedAuditLog(t, e, "a1", "restart_pod", "team-a", true, "")      // operator falls back to system
	seedAuditLog(t, e, "a2", "delete_pod", "team-a", false, "alice") // explicit approver

	// Execution audit persisted and queryable through the executor.
	logs, err := e.GetAuditLogs(context.Background(), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected 2 persisted audit logs, got %d", len(logs))
	}

	// Operation audit rows carry the operator and action.
	var ops []maudit.OperationAuditLog
	if err := db.Table("op_audit_test").Order("id ASC").Find(&ops).Error; err != nil {
		t.Fatal(err)
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 operation audit rows, got %d", len(ops))
	}
	if ops[0].Operator != "system" || ops[0].Action != "restart_pod" {
		t.Fatalf("auto execution must be attributed to system: %+v", ops[0])
	}
	if ops[1].Operator != "alice" || ops[1].Action != "delete_pod" || ops[1].Namespace != "team-a" {
		t.Fatalf("approved execution must be attributed to the approver: %+v", ops[1])
	}
}

func TestMemoryAuditLogStoreWrapper(t *testing.T) {
	store := NewMemoryAuditLogStore()
	log := ex.AuditLog{
		ID:        "audit-1",
		Plan:      ex.ExecutionPlan{ID: "p1", Action: ex.ActionRestartPod, Namespace: "ns"},
		Result:    ex.ExecutionResult{Success: false, Message: "pending"},
		Timestamp: time.Now(),
	}
	if err := store.Save(log); err != nil {
		t.Fatal(err)
	}
	logs, err := store.List(map[string]string{"action": "restart_pod"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("list: %v, %+v", err, logs)
	}
	got, err := store.GetByID("audit-1")
	if err != nil || got.Plan.ID != "p1" {
		t.Fatalf("get: %v, %+v", err, got)
	}
	if _, err := store.GetByID("nope"); err == nil {
		t.Fatal("missing id must error")
	}
	if err := store.UpdateResult("p1", true, "done"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID("audit-1")
	if !got.Result.Success || got.Result.Message != "done" {
		t.Fatalf("UpdateResult not applied: %+v", got.Result)
	}
	if err := store.UpdateResult("ghost", true, "x"); err == nil {
		t.Fatal("UpdateResult for unknown plan must error")
	}
}

func TestPostgresAuditStoreRoundtrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgresAuditStore(zap.NewNop(), db, "exec_audit_rt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPostgresAuditStore(zap.NewNop(), nil, "x"); err == nil {
		t.Fatal("nil db must be rejected")
	}

	log := ex.AuditLog{
		ID: "audit-1",
		Plan: ex.ExecutionPlan{
			ID: "p1", Action: ex.ActionRestartPod, Namespace: "ns",
			ResourceName: "pod-1", Fingerprint: "fp-1", ApprovedBy: "alice",
		},
		Result:       ex.ExecutionResult{Success: true, Message: "pod restarted"},
		AutoExecuted: false,
		ApprovedBy:   "alice",
		Timestamp:    time.Now(),
	}
	if err := store.Save(log); err != nil {
		t.Fatal(err)
	}

	// The DB-backed store supports fingerprint filtering.
	logs, err := store.List(map[string]string{"fingerprint": "fp-1"})
	if err != nil || len(logs) != 1 {
		t.Fatalf("fingerprint filter: %v, %+v", err, logs)
	}
	if logs[0].Plan.ResourceName != "pod-1" || logs[0].ApprovedBy != "alice" {
		t.Fatalf("roundtrip lost plan fields: %+v", logs[0])
	}
	logs, err = store.List(map[string]string{"fingerprint": "other"})
	if err != nil || len(logs) != 0 {
		t.Fatalf("non-matching fingerprint must return empty: %v, %+v", err, logs)
	}

	got, err := store.GetByID(logs0ID(t, store))
	if err != nil || got.Plan.ID != "p1" {
		t.Fatalf("GetByID: %v, %+v", err, got)
	}
	if err := store.UpdateResult("p1", false, "rolled back"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(logs0ID(t, store))
	if got.Result.Success || got.Result.Message != "rolled back" {
		t.Fatalf("UpdateResult not persisted: %+v", got.Result)
	}
}

// logs0ID returns the storage ID of the first persisted audit row. The
// DB-backed store keys rows by numeric primary key (the controller strips the
// "audit-" display prefix before lookup).
func logs0ID(t *testing.T, store AuditLogStore) string {
	t.Helper()
	logs, err := store.List(map[string]string{})
	if err != nil || len(logs) == 0 {
		t.Fatalf("expected persisted logs: %v", err)
	}
	return strings.TrimPrefix(logs[0].ID, "audit-")
}
