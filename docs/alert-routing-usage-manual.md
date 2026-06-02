# 告警路由与拓扑关联——使用手册

> 版本：v2.0 | 分支：`feature/alert-routing-topology` | 日期：2026-05-27

## 目录

1. [架构概览](#一架构概览)
2. [配置指南](#二配置指南)
3. [告警处理流程](#三告警处理流程)
4. [Stakeholder 推导](#四stakeholder-推导)
5. [抑制规则](#五抑制规则)
6. [巡检系统](#六巡检系统)
7. [完整示例](#七完整示例)
8. [故障排查](#八故障排查)

---

## 一、架构概览

### 1.1 系统边界

```
Prometheus              Alertmanager             mutong
┌──────────┐          ┌──────────────┐        ┌──────────────────────────┐
│ 告警规则   │  触发    │ 分组/去重      │ webhook│ ① 拓扑富化（Enricher）      │
│ 阈值评估   │ ──────→ │ 标签匹配      │ ─────→│ ② 抑制检查（Suppressor）    │
│ firing    │          │ 路由到webhook  │        │ ③ 路由决策（Router）        │
└──────────┘          └──────────────┘        │ ④ 通知分发（Notifier）      │
                                              │ ⑤ AI 诊断（autoDiagnosis）  │
   告警的产生                                    └──────────────────────────┘
   （不归 mutong 管）                              告警的处理（归 mutong 管）
```

### 1.2 告警处理流水线

```
Alertmanager Webhook → AlertController → AlertService.Process()
    ├─ Enricher:     资源拓扑查询 → 业务上下文注入 → Stakeholder 推导
    ├─ Suppressor:   拓扑抑制 → 因果链抑制 → 声明式规则检查
    ├─ Router:       Owner 路由 → Stakeholder 路由 → severity 降级
    ├─ Storage:      PostgreSQL 持久化
    └─ Notifier:     Slack / PagerDuty / 钉钉 / 邮件
```

### 1.3 核心概念

| 概念 | 含义 | 通知方式 |
|------|------|---------|
| **Owner** | 负责修复告警的团队 | PagerDuty + Slack，需立即处理 |
| **Stakeholder** | 受告警影响的业务方团队 | Slack/钉钉，需关注但不需处理 |
| **Inhibition** | 根因告警抑制衍生告警 | 记录但不通知 |
| **Dedup** | 同一根因对同一团队不重复通知 | 合并到已有通知 |

---

## 二、配置指南

配置文件拆分在 `configs/` 目录下：

| 文件 | 内容 |
|------|------|
| `configs/config.alert.yaml` | 告警路由、抑制规则、Stakeholder 通知、分组、外部告警源注册 |
| `configs/config.business.yaml` | 业务拓扑、中间件声明、namespace 映射 |
| `configs/config.inspection.yaml` | 巡检规则 |
| `configs/config.core.yaml` | 核心系统参数 |
| `configs/config.diagnosis.yaml` | AI 诊断参数 |
| `configs/config.infra.yaml` | 基础设施连接（DB/Nebula/Kafka） |

### 2.1 configs/config.alert.yaml

```yaml
alert:
  routing:
    # === 基础路由 ===
    defaultReceiver: "default"
    defaultChannel: "slack"
    teamRouting:                          # team → receiver
      "电商组": "ecom-receiver"
    businessContextRouting:               # 团队回退路由
      "middleware-team": "middleware-receiver"

    # === 声明式抑制规则 ===
    inhibition:
      rules:
        - name: "node-suppress-pods"
          source:
            resourceType: "Node"
            severity: ["critical", "error"]
          target:
            resourceType: "Pod"
          topologyScope: "same_node"
          timeWindow: 300
        - name: "middleware-suppress-upstream"
          source:
            serviceType: "middleware"
            severity: ["critical", "error"]
          target:
            alertLabels:
              depends_on: "${source.appName}"
          topologyScope: "calls_app_upstream"
          timeWindow: 120
      exceptions:
        severity: ["critical"]
        criticality: ["P0"]

    # === Stakeholder 影响通知 ===
    impactRouting:
      enabled: true
      minOwnerSeverity: "error"           # Owner severity ≥ error 才通知
      defaultStakeholderChannel: "slack"  # Stakeholder 只用 Slack
      stakeholderRepeatInterval: 1800     # 同 team 同 root 30min 不重复
      stakeholderSeverityDowngrade:       # 降级规则
        critical: "error"
        error: "warning"
        warning: "info"
      maxStakeholderHops: 1
      excludeTeamSelf: true

    # === 分组 ===
    aggregation:
      groupBy: ["alertname", "namespace"]
      groupWait: 10s
      groupInterval: 30s
    
    severityChannelRules:
      critical: "pagerduty"
      error: "slack"
      warning: "slack"
      info: "slack"

  suppressor:
    topologySuppression:
      enabled: true
      timeWindowSeconds: 300
    severityExceptions: ["critical"]
    criticalityExceptions: ["P0"]

# === 外部告警源配置（alertSources）===
# 支持 Ceph/Kafka/MySQL/网络设备等外部系统告警接入
# enrichmentStrategy 选择桥接策略：
#   node_affinity        - 物理节点 → K8s Node → Pod → BusinessApp
#   service_graph        - 服务名 → BusinessApp → CallsApp 上下游
#   direct_business_app  - appName → BusinessApp 直接匹配
#   business_labels      - 从 Prometheus 标签直接提取业务信息
#   none                 - 不做业务富化，仅透传
alertSources:
  sources:
    - name: "ceph-prod"
      type: "ceph"
      displayName: "Ceph 生产集群"
      labelMapping:
        alertname: "alert_name"
        severity: "severity"
        node: "host"
        custom_pool: "pool"
      enrichmentStrategy: "node_affinity"
      businessMapping:
        team: "storage-team"
        criticality: "high"
      fingerprintKeys: ["alert_name", "host", "osd_id"]
    - name: "kafka-broker"
      type: "middleware"
      displayName: "Kafka 消息队列"
      labelMapping:
        alertname: "alert_name"
        severity: "severity"
        serviceName: "kafka"
        custom_topic: "topic"
      enrichmentStrategy: "service_graph"
      businessMapping:
        team: "data-platform-team"
        criticality: "high"
        serviceType: "middleware"
```

### 2.2 config.business.yaml 业务拓扑配置

```yaml
businessTopology:
  enabled: true

  # 中间件声明 — 替代 namespace 硬编码
  knownServices:
    "kafka":
      name: "kafka"
      namespace: "redpanda"
      ownerTeam: "middleware-team"
      type: "middleware"
    "redis-cluster":
      name: "redis-cluster"
      namespace: "redis"
      ownerTeam: "middleware-team"
      type: "cache"

  # namespace → 业务属性
  namespaceMapping:
    "kube-system":
      businessUnit: "基础设施"
      team: "platform-team"
      criticality: "critical"
      environment: "production"
    "redpanda":
      businessUnit: "消息基础设施"
      team: "middleware-team"
      criticality: "high"
      environment: "production"
    "ecommerce":
      team: "电商组"
      environment: "production"
    "prod-*":
      environment: "production"
```

### 2.3 config.inspection.yaml 巡检配置

```yaml
inspection:
  schedule: "0 */6 * * *"
  maxHistory: 30

  rules:
    - name: "single_point_failure"
      description: "单点故障检测"
      query: |
        MATCH (n:K8sResource{kind:'Node',is_deleted:false})
        MATCH (p:K8sResource{kind:'Pod',is_deleted:false})-[:RunsOn]->(n)
        WITH n, collect(p) as pods WHERE size(pods) > 30
        RETURN n.K8sResource.name as name, n.K8sResource.name_space as namespace, size(pods) as podCount
      check:
        type: "min_rows"
        threshold: 1
      severity: "warning"
      suggestion: "建议分散 Pod 到其他节点"

    - name: "image_audit"
      description: "latest 标签检测"
      query: |
        MATCH (p:K8sResource{kind:'Pod',is_deleted:false})
        RETURN p.K8sResource.name as name, p.K8sResource.name_space as namespace, p.K8sResource.resource_define as resourceDefine
        LIMIT 200
      check:
        type: "field_contains"
        field: "resourceDefine"
        value: ":latest"
      severity: "warning"
      suggestion: "避免使用 latest 标签"

    # 自定义检查 — 外部命令插件
    - name: "cert_expiry"
      description: "TLS 证书过期检测"
      query: |
        MATCH (s:K8sResource{kind:'Secret',is_deleted:false})
        WHERE s.K8sResource.resource_define CONTAINS 'kubernetes.io/tls'
        RETURN s.K8sResource.name as name, s.K8sResource.name_space as namespace, s.K8sResource.resource_define as resourceDefine
        LIMIT 100
      check:
        type: "command"
        command: "/etc/mutong/plugins/check_cert.py"
        timeout: "30s"
        params:
          warnDays: 30
          criticalDays: 0
      severity: "warning"
      suggestion: "证书即将过期，请尽快更新"
```

---

## 三、告警处理流程

### 3.1 拓扑富化（Enricher）

告警到达后，Enricher 通过 NebulaGraph 查询 4 件事：

| 步骤 | 查询 | 输出 |
|------|------|------|
| 资源定位 | `MATCH Pod → Node, Controller` | NodeName, OwnerKind, OwnerName |
| 团队归属 | `namespace → namespaceMapper` 或 `app.mutong.io/team` label | BusinessContext.Team |
| 业务调用链 | `MATCH BusinessApp ←[:CallsApp]` | BusinessCalls (上游/下游) |
| 关键级别 | `namespace Mapping` 或 `app.mutong.io/criticality` label | BusinessContext.Criticality |

### 3.2 Stakeholder 推导

> 详见[第四节](#四stakeholder-推导)

### 3.3 抑制检查（Suppressor）

抑制规则从上到下评估，first match wins：

```
1. severity 例外检查 → critical 跳过抑制
2. criticality 例外检查 → P0 业务跳过抑制
3. 拓扑抑制 → Node→Pod（已有）
4. 声明式规则检查 → 按 inhibition.rules 逐条匹配
5. 因果链抑制 → Kafka→上游超时（基于 BusinessApp 索引）
```

### 3.4 路由决策（Router）

```
Owner 路由:
  alert.Labels["team"] → TeamRouting
  或 alert.BusinessContext.Team → BusinessContextRouting
  → Owner receiver + channel

Stakeholder 路由:
  alert.Stakeholders 非空 + impactRouting.enabled
  → 每个 Stakeholder 查 TeamRouting
  → severity 降级（critical→error, error→warning）
  → ≥5 个 Stakeholder → 汇总通知模式
```

### 3.5 AI 诊断触发

AI 诊断不阻塞告警处理——异步触发，基于多维度置信度：

```
置信度 = 规则诊断分
       + 关联告警数 (≥3条 +0.25)
       + 业务关键度 (P0/critical +0.15)

≥ 阈值(默认0.8) → 直接返回结果
< 阈值       → 调 LLM 深度分析
```

---

## 四、Stakeholder 推导

### 4.1 推导策略

`determineStakeholderStrategy()` 根据告警特征分派策略：

```
告警是 Node ? → node_pods（查该 Node 上所有 Pod 的 team）
告警是中间件 ?
  ├─ ConsumerLag → upstream_consumer（查具体消费者）
  ├─ BrokerDown/OOM → upstream_all（查所有上游调用方）
  └─ 其他 → none
告警是普通业务 ?
  ├─ severity≥error → upstream_callers（查上游调用方）
  └─ severity=warning → none
```

### 4.2 中间件识别

通过 `config.business.yaml` 的 `knownServices` 声明。Go 层读取后注入 `BusinessContext.ServiceType`：

```go
// 代码中的判断
if alert.BusinessContext.ServiceType == "middleware" {
    // 按告警类型分派 deriv 策略
}
```

### 4.3 添加新的 Stakeholder 策略

1. 在 `determineStakeholderStrategy()` 加一个 `if` 分支
2. 在 `enrichStakeholders()` 的 `switch` 加一个 `case`
3. 实现对应的查询方法（如 `queryTeamsForNewPattern()`）

---

## 五、抑制规则

### 5.1 规则语法

```yaml
inhibition:
  rules:
    - name: "规则名称"
      source:                   # 抑制源（根因告警）
        resourceType: "Node"              # 资源类型
        serviceType: "middleware"         # 服务类型（来自 knownServices）
        severity: ["critical", "error"]   # 严重级别（至少一个匹配）
      target:                   # 被抑制方（衍生告警）
        resourceType: "Pod"              # 资源类型
        alertLabels:                      # 标签匹配（支持 ${source.xxx} 变量）
          depends_on: "${source.appName}"
      topologyScope: "same_node"         # 拓扑作用域
      timeWindow: 300                    # 有效时间窗口（秒）
  exceptions:
    severity: ["critical"]               # 这些 severity 永不抑制
    criticality: ["P0"]                  # 这些业务永不抑制
```

### 5.2 拓扑作用域（topologyScope）

| 值 | 含义 | 对应的 NebulaGraph 边 |
|---|------|---------------------|
| `same_node` | 同 Node | RunsOn 反向 |
| `same_owner` | 同 Controller | OwnedBy 反向 |
| `calls_app_upstream` | 业务上游依赖 | CallsApp 入向 |
| `calls_app_downstream` | 业务下游依赖 | CallsApp 出向 |

### 5.3 规则评估

每条规则独立评估。source 条件全部满足 + target 条件全部满足 + topologyScope 内存在 source 告警 → 抑制 target 告警。

**SOURCE 告警不受自己定义的规则影响**——它只作为抑制源，不会被自己抑制。

### 5.4 审计日志

每次抑制决策记录到 PostgreSQL `inhibition_records`：

```
fingerprint: 被抑制的告警指纹
suppressedBy: 抑制源告警指纹或规则名
reason: "rule: node-suppress-pods | scope: same_node"
suppressedAt: 抑制时间
```

---

## 六、巡检系统

### 6.1 声明式巡检规则

一条规则 = query + check + severity + suggestion。

**内置 check type**：

| type | 用途 | 参数 |
|------|------|------|
| `min_rows` | 查询结果行数 ≥ N | `threshold` |
| `field_contains` | 指定字段包含某字符串 | `field`, `value` |
| `command` | 外部命令插件 (stdin/stdout JSON) | `command`, `timeout`, `params` |

> **注**：`field_compare` 和 `prometheus` 检查类型为非内置类型，需通过 `command` 插件机制或自定义规则扩展实现。

### 6.2 外部命令插件

**stdin（mutong → 插件）**：
```json
{
  "rule": { "name": "cert_expiry", "params": { "warnDays": 30 } },
  "rows": [{"name": "my-tls", "resourceDefine": "{...}"}]
}
```

**stdout（插件 → mutong）**：
```json
{
  "findings": [{
    "resource": "my-tls", "namespace": "default",
    "severity": "warning", "message": "证书将在 15 天后过期"
  }]
}
```

**退出码**：0 = 成功，非 0 = 插件错误（记日志）

### 6.3 添加巡检规则

1. 在 `configs/config.inspection.yaml` 的 `rules` 列表中添加一条
2. 指定 `query`（nGQL）+ `check`（内置 type 或 command）+ `severity` + `suggestion`
3. 如果是自定义 type，编写 command 脚本放到 `/etc/mutong/plugins/`
4. 无需修改 Go 代码，无需重新编译

---

## 七、完整示例

### 7.1 场景：Kafka 故障影响多个业务团队

```
集群配置:
  ─ Kafka 在 redpanda namespace，team=middleware-team（来自 namespaceMapping）
  ─ order-service 在 ecommerce namespace，team=电商组
  ─ payment-service 在 payment namespace，team=支付组
  ─ CallsApp: order-service → Kafka, payment-service → Kafka

触发告警: KafkaBrokerDown (severity=critical)
```

**① 告警到达**：
```
Enricher:
  namespaceMapper.Resolve("redpanda") → team="middleware-team"
  knownServices["kafka"].type → ServiceType="middleware"
  CallsApp 入向查询 → upstreams=[order-service, payment-service]
```

**② Stakeholder 推导**：
```
determineStakeholderStrategy:
  ServiceType="middleware" + alertname 含 "Broker"
  → strategy="upstream_all"
  → 查 upstream teams → ["电商组", "支付组"]
  → 排除 Owner "middleware-team" → Stakeholders = [电商组, 支付组]
```

**③ 抑制检查**：
```
severity=critical → 跳过拓扑抑制
中间件故障抑制规则：source serviceType="middleware", severity=critical
  → 进入 activeAlerts
  后续 order-service 的 KafkaWriteTimeout 告警到达时
  → topologyScope="calls_app_upstream" → 查 activeAlerts → KafkaBrokerDown firing
  → 抑制 order-service 超时告警
```

**④ 路由**：
```
Owner: team="middleware-team" → businessContextRouting → "middleware-receiver" → PagerDuty
Stakeholder 电商组: teamRouting["电商组"] = "ecom-receiver" → Slack
Stakeholder 支付组: teamRouting["支付组"] = "pay-receiver" → Slack
Severity 降级: critical → error (Stakeholder 只收 error 级别)
```

**⑤ 通知**：
```
🔴 PagerDuty + Slack → middleware-team："KafkaBrokerDown - kafka-0 OOMKilled, 请立即处理"

⚠️ Slack → 电商组："业务影响通知：order-service 依赖的 Kafka 当前 BrokerDown，负责团队：middleware-team（已在处理中）"

⚠️ Slack → 支付组：（同上）
```

### 7.2 3 分钟后：上游超时被因果抑制

```
order-service 的 KafkaWriteTimeout 告警到达:
  → Enricher: team=电商组, Stakeholders=[]
  → Suppressor: 抑制规则 "middleware-suppress-upstream" 匹配
    → KafkaBrokerDown 仍在 firing → 抑制！
  → Router + Notifier: 跳过
  → 记录抑制日志: "Causal: downstream Kafka firing (KafkaBrokerDown)"
```

**效果**：1 条 Kafka 根因告警 → 通知 3 方（1 个 Owner + 2 个 Stakeholder）。后续 10+ 条上游超时告警全部被抑制，避免告警风暴。

---

## 八、提示词配置

### 8.1 提示词位置

AI 诊断和复盘报告的自然语言提示词模板存放在 `configs/prompts/` 目录：

```
configs/prompts/
├── diagnosis_pod.md         # Pod 故障诊断
├── diagnosis_node.md        # Node 故障诊断
├── diagnosis_deployment.md  # Deployment 故障诊断
├── diagnosis_service.md     # Service 故障诊断
├── diagnosis_ingress.md     # Ingress 故障诊断
├── diagnosis_pvc.md         # PVC 故障诊断
├── diagnosis_default.md     # 兜底模板
└── postmortem.md            # 复盘报告
```

### 8.2 修改提示词

直接编辑对应的 `.md` 文件，保存后重启服务即可生效，无需重新编译。

> **注意**：模板只包含自然语言叙事（诊断重点、分析要求），不包含数据块排版和 JSON 输出格式。这些由 Go 代码控制，保证修改不会破坏输出结构。

### 8.3 部署

构建时 `config/prompts/` 会自动复制到 `artifacts/` 目录，随二进制文件一起部署。生产环境修改后只需替换模板文件并重启。

## 九、故障排查

### 9.1 Stakeholder 没有收到通知

**检查清单**：
1. `config.alert.yaml` 中 `impactRouting.enabled: true` 是否设置
2. `config.business.yaml` 中 namespace 是否配置了 `team`
3. NebulaGraph 中是否存在 BelongsToApp 和 CallsApp 边
4. 告警 severity 是否 ≥ `minOwnerSeverity`
5. 查看 debug 日志：`enrichStakeholders: strategy=xxx`

### 9.2 抑制规则不生效

**检查清单**：
1. 规则格式是否正确：source/target/topologyScope/timeWindow 四个必填字段
2. topologyScope 是否匹配实际拓扑关系
3. source 告警是否在 `timeWindow` 秒内
4. source 告警的 severity 是否在规则的 `severity` 列表中
5. 是否被全局例外 (`exceptions.severity`) 跳过

### 9.3 巡检报告为空

**检查清单**：
1. nGQL 查询是否正确（可在 NebulaGraph Studio 中验证）
2. check type 是否匹配（min_rows 需要查询返回行数 ≥ threshold）
3. command 插件是否可执行、退出码是否为 0

### 9.4 日志级别

关键 debug 日志：
```
enrichStakeholders: strategy determined strategy=xxx serviceType=xxx
enrichStakeholders: done stakeholderCount=N
Alert routed receiver=xxx channel=xxx
inspection rule completed rule=xxx findings=N
```

设置 `log.level: debug` 可查看完整处理链路。
