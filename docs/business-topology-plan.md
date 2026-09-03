# 业务拓扑身份建模修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 消除业务拓扑的双身份分裂（BLS 用 label 名、Trace 用 owner 名建出两个 BusinessApp），修正 STS DNS 解析错误与 self-call 过滤误杀有状态副本互连，并以统一身份解析收敛三方（BLS/Trace/告警）。

**Architecture:** 保持 workload 级为业务拓扑规范中心（Pod 留在物理层），引入 `owner_name` 别名属性作为「workload 技术名 → 业务名」的桥接：BLS 建点时把 workload 的 owner 名写入 BusinessApp，Trace 解析 caller 时先按 owner 名反查 canonical 顶点，命中则复用、miss 才自动建点（并带 owner 名供后续合并）。self-call 过滤从「同 app 丢」收紧为「同 pod 才丢」。

**Tech Stack:** Go 1.25 + NebulaGraph v3（nGQL）+ OTLP trace protobuf + BigCache。

---

## 背景事实（已核实，来自生产图库与源码）

- `trace_topology_syncer.go:342` 的 `caller.uid == callee.uid` 过滤把 kafka broker 副本间连接整体丢弃（app 粒度）。
- `business_label_syncer.go:256` BLS 用 `app.kubernetes.io/name` label 建点（kafka → `kafka`）；`trace_topology_syncer.go:371` Trace 用 `k8s.owner.name` 建点（kafka-controller → `kafka-controller`）。两个名字、两个 `bizapp-{cluster}-{ns}-{name}` UID。
- `resolvePeerFromAddress:486-524` 对 `kafka-0.kafka-headless.base.svc.cluster.local` 解析出 appName=`kafka-0`、namespace=`kafka-headless`（两段式取错，且把 pod 名当服务名）。
- `business_topology_service.go:218` 的 `matchBusinessAppByName` 查询了 `a.BusinessApp.is_deleted == false`，但 BusinessApp tag 没有 `is_deleted` 属性（schema.ngql:174-182），该查询恒失败。
- 生产实测：`base-kafka` 顶点无任何 broker↔broker 边；`base-kafka`（businessUnit=基础中间件）与 `base-kafka-controller`（属性空）并存。

---

## 文件结构

**新建：**
- `services/trace_topology_syncer_alias.go` — owner_name 别名查找 + K8s FQDN 解析 + pod 名提取（纯函数与 TraceSyncer 方法，独立文件便于单测）

**修改：**
- `scripts/schema.ngql:174-185` — BusinessApp tag 增加 `owner_name`/`owner_kind`，加 `bizapp_owner_name` 索引
- `models/business_app.go` — struct 增加 `OwnerName`/`OwnerKind` 字段
- `services/business_label_syncer.go` — syncApp 写入 owner_name/owner_kind，batchUpsert + hash 覆盖
- `services/trace_topology_syncer.go` — caller/callee 解析改走别名 + 新 FQDN 解析 + self-call 过滤收紧
- `services/business_topology_service.go:216-222` — 移除 is_deleted 条件
- `cmd/mergebizapp/main.go` — 一次性合并重复 BusinessApp 顶点的维护命令

**测试：**
- `services/trace_topology_syncer_alias_test.go` — FQDN 解析 / pod 名提取 / owner 别名查找单测
- `services/trace_topology_syncer_test.go` — 更新 self-call 过滤测试

---

## Task 1: Schema 增加 owner_name/owner_kind 别名属性

**Files:**
- Modify: `scripts/schema.ngql:174-185`
- Modify: `models/business_app.go`
- Migration: 生产环境执行 `ALTER TAG`（一次性）

- [ ] **Step 1: 更新 schema.ngql**

把 `scripts/schema.ngql` 的 BusinessApp 定义改为：

```ngql
CREATE TAG IF NOT EXISTS BusinessApp (
    uid string,
    app_name string,
    namespace string,
    criticality string,
    environment string,
    team string,
    business_unit string,
    owner_name string,
    owner_kind string
) comment = '业务应用实体';

CREATE TAG INDEX IF NOT EXISTS `bizapp_name` ON `BusinessApp`(`app_name`(128));
CREATE TAG INDEX IF NOT EXISTS `bizapp_ns` ON `BusinessApp`(`namespace`(128));
CREATE TAG INDEX IF NOT EXISTS `bizapp_owner_name` ON `BusinessApp`(`owner_name`(128));
```

- [ ] **Step 2: 更新 models.BusinessApp**

`models/business_app.go` 的 struct 末尾加两个字段：

```go
type BusinessApp struct {
	UID          string `json:"uid" nebula:"uid"`
	AppName      string `json:"app_name" nebula:"app_name"`
	Namespace    string `json:"namespace" nebula:"namespace"`
	Criticality  string `json:"criticality" nebula:"criticality"`
	Environment  string `json:"environment" nebula:"environment"`
	Team         string `json:"team" nebula:"team"`
	BusinessUnit string `json:"business_unit" nebula:"business_unit"`
	OwnerName    string `json:"owner_name" nebula:"owner_name"`
	OwnerKind    string `json:"owner_kind" nebula:"owner_kind"`
}
```

- [ ] **Step 3: 生产环境一次性迁移（已有部署必须执行）**

`CREATE TAG IF NOT EXISTS` 不会给已存在的 tag 加属性，需在 NebulaGraph 手动执行（k8s-m1 的 nebula-console pod）：

```bash
kubectl exec -i -n nebula-cluster nebula-console -- nebula-console \
  -addr nebula-graphd-svc -port 9669 -u root -p nebula \
  -e "USE mutong; ALTER TAG BusinessApp ADD (owner_name string, owner_kind string);"
kubectl exec -i -n nebula-cluster nebula-console -- nebula-console \
  -addr nebula-graphd-svc -port 9669 -u root -p nebula \
  -e "USE mutong; CREATE TAG INDEX IF NOT EXISTS bizapp_owner_name ON BusinessApp(owner_name(128));"
```

Expected: `Execution succeeded`。

- [ ] **Step 4: 编译验证**

Run: `go build ./...`
Expected: exit 0（Step 2 只加字段，不影响编译）。

- [ ] **Step 5: Commit**

```bash
git add scripts/schema.ngql models/business_app.go
git commit -m "feat(topology): BusinessApp 增加 owner_name/owner_kind 别名属性"
```

---

## Task 2: BLS 写入 owner_name/owner_kind

**Files:**
- Modify: `services/business_label_syncer.go:275-283, 316-322, 477-493`

- [ ] **Step 1: syncApp 填充 OwnerName/OwnerKind**

在 `syncApp`（`business_label_syncer.go`）构造 `app := models.BusinessApp{...}` 处（现第 275-283 行），补两个字段：

```go
	app := models.BusinessApp{
		UID:          bizUID,
		AppName:      appName,
		Namespace:    namespace,
		Criticality:  bizAttrs.Criticality,
		Environment:  bizAttrs.Environment,
		Team:         bizAttrs.Team,
		BusinessUnit: bizAttrs.BusinessUnit,
		OwnerName:    unstructuredObj.GetName(),
		OwnerKind:    unstructuredObj.GetKind(),
	}
```

- [ ] **Step 2: hashBusinessAttrs 覆盖 owner_name**

`hashBusinessAttrs`（现第 316-322 行）把 OwnerName/OwnerKind 加入 hash 输入，确保 owner 名变化会触发重写：

```go
func (s *BusinessLabelSyncer) hashBusinessAttrs(appName, namespace string, bizAttrs config.NamespaceMappingEntry) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		appName, namespace,
		bizAttrs.Criticality, bizAttrs.Environment, bizAttrs.Team, bizAttrs.BusinessUnit,
	}, "|")))
	return string(h[:])
}
```

改为：

```go
func (s *BusinessLabelSyncer) hashBusinessAttrs(appName, namespace, ownerName, ownerKind string, bizAttrs config.NamespaceMappingEntry) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		appName, namespace, ownerName, ownerKind,
		bizAttrs.Criticality, bizAttrs.Environment, bizAttrs.Team, bizAttrs.BusinessUnit,
	}, "|")))
	return string(h[:])
}
```

对应调用处（`syncApp` 第 285 行）同步改为：

```go
	attrsHash := s.hashBusinessAttrs(appName, namespace, unstructuredObj.GetName(), unstructuredObj.GetKind(), bizAttrs)
```

- [ ] **Step 3: batchUpsertBusinessApp 写入 owner_name/owner_kind**

`batchUpsertBusinessApp`（现第 477-509 行）的 INSERT 语句加两列：

```go
		values = append(values, fmt.Sprintf(
			`%s:(%s, %s, %s, %s, %s, %s, %s, %s, %s)`,
			strconv.Quote(item.app.UID),
			strconv.Quote(item.app.UID),
			strconv.Quote(item.app.AppName),
			strconv.Quote(item.app.Namespace),
			strconv.Quote(item.app.Criticality),
			strconv.Quote(item.app.Environment),
			strconv.Quote(item.app.Team),
			strconv.Quote(item.app.BusinessUnit),
			strconv.Quote(item.app.OwnerName),
			strconv.Quote(item.app.OwnerKind),
		))
	...
	query := fmt.Sprintf(`INSERT VERTEX BusinessApp(uid, app_name, namespace, criticality, environment, team, business_unit, owner_name, owner_kind) VALUES %s;`,
		strings.Join(values, ", "))
```

- [ ] **Step 4: 编译 + 测试**

Run: `go build ./... && go test -race -count=1 ./services/ ./services/... 2>&1 | tail -20`
Expected: build exit 0；`services` 与 `services/auth` 测试 PASS（BLS 单测若断言了 hash/INSERT 内容，需同步更新——运行后按失败提示修）。

- [ ] **Step 5: Commit**

```bash
git add services/business_label_syncer.go
git commit -m "feat(topology): BLS 写入 BusinessApp 的 owner_name/owner_kind 别名"
```

---

## Task 3: Trace caller 经 owner_name 别名解析（消除双身份）

**Files:**
- Create: `services/trace_topology_syncer_alias.go`
- Modify: `services/trace_topology_syncer.go:370-408, 526-638`
- Test: `services/trace_topology_syncer_alias_test.go`

- [ ] **Step 1: 写失败测试（lookupByOwnerName）**

`services/trace_topology_syncer_alias_test.go`：

```go
package services

import (
	"testing"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

func TestLookupByOwnerName_Hit(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) {
		// 返回 owner_name 命中的 canonical 顶点（模拟 BLS 建好的 kafka）
		return &nebula.ResultSet{}, nil
	})
	s := newTraceSyncer(db, "c1")
	// 直接测 query 内容：命中应查 owner_name 而非 app_name
	s.lookupByOwnerName("kafka-controller", "base")
	if len(db.calls) < 1 {
		t.Fatal("no query issued")
	}
	q := db.calls[0]
	if !sc(q, "owner_name ==") {
		t.Errorf("owner lookup should filter owner_name, got %q", q)
	}
	if !sc(q, "kafka-controller") {
		t.Errorf("query missing owner name, got %q", q)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./services/ -run TestLookupByOwnerName_Hit -count=1 -v`
Expected: 编译失败（`lookupByOwnerName` 未定义）。

- [ ] **Step 3: 实现 lookupByOwnerName + parseK8sFQDN + extractPodNameFromAddress**

新建 `services/trace_topology_syncer_alias.go`：

```go
package services

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// stsPodPattern matches StatefulSet pod names like "kafka-0", "kafka-controller-0".
var stsPodPattern = regexp.MustCompile(`^(.+)-\d+$`)

// parseK8sFQDN extracts (appName, namespace) from a K8s DNS name.
//   - namespace = the label immediately preceding "svc" (kafka-0.kafka-headless.base.svc -> "base")
//   - StatefulSet pod names strip their ordinal suffix (kafka-0 -> "kafka")
//   - legacy two-segment names without "svc" fall back to service.namespace
func parseK8sFQDN(host string) (appName, namespace string) {
	labels := strings.Split(host, ".")
	if len(labels) == 0 || labels[0] == "" {
		return "", ""
	}

	for i := 1; i < len(labels); i++ {
		if labels[i] == "svc" && i > 0 {
			namespace = labels[i-1]
			break
		}
	}
	if namespace == "" && len(labels) > 1 {
		namespace = labels[1]
	}

	first := labels[0]
	if m := stsPodPattern.FindStringSubmatch(first); m != nil {
		return m[1], namespace
	}
	return first, namespace
}

// extractPodNameFromAddress returns the pod name if rawPeer is a K8s pod FQDN
// (e.g. "kafka-controller-0.kafka-controller-headless.base.svc.cluster.local"
//  -> "kafka-controller-0"), otherwise "".
func extractPodNameFromAddress(rawPeer string) string {
	host := rawPeer
	if idx := strings.LastIndex(rawPeer, ":"); idx > 0 {
		host = rawPeer[:idx]
	}
	if net.ParseIP(host) != nil {
		return ""
	}
	labels := strings.Split(host, ".")
	if len(labels) == 0 {
		return ""
	}
	if stsPodPattern.MatchString(labels[0]) {
		return labels[0]
	}
	return ""
}

// lookupByOwnerName resolves a BusinessApp by its workload owner name + namespace.
// It returns the canonical app (uid + app_name) that BLS created from labels.
func (s *TraceTopologySyncer) lookupByOwnerName(ownerName, namespace string) (businessAppRef, bool) {
	if ownerName == "" || namespace == "" {
		return businessAppRef{}, false
	}
	cacheKey := "bizowner:" + namespace + ":" + ownerName
	if cached, err := s.relationCache.Get(cacheKey); err == nil {
		parts := strings.SplitN(string(cached), "|", 2)
		if len(parts) == 2 {
			s.logger.Debug("OWNER_LOOKUP cache hit", zap.String("owner", ownerName), zap.String("app", parts[1]))
			return businessAppRef{uid: parts[0], appName: parts[1], namespace: namespace}, true
		}
	}

	query := fmt.Sprintf(
		`MATCH (v:BusinessApp) WHERE v.BusinessApp.owner_name == %s AND v.BusinessApp.namespace == %s RETURN v.BusinessApp.uid as uid, v.BusinessApp.app_name as app_name LIMIT 1`,
		strconv.Quote(ownerName), strconv.Quote(namespace))
	resultSet, err := s.graphDB.ExecuteAndCheck(query)
	if err != nil || resultSet == nil || resultSet.GetRowSize() == 0 {
		return businessAppRef{}, false
	}
	row, rowErr := resultSet.GetRowValuesByIndex(0)
	if rowErr != nil {
		return businessAppRef{}, false
	}
	uidV, e1 := row.GetValueByColName("uid")
	appV, e2 := row.GetValueByColName("app_name")
	if e1 != nil || e2 != nil {
		return businessAppRef{}, false
	}
	uid, _ := uidV.AsString()
	app, _ := appV.AsString()
	if uid == "" || app == "" {
		return businessAppRef{}, false
	}
	_ = s.relationCache.Set(cacheKey, []byte(uid+"|"+app))
	return businessAppRef{uid: uid, appName: app, namespace: namespace}, true
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./services/ -run TestLookupByOwnerName_Hit -count=1 -v`
Expected: PASS。

- [ ] **Step 5: 改造 resolveCallerBusinessApp 走别名**

`services/trace_topology_syncer.go` 的 `resolveCallerBusinessApp`（现第 370-408 行），在得到 `appName`（owner 名）与 `namespace` 之后、`lookupOrInsertBusinessApp` 之前插入别名查找：

```go
	namespace := extractResourceAttr(rs, "k8s.namespace.name")

	// 先按 owner 名反查 canonical 顶点（BLS 已用 label 建好），消除双身份
	if ref, ok := s.lookupByOwnerName(appName, namespace); ok {
		s.logger.Debug("CALLER_RESOLVE resolved via owner_name alias",
			zap.String("owner_name", appName),
			zap.String("canonical_app", ref.appName))
		ref.podName = extractResourceAttr(rs, "k8s.pod.name")
		return ref, true
	}

	uid, ok := s.lookupOrInsertBusinessApp(appName, namespace, appName)
	if !ok {
		...
		return businessAppRef{}, false
	}
	return businessAppRef{uid: uid, appName: appName, namespace: namespace, podName: extractResourceAttr(rs, "k8s.pod.name")}, true
```

注意：`lookupOrInsertBusinessApp` 签名从 2 参改为 3 参（第三个参数 `ownerName`，peer 路径传空串），见 Task 5 一并处理。

- [ ] **Step 6: Commit**

```bash
git add services/trace_topology_syncer.go services/trace_topology_syncer_alias.go services/trace_topology_syncer_alias_test.go
git commit -m "feat(topology): trace caller 经 owner_name 别名解析，消除 BusinessApp 双身份"
```

---

## Task 4: 修 resolvePeerFromAddress 的 FQDN 解析

**Files:**
- Modify: `services/trace_topology_syncer.go:486-524`
- Test: `services/trace_topology_syncer_alias_test.go`

- [ ] **Step 1: 写失败测试**

`services/trace_topology_syncer_alias_test.go` 追加：

```go
func TestParseK8sFQDN_STSPod(t *testing.T) {
	app, ns := parseK8sFQDN("kafka-0.kafka-headless.base.svc.cluster.local")
	assertEq(t, "kafka", app, "appName strips ordinal")
	assertEq(t, "base", ns, "namespace is label before svc")
}

func TestParseK8sFQDN_RegularService(t *testing.T) {
	app, ns := parseK8sFQDN("kafka.messaging.svc.cluster.local")
	assertEq(t, "kafka", app, "appName")
	assertEq(t, "messaging", ns, "namespace")
}

func TestParseK8sFQDN_TwoSegLegacy(t *testing.T) {
	app, ns := parseK8sFQDN("mysql.staging")
	assertEq(t, "mysql", app, "appName")
	assertEq(t, "staging", ns, "namespace")
}

func TestExtractPodNameFromAddress_STS(t *testing.T) {
	assertEq(t, "kafka-controller-0",
		extractPodNameFromAddress("kafka-controller-0.kafka-controller-headless.base.svc.cluster.local"),
		"pod name")
}

func TestExtractPodNameFromAddress_Service(t *testing.T) {
	if p := extractPodNameFromAddress("kafka.base.svc.cluster.local"); p != "" {
		t.Errorf("service FQDN should yield empty pod name, got %q", p)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./services/ -run 'TestParseK8sFQDN|TestExtractPodName' -count=1 -v`
Expected: 编译失败（函数未定义）。

- [ ] **Step 3: 让 resolvePeerFromAddress 改用 parseK8sFQDN**

`resolvePeerFromAddress` 现第 486-524 行，保留 knownServices/IP 逻辑，把末尾两段式解析替换为：

```go
func (s *TraceTopologySyncer) resolvePeerFromAddress(rawPeer string) (appName, namespace string) {
	if entry, ok := s.knownServices[rawPeer]; ok {
		return entry.Name, entry.Namespace
	}

	host := rawPeer
	if idx := strings.LastIndex(rawPeer, ":"); idx > 0 {
		stripped := rawPeer[:idx]
		if entry, ok := s.knownServices[stripped]; ok {
			return entry.Name, entry.Namespace
		}
		if net.ParseIP(stripped) != nil {
			return "", ""
		}
		host = stripped
	}

	if net.ParseIP(host) != nil {
		if entry, ok := s.knownServices[host]; ok {
			return entry.Name, entry.Namespace
		}
		return "", ""
	}

	return parseK8sFQDN(host)
}
```

- [ ] **Step 4: 运行全部相关测试**

Run: `go test ./services/ -run 'TestParsePeerServiceName|TestParseK8sFQDN|TestExtractPodName|TestResolvePeer' -count=1 -v`
Expected: 既有 `TestParsePeerServiceName_*` 与新增测试全部 PASS（parseK8sFQDN 保持了 legacy 两段式语义）。

- [ ] **Step 5: Commit**

```bash
git add services/trace_topology_syncer.go services/trace_topology_syncer_alias_test.go
git commit -m "fix(topology): 修正 resolvePeerFromAddress 对 K8s FQDN/STS pod 名的解析"
```

---

## Task 5: self-call 过滤收紧为「同 pod 才丢」

**Files:**
- Modify: `services/trace_topology_syncer.go`（businessAppRef、resolveCaller/resolvePeer、过滤逻辑、lookupOrInsertBusinessApp 签名）
- Test: `services/trace_topology_syncer_test.go:374-391`（更新）

- [ ] **Step 1: businessAppRef 增加 podName 字段**

`services/trace_topology_syncer.go:58-62`：

```go
type businessAppRef struct {
	uid       string
	appName   string
	namespace string
	podName   string
}
```

- [ ] **Step 2: resolvePeerBusinessApp 提取 callee pod 名**

`resolvePeerBusinessApp` 返回处（现第 483 行）加 `podName: extractPodNameFromAddress(rawPeer)`：

```go
	return businessAppRef{uid: uid, appName: appName, namespace: namespace, podName: extractPodNameFromAddress(rawPeer)}, true
```

（resolveCaller 的 podName 已在 Task 3 Step 5 里通过 `extractResourceAttr(rs, "k8s.pod.name")` 填充。）

- [ ] **Step 3: 改过滤逻辑**

`processSpanRecord` 现第 342-348 行：

```go
				if caller.uid == callee.uid && caller.podName != "" && caller.podName == callee.podName {
					atomic.AddInt64(&s.droppedSelfCall, 1)
					s.logger.Debug("TRACE_PROCESS Same-pod self-call filtered",
						zap.String("uid", caller.uid),
						zap.String("app_name", caller.appName),
						zap.String("pod", caller.podName))
					continue
				}
```

（语义变化：同 workload 不同副本（kafka-0→kafka-1）不再被丢，产出 self-loop 边；只有确认同 pod 才丢。）

- [ ] **Step 4: lookupOrInsertBusinessApp 增加 ownerName 参数**

签名从 `(appName, namespace string)` 改为 `(appName, namespace, ownerName string)`；自动建点的 INSERT 补两列：

```go
	insertQuery := fmt.Sprintf(
		`INSERT VERTEX BusinessApp(uid, app_name, namespace, criticality, environment, team, business_unit, owner_name, owner_kind) VALUES %s:(%s, %s, %s, %s, %s, %s, %s, %s, %s);`,
		strconv.Quote(uid),
		strconv.Quote(uid),
		strconv.Quote(appName),
		strconv.Quote(namespace),
		strconv.Quote("medium"),
		strconv.Quote(""),
		strconv.Quote(""),
		strconv.Quote(""),
		strconv.Quote(ownerName),
		strconv.Quote(""),
	)
```

（caller 路径传 owner 名，peer 路径传空串。所有既有 `lookupOrInsertBusinessApp("x","y")` 调用点补第三参。）

- [ ] **Step 5: 更新 self-call 测试**

`services/trace_topology_syncer_test.go:374-391` 的 `TestProcessSpan_SelfCallFiltered` 改为「同 pod 才滤」，并新增「跨副本保留」用例：

```go
func TestProcessSpan_SamePodSelfCallFiltered(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "kafka"),
		sa("k8s.namespace.name", "messaging"),
		sa("k8s.pod.name", "kafka-0"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "kafka-0.messaging.svc.cluster.local")}, v1.Span_SPAN_KIND_CLIENT),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 0 {
		t.Errorf("same-pod self-call should be filtered, got %d edges", n)
	}
}

func TestProcessSpan_CrossReplicaSameWorkloadKept(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newTraceSyncer(db, "")
	rs := tpRes([]*commonv1.KeyValue{
		sa("k8s.owner.name", "kafka"),
		sa("k8s.namespace.name", "messaging"),
		sa("k8s.pod.name", "kafka-0"),
	})
	rs.ScopeSpans = []*v1.ScopeSpans{{Spans: []*v1.Span{
		tpSpan([]*commonv1.KeyValue{sa("peer.service", "kafka-1.messaging.svc.cluster.local")}, v1.Span_SPAN_KIND_CLIENT),
	}}}
	s.processSpanRecord(&interfaces.Message{Value: tpMarshal(rs)})
	s.bufferMu.Lock()
	n := len(s.edgeBuffer)
	s.bufferMu.Unlock()
	if n != 1 {
		t.Fatalf("cross-replica same-workload should keep self-loop edge, got %d", n)
	}
}
```

- [ ] **Step 6: 全量测试 + race**

Run: `go test -race -count=1 ./services/ ./services/auth/... ./config/... 2>&1 | tail -20`
Expected: 全 PASS。

- [ ] **Step 7: Commit**

```bash
git add services/trace_topology_syncer.go services/trace_topology_syncer_test.go
git commit -m "fix(topology): self-call 过滤收紧为同 pod 才丢，保留有状态副本互连"
```

---

## Task 6: 一次性合并重复 BusinessApp 顶点

**Files:**
- Create: `cmd/mergebizapp/main.go`

合并策略：找出 `app_name` 等于另一顶点 `owner_name`（且 namespace 相同）的重复对，把「无业务属性的 trace 自动建顶点」上的 CallsApp/BelongsToApp 边迁到「BLS 建的 canonical 顶点」上，再删除重复顶点。幂等，可反复跑。

- [ ] **Step 1: 写维护命令骨架**

`cmd/mergebizapp/main.go`（复用 resetadmin 的单文件 cmd 模式，用 nebula-go 直连，读 `--addr`/`--user`/`--pass`）：

```go
package main

import (
	"flag"
	"fmt"
	"log"

	nebula "github.com/vesoft-inc/nebula-go/v3"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9669", "nebula graphd addr")
	user := flag.String("user", "root", "nebula user")
	pass := flag.String("pass", "nebula", "nebula password")
	space := flag.String("space", "mutong", "nebula space")
	flag.Parse()

	pool, err := nebula.NewConnectionPool([]nebula.HostAddress{{Host: hostOf(*addr), Port: portOf(*addr)}}, nebula.GetDefaultConf(), nebula.DefaultLogger{})
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	sess, err := pool.GetSession(*user, *pass)
	if err != nil {
		log.Fatalf("session: %v", err)
	}
	defer sess.Release()

	_ = *space
	// 见 Step 2 的合并逻辑
	fmt.Println("merge done")
}
```

（`hostOf`/`portOf` 用 `strings.Split` 解析 addr，`net.ParseIP` 兜底——具体实现照 resetadmin 的既有写法。）

- [ ] **Step 2: 合并核心逻辑**

在骨架里补：先查所有「owner_name 非空且 app_name != owner_name」的 canonical 顶点；再对每个 canonical 顶点查同 namespace 下 `app_name == owner_name` 的重复顶点；把重复顶点上的入/出 CallsApp、BelongsToApp 边迁到 canonical（DELETE EDGE + INSERT EDGE），最后 DELETE VERTEX 重复顶点。

```go
	// 1) canonical 顶点：有 owner_name 且 app_name != owner_name
	canon := `MATCH (v:BusinessApp) WHERE v.BusinessApp.owner_name != "" AND v.BusinessApp.app_name != v.BusinessApp.owner_name RETURN v.BusinessApp.uid as uid, v.BusinessApp.owner_name as owner, v.BusinessApp.namespace as ns`
	// 2) 对每个 (owner, ns)：找重复顶点
	dup := `MATCH (v:BusinessApp) WHERE v.BusinessApp.app_name == "%s" AND v.BusinessApp.namespace == "%s" AND v.BusinessApp.uid != "%s" RETURN v.BusinessApp.uid as uid`
	// 3) 迁移边 + 删点
	rebind := `GO FROM "%s" OVER CallsApp BIDIRECT YIELD edge AS e | ...` // 先查再 DELETE EDGE / INSERT EDGE 到 canonical
```

> 注：Nebula 无「改边端点」语句，迁移需「查出边 → DELETE EDGE → INSERT EDGE 到 canonical」。本步给出 SQL 骨架与执行顺序，落地时用 nebula-go 的 Execute 逐条执行并打日志（每迁移一个重复点打 Info）。合并只处理「app_name == 另一顶点 owner_name」的明确重复，不碰未知语义的顶点。

- [ ] **Step 3: 编译 + 干跑**

Run: `go build ./cmd/mergebizapp && go run ./cmd/mergebizapp -addr 10.220.71.198:9669 -pass <nebula-pass> -dry-run`
Expected: 打印待合并的重复对列表，不写库。

> 建议先加 `-dry-run` 只打印不动库，人工核对后再真正执行（生产图库，务必先备份/确认）。

- [ ] **Step 4: Commit**

```bash
git add cmd/mergebizapp/main.go
git commit -m "feat(topology): 一次性合并重复 BusinessApp 顶点的维护命令"
```

---

## Task 7: 修 matchBusinessAppByName 的 is_deleted 假条件

**Files:**
- Modify: `services/business_topology_service.go:216-222`

- [ ] **Step 1: 移除不存在的属性条件**

`matchBusinessAppByName` 的查询里 `a.BusinessApp.is_deleted == false` 引用了一个 BusinessApp tag 上不存在的属性（BusinessApp 无 is_deleted），导致查询恒失败、告警富化第一步静默跳过。改为：

```go
	query := fmt.Sprintf(`
		MATCH (a:BusinessApp) WHERE a.BusinessApp.app_name == %s AND a.BusinessApp.namespace == %s
		RETURN a.BusinessApp.uid as uid, a.BusinessApp.app_name as app_name, a.BusinessApp.namespace as namespace, a.BusinessApp.criticality as criticality, a.BusinessApp.environment as environment, a.BusinessApp.team as team, a.BusinessApp.business_unit as business_unit LIMIT 1`,
		strconv.Quote(appName), strconv.Quote(namespace))
```

- [ ] **Step 2: 编译 + 测试**

Run: `go build ./... && go test -race -count=1 ./services/... 2>&1 | tail -10`
Expected: PASS。

- [ ] **Step 3: Commit**

```bash
git add services/business_topology_service.go
git commit -m "fix(topology): 移除 matchBusinessAppByName 对不存在属性 is_deleted 的过滤"
```

---

## Task 8: 消费 Beyla L4 flow 指标补 workload 级连接盲区（决策已定稿）

**决策定稿**：粒度 = **workload 级**；边模型 = **复用 CallsApp（选项 A）**。Beyla 零改动（用默认 owner 级维度），flow 按 `owner.name` 上卷到 BusinessApp 写 `BusinessApp → BusinessApp` 的 CallsApp 边。pod 级（副本间明细）不做静态建模——副本级诊断由 LLM 经物理拓扑 + MCP 实时查询覆盖（见 Task 9 评估结论）。

**两个关键事实（已查证，纠正早期判断）：**
1. OTel Collector contrib 的 `kafkaexporter` **支持 metrics**（beta 稳定度，encoding `otlp_proto`/`otlp_json`，默认 topic `otlp_metrics`）——之前"kafka exporter 不支持 metrics"的说法有误，L4 flow 走 Kafka 完全可行，无需绕 Prometheus。
2. 线上 Beyla 配置里的 `flow_export_interval` 是**无效键**（v1.9/v2.8/v3.33 源码均无此字段），正确是 `network.cache_active_timeout`（默认 5s）——落地时顺手修正线上配置。

**数据流**：
```
Beyla (eBPF, network.enable + otel_metrics_export → collector:4318)
  → OTel Collector (otlp receiver → batch → kafka/metrics exporter, topic=mutong_flow_01)
  → Kafka (mutong_flow_01: OTLP MetricsData protobuf)
  → mutong FlowTopologySyncer (解析 metrics → src/dst owner.name → lookupByOwnerName 上卷 → 写 CallsApp 边)
```

**Files:**
- Modify: `deploy/helm-beyla-values.yaml`（加 metrics export + 修正无效键）
- Modify: `deploy/helm-otelcol-values.yaml`（加 metrics pipeline + 对齐 trace topic）
- Modify: `config/config_base.go`（KafkaConf 加 FlowTopic/FlowGroup/FlowClient）
- Modify: `config/config.go`（SetupFlowKafkaClient + GetFlowMessageQueue）
- Create: `services/flow_topology_syncer.go`
- Create: `services/flow_topology_syncer_test.go`
- Modify: `cmd/main.go`（装配 flowSyncer）
- Modify: `configs/config.core.yaml`（flowTopic/flowGroup）

**零依赖新增**：`go.opentelemetry.io/proto/otlp v1.10.0` 已在 go.mod，含 `metrics/v1` 子包，直接 import。

- [ ] **Step 1: 写失败测试（flow 指标解析）**

`services/flow_topology_syncer_test.go`（复用 trace 测试的 `traceGraphDB`/`newTraceSyncer` 等辅助模式）：

```go
package services

import (
	"testing"

	mtr "go.opentelemetry.io/proto/otlp/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestProcessFlow_SameWorkloadCrossReplica(t *testing.T) {
	db := newDB(func(q string) (*nebula.ResultSet, error) { return nil, nil })
	s := newFlowSyncer(db, "")
	// 构造一个 flow Sum 指标：kafka-controller-0 → kafka-controller-1
	dp := &mtr.NumberDataPoint{Attributes: []*commonv1.KeyValue{
		sa("k8s.src.owner.name", "kafka-controller"),
		sa("k8s.src.namespace", "base"),
		sa("k8s.dst.owner.name", "kafka-controller"),
		sa("k8s.dst.namespace", "base"),
	}}
	metric := &mtr.Metric{Name: "beyla.network.flow.bytes", Data: &mtr.Metric_Sum{Sum: &mtr.Sum{DataPoints: []*mtr.NumberDataPoint{dp}}}}
	md := &mtr.MetricsData{ResourceMetrics: []*mtr.ResourceMetrics{{ScopeMetrics: []*mtr.ScopeMetrics{{Metrics: []*mtr.Metric{metric}}}}}}
	b, _ := proto.Marshal(md)
	s.processMetricsRecord(&interfaces.Message{Value: b})
	if len(s.edgeBuffer) != 1 {
		t.Fatalf("want 1 flow edge, got %d", len(s.edgeBuffer))
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./services/ -run TestProcessFlow -count=1 -v`
Expected: 编译失败（`newFlowSyncer`/`processMetricsRecord` 未定义）。

- [ ] **Step 3: 实现 FlowTopologySyncer**

`services/flow_topology_syncer.go`（复用 TraceTopologySyncer 的 `lookupByOwnerName`、`upsertCallEdge`、`businessAppRef`）：

```go
package services

import (
	"context"
	"sync"
	"time"

	"gitee.com/tddh/mutong/interfaces"
	mtr "go.opentelemetry.io/proto/otlp/metrics/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

type FlowTopologySyncer struct {
	logger     interfaces.Logger
	graphDB    interfaces.GraphDB
	messageQueue interfaces.MessageQueue
	clusterName string
	// 复用 TraceTopologySyncer 的解析/上卷/写边能力
	resolver   *TraceTopologySyncer
	edgeBuffer []pendingCallEdge
	bufferMu   sync.Mutex
	stopCh     chan struct{}
}

// flowMetricNames 是 Beyla L4 flow 指标名（OTel 点分隔 + Prometheus 下划线两种形态）
var flowMetricNames = map[string]bool{
	"beyla.network.flow.bytes":        true,
	"beyla_network_flow_bytes_total":  true,
}

func (s *FlowTopologySyncer) processMetricsRecord(msg *interfaces.Message) bool {
	data := &mtr.MetricsData{}
	if err := proto.Unmarshal(msg.Value, data); err != nil {
		s.logger.Error("flow: unmarshal MetricsData failed", err)
		return false
	}
	for _, rm := range data.GetResourceMetrics() {
		for _, sm := range rm.GetScopeMetrics() {
			for _, metric := range sm.GetMetrics() {
				if !flowMetricNames[metric.GetName()] {
					continue
				}
				for _, dp := range metric.GetSum().GetDataPoints() {
					s.handleFlowPoint(dp.GetAttributes())
				}
			}
		}
	}
	return true
}

func (s *FlowTopologySyncer) handleFlowPoint(attrs []*commonv1.KeyValue) {
	srcOwner := flowAttr(attrs, "k8s.src.owner.name", "source.workload.name", "src_owner_name")
	srcNs := flowAttr(attrs, "k8s.src.namespace", "source.namespace", "src_namespace")
	dstOwner := flowAttr(attrs, "k8s.dst.owner.name", "destination.workload.name", "dst_owner_name")
	dstNs := flowAttr(attrs, "k8s.dst.namespace", "destination.namespace", "dst_namespace")

	caller, ok1 := s.resolver.lookupByOwnerName(srcOwner, srcNs)
	callee, ok2 := s.resolver.lookupByOwnerName(dstOwner, dstNs)
	if !ok1 || !ok2 {
		return // 无 owner 或未建 BusinessApp 的流量，跳过
	}
	s.resolver.upsertCallEdge(caller, callee)
}

func flowAttr(attrs []*commonv1.KeyValue, keys ...string) string {
	for _, a := range attrs {
		for _, k := range keys {
			if a.GetKey() == k {
				return a.GetValue().GetStringValue()
			}
		}
	}
	return ""
}
```

（`Start/Stop/consumeLoop/flushLoop` 照抄 `TraceTopologySyncer` 的模式；`handleFlowPoint` 里的多属性名 fallback 覆盖 OTel 语义/新老版本差异，落地时抓真实数据点确认后收敛为一个。）

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./services/ -run TestProcessFlow -count=1 -v`
Expected: PASS（`lookupByOwnerName` 走 cache miss → 自动建点，产出 1 条自环边）。

- [ ] **Step 5: 配置层 + 装配**

照 `SetupTraceKafkaClient` 三件套加第四套：`config_base.go` 的 KafkaConf 加 `FlowTopic/FlowGroup/FlowClient`；`config.go` 加 `SetupFlowKafkaClient()`（消费 `FlowTopic`、group `FlowGroup`）与 `GetFlowMessageQueue()`；`main.go` 在 `cfg.BusinessTopology.Enabled` 分支照 traceSyncer 装配 flowSyncer；`configs/config.core.yaml` 加 `flowTopic: mutong_flow_01` / `flowGroup: mutong_flow_v01`。

- [ ] **Step 6: deploy 侧配置**

`deploy/helm-beyla-values.yaml`：

```yaml
config:
  discovery:
    services:
      - k8s_namespace: ".*"
        exports: [traces, metrics]
  network:
    enable: true
    cache_active_timeout: 5s            # 修正原 flow_export_interval 无效键
  otel_metrics_export:
    endpoint: "http://$(OTEL_COLLECTOR_HOST):4318"
```

`deploy/helm-otelcol-values.yaml`：

```yaml
exporters:
  kafka/traces:
    brokers: ["kafka:9092"]
    encoding: otlp_proto
    topic: mutong_trace_01          # 对齐 mutong 消费的 topic（原 otlp_spans 与消费端不一致）
  kafka/metrics:
    brokers: ["kafka:9092"]
    encoding: otlp_proto
    topic: mutong_flow_01
service:
  pipelines:
    traces:  {receivers: [otlp], processors: [batch], exporters: [kafka/traces]}
    metrics: {receivers: [otlp], processors: [batch], exporters: [kafka/metrics]}
```

- [ ] **Step 7: 全量测试 + race**

Run: `go test -race -count=1 ./services/ ./config/... 2>&1 | tail -20`
Expected: PASS。

- [ ] **Step 8: Commit**

```bash
git add services/flow_topology_syncer.go services/flow_topology_syncer_test.go config/config_base.go config/config.go cmd/main.go configs/config.core.yaml deploy/helm-beyla-values.yaml deploy/helm-otelcol-values.yaml
git commit -m "feat(topology): FlowTopologySyncer 消费 Beyla L4 flow 指标补 workload 级连接边"
```

**对现有设计的影响**：纯增量、正交。复用 `interfaces.MessageQueue`/`KafkaAdapter`（零改动）、复用 BusinessApp 顶点与 `BelongsToApp`（零改动）、复用 `lookupByOwnerName`（依赖 Task 3）。唯一跨任务依赖：**flow 上卷需要 Task 3 的 owner_name 别名**，故 Task 8 必须在 Task 3 之后执行。schema 无改动（复用 CallsApp）。

---

## Task 9（已评估，决定不做）: 副本级静态建模

**结论：不做。** 副本级静态建模（Replica 顶点 + ReplicaConnects 边 + role 属性）经评估属于过度设计。

**理由（平台定位视角）：** mutong 是 LLM 的「上下文供给层」，LLM 是分析主体。对于 kafka 这类有状态中间件，LLM 诊断副本级问题时三条已有路径即可闭环，无需静态建模：
1. **物理拓扑推断集群**：K8sResource 图已有 `kafka-controller-0/1/2` Pod + `OwnedBy` 边指向同一 StatefulSet，LLM 直接推断出集群结构。
2. **MCP 实时查状态**：`get_pod_logs`/`inspect_resource`/`get_resource_metrics`/`list_resources_from_graph` 本就是给 LLM 按需调用的，副本的实时日志/状态/指标动态查即可。
3. **告警 label 带副本键**：告警已带 `pod`/`instance` 标签，LLM 直接定位具体副本。

副本间关系（谁是 leader、ISR 有哪些、谁掉队）是**运行时状态**，静态建模会过时；应由告警 label 带进来或 LLM 实时查，不预先固化。

**保留的证据**：Task 5 的 self-call 收紧让 `kafka → kafka` 自环边保留，这条边本身就是「该 app 内部有副本互访（集群/复制）」的静态证据，配合物理拓扑的 Pod 集合，LLM 足以判断集群组合。

---

## Self-Review 记录

1. **Spec 覆盖**：P0 五项（双身份、DNS 解析、self-call、合并、is_deleted 假条件）+ 增强项 Task 8（L4 flow 消费）均有对应 Task；Task 9（副本级静态建模）经评估决定不做（LLM 经物理拓扑 + MCP 实时查询已覆盖），已记录理由。
2. **Placeholder 扫描**：Task 6 的迁移 SQL 骨架明确标注了「查边→删边→插边」执行顺序与幂等要求，无 TBD；Task 8 已展开为含代码、配置、测试、commit 的完整步骤。
3. **类型一致性**：`businessAppRef.podName` 在 Task 3 与 Task 5 中字段名一致；`lookupOrInsertBusinessApp(appName, namespace, ownerName)` 三参签名在 Task 3/5 统一；`owner_name`/`owner_kind` 属性在 schema/models/BLS/trace 四处一致。
4. **验证依赖**：Task 1 的 ALTER TAG 必须在 Task 2/3 上线前对生产库执行，否则 INSERT 带 owner_name 会失败——已在 Task 1 Step 3 标注，执行时务必先做。

---

## 落地顺序与风险

**执行顺序**：Task 1（schema+migration）→ Task 2（BLS 写别名）→ Task 3（trace 走别名）→ Task 4（DNS 修复）→ Task 5（self-call）→ Task 6（合并存量）→ Task 7（is_deleted）→ Task 8（L4 flow 消费，依赖 Task 3 的 owner_name 别名）。

**关键风险**：
- Task 1 的 `ALTER TAG` 是生产库结构变更，必须先在测试库/备份验证。
- Task 6 合并会 DELETE VERTEX，先 `-dry-run` 核对，合并前备份。
- Task 5 改动 self-call 语义后，存量 `kafka→kafka` self-loop 边会开始出现——这是「集群内部副本互访」的预期证据（LLM 据此判断集群组合），非 bug；前端 force-graph 对自环的渲染可能不显眼，可视化需前端另补，属后续增强。
- Task 8 上线前需先确认线上 Beyla 版本（v2 用 `network.enable`、v3 用 `metrics.features`），并抓一个真实 flow 数据点确认 src/dst 属性名；`otel_metrics_export` 与 metrics pipeline 属基础设施变更，需在测试环境先行验证。

**每个 Task 独立可交付**：Task 1-8 各自 build+test 通过即可 commit，不依赖后续 Task 才能跑起来（Task 8 依赖 Task 3 已合入）。

---

## 增量：业务顶点来源可追溯（2026-09-03 已实现并部署）

**动机**：业务拓扑里点开一个 `BusinessApp` 顶点，只看得到 appName/团队/业务单元/关键等级/环境，**看不出它到底由哪个 K8s 资源创建**，也分不清是「真实资源建模」还是「链路推断出来的幽灵身份」。生产实测：145 个业务顶点中，仅 59 个有真实资源经 `BelongsToApp` 指向（资源建模），其余 **86 个（60%）是 trace 自动建出的幽灵点**（`owner_name` 为 NULL、无任何成员），如 `ingress-nginx-controller`、`bizapp-cluster1--:`、`juicefs-worker-02-…`、typo 的 `dingofgofs`。

**关键设计取舍**：`owner_name/owner_kind` 是「最后写入者胜出」的单值，不可靠（实测 percona-postgresql 的 owner 被临时备份 `Job` 抢占、nebula-graph 的 owner 是 `metad` 而非主体）。因此**权威的「创建来源」= `BelongsToApp` 入边反查的成员集合**，归属工作负载从成员里推导（优先未删除的控制器级），`owner_name` 仅作快速提示。

**实现要点**：
- **A 详情端点**：`BusinessTopologyService.GetAppDetail(uid)` + `GET /api/v1/business-topology/app?uid=`。返回顶点属性 + `BelongsToApp` 反查的成员资源（kind/name/ns/uid/is_deleted/isController）+ 从成员推导的 `primaryWorkload` + `provenance`（有成员=`label` / 无=`trace`）。前端点击业务顶点懒加载，详情面板新增「🏢 业务应用」区块：来源徽标、归属负载、可点击跳转物理拓扑并选中的成员清单。
- **A3 图面区分**：`GetApps/GetGraph` 节点增加 `ownerName/ownerKind/provenance`（一次性查出有成员的顶点集合判定，不逐点查）；force-graph 对 trace 幽灵点用**灰色虚线**绘制、子标签显示归属工作负载类型（如 Deployment）或「链路推断」，不点开即可分辨真假。
- **B owner 控制器优先**：`business_label_syncer.syncApp` 仅当资源是 `Deployment/StatefulSet/DaemonSet/CronJob` 时才写 `owner_name/owner_kind`；`batchUpsertBusinessApp` 分区写入——控制器级用 `INSERT VERTEX`（覆盖、写 owner），非控制器级用 `INSERT VERTEX IF NOT EXISTS`（仅在顶点缺失时创建，**绝不覆盖已有 owner**，规避 Nebula INSERT 整点覆盖会清空 owner 的坑）；去重拆为 baseHash/ownerHash 双键，避免 Pod 等非控制器资源反复触发写。
- **C 计数修复**：`cmd/mergebizapp` 的 dry-run 分支此前不自增计数器，结尾恒显示「0 merges」，与实际列出的待合并条目不符；已修复。

**验证（tf001 生产实例，经真实 HTTP 面）**：
- `coredns` → `provenance=label`、`primary=Deployment/coredns`、成员 1 条且 `isController=true`
- 幽灵点 `ingress-nginx-controller` → `provenance=trace`、`memberCount=0`、owner 空
- `percona-postgresql` → 诚实反映成员就是 `Job/pg-c1-pg-db-backup-7mxt`
- `/graph` → 145 节点，**59 label / 86 trace**，与图库 `LOOKUP`/`MATCH` 实测一致
- 探针：空 uid→500 干净错误不 panic；不存在/异类 uid→200 优雅空；注入 `uid=x") DELETE VERTEX "…"` 被 `strconv.Quote` 中和、复核 coredns 顶点未被误删
- B 的 `INSERT VERTEX IF NOT EXISTS` 语法经真实 Nebula 验证有效，启动后 BLS 无插入错误

**遗留（诚实记录，未在本次处理）**：
- 86 个幽灵点中，`cmd/mergebizapp -dry-run` 仅识别出 **4 个身份明确、可安全合并**的重复对（metallb-speaker→metallb、vm-…-vmselect→victoria-metrics-cluster、radpanda-connect-…→redpanda-connect、nebula-operator-scheduler-deployment→nebula-operator）。该合并是破坏性操作（迁边 + DELETE VERTEX），**已 dry-run 就绪但暂未执行**，需人工确认。
- 其余 ~82 个幽灵点**不能一刀切删**：含真实外部依赖（`googleapis-storage`/`gravatar-secure`/`com-grafana`，是真实出向流量）与合法链路-only 组件。其根因是 **trace 解析器**——ns 未解析（22 个 `bizapp-cluster1--*`）、Pod 序号未上卷（`redpanda-0/1/2`、`vmagent-0/1/2`、`nebula-storaged-0/1/2` 本应上卷到 workload）、FQDN 字符损坏（`dingofs` 的 ~15 个变体）——属 Task 4/5 范畴，需单独评估，不在「丰富顶点信息」内。

**相关 commit**：`457fd2f`（A/A3）、`86f6348`（B）、`de4e47d`（C 计数修复）。
