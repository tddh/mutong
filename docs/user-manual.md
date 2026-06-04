# 重明 (Mutong) 用户使用手册

> 离，丽也。日月丽乎天，百谷草木丽乎土，重明以丽乎正，乃化成天下。——《易经·离卦·彖传》

本文档面向所有重明平台用户——SRE 运维人员、DevOps 工程师、业务研发和管理者。

---

## 目录

- [1. 快速开始](#1-快速开始)
- [2. 认证与权限](#2-认证与权限)
- [3. Web UI 功能](#3-web-ui-功能) — 15 个页面详解
- [4. CLI 工具 (mutongctl)](#4-cli-工具-mutongctl)
- [5. 配置详解](#5-配置详解) — 9 个配置文件
- [6. 告警管理](#6-告警管理)
- [7. AI 诊断](#7-ai-诊断)
- [8. 巡检系统](#8-巡检系统)
- [9. 自愈执行器](#9-自愈执行器)
- [10. 复盘分析](#10-复盘分析)
- [11. 业务拓扑](#11-业务拓扑)
- [12. 日志与指标](#12-日志与指标)
- [13. Docker 部署](#13-docker-部署)
- [14. 故障排查](#14-故障排查)
- [15. API 参考](#15-api-参考)
- [16. 安全](#16-安全)

---

## 1. 快速开始

### 1.1 系统要求

**必须**：
- Go 1.25+（后端编译运行）
- Bun 1.x+（前端包管理）
- NebulaGraph 3.8+（图数据库，硬依赖）
- Kafka 或 Redpanda 2.4+（消息队列，硬依赖）

**可选（按需启用，缺失时对应功能自动降级）**：
- PostgreSQL 15+（含 pgvector）
- Redis 7+
- Kubernetes 集群
- Prometheus 2.x+
- Elasticsearch 8.x+
- LLM API Key（OpenAI / Claude / MiniMax）
- Beyla + OTel Collector（业务拓扑）

### 1.2 5 分钟上手

```bash
# 1. 克隆项目
git clone https://gitee.com/tddh/mutong.git
cd mutong

# 2. 初始化配置
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
cp agent.yaml.example agent.yaml

# 3. 编辑核心配置
vim configs/config.core.yaml   # 至少配置 NebulaGraph + Kafka

# 4. 初始化图数据库
just init-nebula

# 5. 编译运行
just build-mac && just run    # macOS
just build && just run        # 其他平台
```

访问 `http://localhost:8888` 看到仪表盘即启动成功。

### 1.3 Docker 一键部署

```bash
docker compose up -d
```

自动启动 PostgreSQL、NebulaGraph、Redpanda、Redis 和 Mutong 主应用。

---

## 2. 认证与权限

### 2.1 认证模式

| 模式 | 说明 | 适用场景 |
|------|------|---------|
| `local` | 本地用户名密码（Argon2id 哈希） | 独立部署 |
| `hybrid` | 同时支持本地密码 + Zitadel OIDC SSO | 企业集成 |

配置在 `configs/config.auth.yaml`：
```yaml
auth:
  mode: local    # 或 hybrid
```

### 2.2 Token 类型

| Token | 前缀 | 使用者 | 说明 |
|-------|------|--------|------|
| Session Cookie | `mutong_session` | 浏览器 | HMAC-SHA256 签名 |
| PAT | `mtp_` | 人工 CLI | 个人 Access Token，30–365 天 |
| SAT | `mts_` | Agent/CI | Service Access Token，30–365 天 |
| JWT | `eyJ...` | 浏览器 | Zitadel OIDC 签发（hybrid 模式） |

### 2.3 角色与权限

| 角色 | 权限 | 默认仪表盘 |
|------|------|-----------|
| `admin` | read / write / execute / admin | 管理员视图 |
| `operator` | read / execute | 操作员视图 |
| `viewer` | read | 只读视图 |

### 2.4 登录方式

**Web UI 登录**：
```
http://localhost:8888/login
→ 输入用户名/密码
→ 进入仪表盘
```

**PAT 创建**（在 Web UI 中）：
```
仪表盘 → 用户设置 → API Tokens → Create Token
→ 复制 mtp_ 开头的 token（仅显示一次）
```

**CLI 登录**：
```bash
mutongctl auth login -s http://localhost:8888
# 自动打开浏览器完成 OAuth2 授权码流程（PKCE）
```

### 2.5 密码策略

默认使用 Argon2id 算法，配置在 `config.auth.yaml`：
- 内存：65536 KB
- 迭代：3 次
- 并行：4 线程

登录失败 3 次后需要验证码，连续 5 次失败锁定 15 分钟。IP 每分钟最多 10 次登录尝试。

---

## 3. Web UI 功能

### 3.1 全局导航栏

顶部导航栏提供 15 个功能页面入口：

> 仪表盘 | 拓扑 | 告警 | AI 诊断 | 巡检 | 巡检历史 | 复盘分析 | 历史复盘 | 资源列表 | 状态 | 日志 | 监控 | 终端 | 追踪 | 集群

### 3.2 仪表盘 (`/`)

**核心指标**（StatCards）：
- 资源总数、节点数、Pod 数
- 活跃告警数、被抑制告警数
- 巡检异常数

**快速链接**：一键跳转常用功能页面。

### 3.3 拓扑图 (`/topology/index.html`)

**三引擎渲染**：
- @antv/g6（2D 交互拓扑）
- force-graph（3D 力导向图）
- d3（自定义 SVG）

**物理视图**（K8s 资源）：
- Pod ⇄ Deployment、Service ⇄ Pod、Node ⇄ Pod、Ingress ⇄ Service
- 左侧筛选：命名空间、资源类型、标签、名称模糊匹配
- 右侧详情面板：指标徽章 + 关联资源

**业务视图**（应用调用关系）：
- BusinessApp ⇄ BusinessApp（HTTP/gRPC 调用链）
- 颜色标识：故障应用（红色）→ 上游（橙色）→ 下游（蓝色）

### 3.4 资源列表 (`/resource-table.html`)

- 表格分页（500 条/页）
- 按 Kind 着色
- 关键词搜索（300ms 防抖）
- 一键跳转 Terminal

### 3.5 告警管理 (`/alerts/index.html`)

- 聚合告警展开/折叠
- 按业务应用/团队/关键度筛选
- 右侧抽屉：基本信息 + AI 诊断结果双选项卡

### 3.6 AI 诊断 (`/diagnosis/index.html`)

- 对话式 Markdown 流式渲染（30ms 增量缓冲）
- 工具调用浮动面板（pulse 动画 + 自动消失）
- 4 个快速诊断按钮
- 分层超时：总 300s / 首 token 15s / 空闲 60s
- 流中断可重试

### 3.7 巡检报告 (`/inspection/index.html`)

- 手动触发巡检
- 最新报告展示
- 6 类内置规则：证书/单点/镜像/数据孤岛/资源配额/监控盲点

### 3.8 巡检历史 (`/inspection-history/index.html`)

- 历史报告筛选
- 报告对比（diff 视图）
- 趋势分析（7/14/30/60/90 天）

### 3.9 复盘分析 (`/retrospective/index.html`)

输入告警指纹后生成：
- 事件时间线（含 MTTD 延迟标注）
- 因果链 DAG 可视化
- 业务拓扑图（G6 渲染）
- 影响评估（爆炸半径 + 三层分层表格）
- 指标快照（CSS 条形图，支持自定义 PromQL）
- 关键日志（ERROR/WARN/INFO 分级颜色）
- 改进项看板（Prevent / Detect / Mitigate）

### 3.10 复盘历史 (`/retrospective-history/index.html`)

- 历史复盘筛选
- 详情查看与编辑修正（UPSERT）
- Markdown 导出

### 3.11 日志查询 (`/logs/index.html`)

- 7 个筛选器：namespace/pod/container/level/keyword/since/tail
- 按日志级别着色（ERROR 红 / WARN 橙 / INFO 蓝）
- Elasticsearch 全文检索

### 3.12 监控面板 (`/monitoring/index.html`)

- 左侧资源选择器（Pod / Node / Deployment / StatefulSet / DaemonSet / Service）
- 右侧指标卡片网格
- 8 类 Prometheus 指标（CPU / 内存 / 网络 / 重启等）

### 3.13 Web 终端 (`/terminal/index.html`)

- xterm.js 5.5
- 多容器切换
- 自适应 resize
- URL 参数预填充

### 3.14 链路追踪 (`/trace/index.html`)

- SkyWalking 兼容
- 按服务/操作/TraceID 查询
- Span 瀑布图展示

### 3.15 集群管理 (`/clusters/index.html`)

- 集群列表 + 健康检查
- 统计信息弹窗
- 30s 自动刷新

### 3.16 系统状态 (`/status/index.html`)

- 5 张状态卡片：LLM / 抑制状态 / 处理指标 / 诊断统计 / 自愈执行器
- 30s 自动刷新

---

## 4. CLI 工具 (mutongctl)

### 4.1 安装与配置

```bash
# 编译
just build-cli

# 设置 server 地址
mutongctl config set server http://localhost:8888

# 认证登录
mutongctl auth login
```

认证优先级：`auth login` > `--token` flag > `MUTONG_TOKEN` 环境变量

### 4.2 全局标志

| 标志 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| `--server` | `-s` | Mutong 服务地址 | `http://localhost:8888` |
| `--token` | `-t` | 认证 Token | 配置文件或环境变量 |
| `--output` | `-o` | 输出格式: json/table/md/yaml | TTY=table, 管道=json |
| `--verbose` | `-v` | 详细输出 | false |
| `--no-color` | — | 禁用颜色输出 | 管道自动 true |
| `--confirm` | — | 确认危险操作 | false |
| `--admin-key` | — | 管理员密钥 | `MUTONG_ADMIN_KEY` |

### 4.3 命令速查

| 命令 | 功能 |
|------|------|
| `mutongctl get pod <name>` | 获取资源详情 |
| `mutongctl list pods` | 列出资源（支持 6 种类型） |
| `mutongctl topo get <uid> --depth 2` | 拓扑查询 |
| `mutongctl alert list -o json` | 列表告警 |
| `mutongctl diagnose run --fingerprint <fp>` | AI 诊断 |
| `mutongctl inspect run` | 触发巡检 |
| `mutongctl retro generate <fp>` | 生成复盘 |
| `mutongctl logs pod <name>` | 查看日志 |
| `mutongctl metrics resource Pod --name <n>` | 查看指标 |
| `mutongctl biz apps --team "xxx"` | 业务应用 |
| `mutongctl exec execute --action restart_pod` | 自愈操作 |
| `mutongctl system status` | 系统状态 |
| `mutongctl debug-info -o json` | 环境诊断 |
| `mutongctl terminal --pod <name>` | Web 终端 |
| `mutongctl cluster list` | 集群列表 |
| `mutongctl stats overview` | 统计数据 |
| `mutongctl config contexts` | 多环境配置 |
| `mutongctl completion zsh` | Shell 补全 |
| `mutongctl --version` | 版本信息 |

### 4.4 输出格式

| 模式 | 触发 | 适用场景 |
|------|------|---------|
| **Table** | TTY 默认 | 人类终端查看 |
| **Markdown** | `-o md` | Agent 对话渲染、文档嵌入 |
| **JSON** | 管道 / `-o json` | 程序解析、jq 管道 |
| **YAML** | `-o yaml` | 配置导出 |

### 4.5 退出码

| 退出码 | 含义 |
|--------|------|
| 0 | 成功 |
| 1 | API 错误 |
| 2 | 参数错误 |
| 3 | 认证失败 |
| 4 | 网络错误 |
| 5 | 资源不存在 |

### 4.6 Agent 典型排查流程

```bash
# 1. 环境诊断
mutongctl debug-info -o json

# 2. 发现告警
mutongctl alert list -n production -o json

# 3. 一键诊断
mutongctl diagnose run --fingerprint <fp> -o json

# 4. 验证指标
mutongctl metrics resource Pod --name nginx -n production -o json

# 5. 查看日志
mutongctl logs pod nginx -n production --tail 50

# 6. 搜索历史案例
mutongctl retro search "OOMKilled" -o md
```

---

## 5. 配置详解

所有配置文件位于 `configs/` 目录，启动时通过 `-c configs/` 加载。

首次使用：
```bash
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
```

### 5.1 `config.core.yaml` — 核心配置

```yaml
# Kubernetes 配置（多集群模式推荐）
kubernetes:
  clusters:
    # - name: "prod-bj"
    #   config: kubeconfig-prod.yaml
    #   context: ""                    # 可选，指定 context
    #   enable: true                   # 默认 true
    # - name: "staging"
    #   config: kubeconfig-stg.yaml

# PostgreSQL
postgres:
  host: localhost
  port: 5432
  user: mutong
  pass: ""                # 通过 MUTONG_DB_PASSWORD 环境变量设置
  database: mutong
  pool: 100               # 连接池大小

# NebulaGraph（硬依赖）
nebula:
  host: "localhost"
  port: 9669
  user: "root"
  pass: ""                # 通过 MUTONG_NEBULA_PASS 环境变量设置
  space: "mutong"

# NebulaGraph 过期资源清理
nebulaCleanup:
  enabled: true
  retentionDays: 7        # 保留天数
  cleanupInterval: 24     # 清理间隔（小时）
  batchSize: 1000         # 每批处理数量

# Kafka 消息队列（硬依赖）
kafka:
  broker: "localhost:9092"
  topic: mutong30         # 资源变更事件 topic
  group: mutong30         # 消费组
  deadLetterTopic: ""     # 死信队列
  maxRetries: 5
  retryBackoffMs: 100
  traceTopic: mutong_trace_01    # 链路追踪 topic
  traceGroup: mutong_trace_v30
  businessWorkloadTopic: business-workloads
  businessWorkloadGroup: mutong-business-workload
  workloadKinds:                 # 需要监听的工作负载类型
    - Deployment
    - StatefulSet
    - DaemonSet
    - Job
    - CronJob

# 标签过滤（排除不需要的资源）
excludeLabels:
  - "app.kubernetes.io/managed-by: Helm"
  - "app.kubernetes.io/component: controller-eligible"
  - "apps.kubernetes.io/pod-index"

# BigCache 缓存
cache:
  lifeTime: 40              # 缓存生命周期（秒）
  cleanWindow: 10           # 清理窗口（秒）
  enable: true
  hardMaxCacheSize: 4096    # 最大缓存大小（MB）
  shards: 256               # 分片数量
  resyncInterval: 30        # 同步间隔（秒）

# 日志
log:
  level: info               # debug / info / warn / error
  path: logs
  max_size: 100             # 单文件最大 MB
  max_age: 30               # 保留天数

# HTTP 服务器
server:
  address: "0.0.0.0"
  port: 8888
  readTimeoutSec: 60
  writeTimeoutSec: 60
  idleTimeoutSec: 120
  shutdownTimeoutSec: 30
  rateLimitPerSec: 100
```

**环境变量覆盖**：`MUTONG_ADDR`、`MUTONG_PORT`、`MUTONG_DB_USER`、`MUTONG_DB_PASSWORD`、`MUTONG_NEBULA_USER`、`MUTONG_NEBULA_PASS`、`MUTONG_LLM_API_KEY`

### 5.2 `config.alert.yaml` — 告警配置

```yaml
alert:
  routing:
    # === 基础路由 ===
    defaultReceiver: "default"
    defaultChannel: "slack"
    defaultSeverity: "warning"
    severityMapping:
      critical: 1
      error: 2
      warning: 3
      info: 4
    teamRouting: {}                      # team → receiver
    businessContextRouting: {}           # 业务上下文回退路由
    serviceRouting: {}                   # service → receiver
    severityChannelRules:
      critical: "pagerduty"
      error: "slack"
      warning: "slack"
      info: "slack"

    # === 声明式抑制规则 ===
    inhibition:
      rules: []                          # 自定义抑制规则列表
      exceptions:
        severity: ["critical"]           # 不被抑制的级别
        criticality: ["P0"]              # 不被抑制的关键度

    # === 告警分组 ===
    aggregation:
      groupBy: ["alertname", "namespace"]
      groupWait: 10s
      groupInterval: 30s

    # === Stakeholder 影响通知 ===
    impactRouting:
      enabled: false                     # 默认关闭
      minOwnerSeverity: "error"
      defaultStakeholderChannel: "slack"
      stakeholderDefaultReceiver: "default"
      stakeholderRepeatInterval: 1800
      stakeholderSeverityDowngrade:
        critical: "error"
        error: "warning"
        warning: "info"
      maxStakeholderHops: 1
      excludeTeamSelf: true              # 不发给自己团队

  # 通知渠道
  notifier:
    slackWebhookUrl: ""
    pagerdutyApiKey: ""
    dingtalkWebhook: ""
    emailConfig:
      smtpHost: "smtp.example.com"
      smtpPort: 587
      smtpUser: "alerts@example.com"
      smtpPassword: ""
      fromAddress: "alerts@example.com"
    channelEnabled:
      slack: true
      pagerduty: true
      dingtalk: false
      email: true

  # 抑制器
  suppressor:
    ruleChains: []
    timeWindowSeconds: 300
    maxDepth: 3
    severityExceptions: ["critical"]
    criticalityExceptions: ["P0"]
    maxAlertAgeMinutes: 120
    cleanupIntervalSec: 60

  # 清理
  cleanup:
    cleanupInterval: 1                   # 小时
    retentionDuration: 4320              # 小时（约半年）

  # 存储
  storage:
    type: postgres

  # 统计持久化
  statsSyncInterval: 10                  # 秒
```

**声明式抑制规则**示例：
```yaml
alert:
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
```

**Stakeholder 通知**（受影响的业务方）：
```yaml
alert:
  impactRouting:
    enabled: true              # 是否启用业务方通知
    minOwnerSeverity: "error"  # 最低通知级别
    defaultStakeholderChannel: "slack"
```

### 5.3 `config.business.yaml` — 业务拓扑配置

```yaml
businessTopology:
  enabled: true

  # 静态 IP/域名映射（trace IP 转 BusinessApp）
  knownServices:
    "kafka":
      name: "kafka"
      namespace: "base"

  # 命名空间 → 业务单元映射（支持通配符）
  namespaceMapping:
    "kube-system":
      businessUnit: "基础设施"
      team: "platform-team"
      criticality: "critical"
    "prod-*":
      environment: "production"
    "staging-*":
      environment: "staging"

# RBAC
roles:
  enabled: true
  defaultRole: "viewer"
  roles:
    - name: "admin"
      permissions: ["read", "write", "execute", "admin"]
    - name: "operator"
      permissions: ["read", "execute"]
    - name: "viewer"
      permissions: ["read"]
```

### 5.4 `config.diagnosis.yaml` — AI 诊断配置

```yaml
diagnosis:
  enabled: true
  confidenceThreshold: 0.8    # 置信度阈值（≥此值不触发 LLM）
  cacheTTL: 3600              # 诊断缓存 TTL（秒）
  resultRetentionDays: 90     # 结果保留天数
  statsSyncInterval: 10       # 统计同步间隔（秒）
  llm:
    provider: "openai"        # none / openai / claude / custom
    model: "gpt-4"
    apiKey: ""                # 通过 MUTONG_LLM_API_KEY 设置
    baseURL: "https://api.openai.com"
    endpoint: "/chat/completions"
    timeout: 120
    embedding:
      model: "text-embedding-3-small"
      apiKey: ""              # 依次取 MUTONG_EMBEDDING_API_KEY → MUTONG_LLM_API_KEY
      baseURL: "https://api.openai.com"
  session:
    enabled: true
    redis: "localhost:6379"
    password: ""
    db: 9
    ttl: 1800

# 巡检
inspection:
  enabled: true
  cronSpec: "0 */6 * * *"    # 每 6 小时

# Prometheus 指标查询
prometheus:
  enabled: true
  url: "http://localhost:9090"
  timeout: 30
```

### 5.5 `config.infra.yaml` — 基础设施配置

```yaml
# Elasticsearch 日志查询
elasticsearch:
  addresses: ["http://localhost:9200"]
  indexPattern: "k8s-logs-*"
  serviceToIndex:
    default: "k8s-logs-*"
    kafka: "kafka-logs-*"
    redis: "redis-logs-*"
  username: ""
  password: ""
  timeout: 10

# OpenTelemetry 链路追踪
opentelemetry:
  enabled: false
  collectorURL: "http://localhost:4318"
  serviceName: "mutong"
  timeout: 30

# 自愈执行器
executor:
  enabled: false
  autoMode: false
  # defaultNamespace: "default"       # 默认命名空间
  # maxRestartCount: 3                # 单 Pod 最大重启次数
  # coolDownMinutes: 15               # 同一资源冷却时间（分钟）
  # gracePeriodSec: 30                # Pod 优雅终止等待秒数
  # deleteGracePeriodSec: 5           # 强制删除等待秒数
  # maxReplicas: 10                   # 扩缩容副本上限
  # defaultHPA:                       # 创建 HPA 默认参数
  #   minReplicas: 2
  #   maxReplicas: 10
  #   targetCPU: 70
  auditLog:
    type: postgres
    table: executor_audit_logs
  actions:
    restart_pod:
      risk: low              # 风险等级: low / medium / high
      autoThreshold: 0.7     # 自动执行置信阈值
    scale_deployment:
      risk: medium
      autoThreshold: 0.85
    delete_pod:
      risk: high
      autoThreshold: 1.0
    create_hpa:
      risk: medium
      autoThreshold: 0.9
    update_hpa:
      risk: medium
      autoThreshold: 0.85
    update_configmap:
      risk: medium
      autoThreshold: 0.9
    update_secret:
      risk: high
      autoThreshold: 0.95
    update_resource_limits:
      risk: medium
      autoThreshold: 0.85
    update_deployment_image:
      risk: high
      autoThreshold: 0.9
    update_annotations:
      risk: low
      autoThreshold: 0.7
    update_labels:
      risk: low
      autoThreshold: 0.7
```

### 5.6 `config.inspection.yaml` — 巡检配置

```yaml
inspection:
  enabled: true
  cronSpec: "0 */6 * * *"    # 每 6 小时执行一次
```

巡检规则由 Go 代码注册 6 类内置规则：证书过期、单点故障、数据孤岛、资源配额、监控盲点、镜像审计。

### 5.7 `config.profiles.yaml` — 资源画像配置

定义各 K8s 资源类型的 PromQL 指标查询、拓扑关系和证据来源。详见 [config.profiles.yaml.example](../configs/config.profiles.yaml.example)。

支持的资源类型：Pod、Node、Deployment、Service、PVC、Ingress、StatefulSet、DaemonSet。

### 5.8 `config.auth.yaml` — 认证配置

```yaml
auth:
  mode: local                 # local / hybrid
  issuer: "https://mutong.example.com"
  access_token_lifespan: 15m
  refresh_token_lifespan: 168h
  global_secret: ""           # 32 bytes base64，通过 MUTONG_AUTH_SECRET 设置
  rsa_private_key_path: ""    # 通过 MUTONG_AUTH_RSA_KEY_PATH 设置
  captcha:
    enabled: true
  login_ratelimit:
    enabled: true
    ip_max_per_minute: 10
  password:
    algorithm: argon2id
```

### 5.9 `config.retrospective.yaml` — 复盘配置

```yaml
retrospective:
  retentionDays: 180          # 复盘报告保留天数
  autoTrigger:
    enabled: false
    delayMinutes: 10          # 告警解决后延迟生成
    minSeverity: "warning"    # 最低触发级别
    maxPerHour: 10            # 每小时最大生成数
    skipIfDiagnosisOlderThan: "24h"  # 诊断超过此时间则跳过
```

---

## 6. 告警管理

### 6.1 告警处理流水线

```
Prometheus ──→ Alertmanager (分组/去重) ──→ webhook ──→ Mutong
                                                        │
                                              ① 拓扑富化 (Enricher)
                                              ② 抑制检查 (Suppressor)
                                              ③ 路由决策 (Router)
                                              ④ 通知分发 (Notifier)
                                              ⑤ AI 诊断 (autoDiagnosis)
```

### 6.2 Webhook 接入

在 Alertmanager 的 `alertmanager.yml` 中添加：
```yaml
route:
  receiver: mutong
receivers:
  - name: mutong
    webhook_configs:
      - url: http://mutong:8888/api/v1/alerts/webhook
        send_resolved: true
```

### 6.3 外部告警源

支持 Ceph、Kafka、MySQL 等任意系统接入，配置在 `config.alert.yaml` 的 `alertSources` 部分。

**4 种富化策略**：

| 策略 | 说明 | 适用场景 |
|------|------|---------|
| `node_affinity` | host/node → K8s Node → Pod → BusinessApp | 物理节点桥接（如 Ceph） |
| `service_graph` | serviceName → BusinessApp → 调用链上下游 | 服务拓扑桥接（如 Kafka） |
| `direct_business_app` | appName → BusinessApp 直接匹配 | 应用层直接告警 |
| `business_labels` | 直接从标签提取 team/businessUnit | Prometheus 侧已打标签 |
| `none` | 不做业务富化，仅透传 | 无需关联 |

**示例**：
```yaml
alertSources:
  sources:
    - name: "ceph-prod"
      type: "ceph"
      displayName: "Ceph 生产集群"
      labelMapping:
        alertname: "alert_name"
        node: "host"
      enrichmentStrategy: "node_affinity"
      businessMapping:
        team: "storage-team"
        criticality: "high"
      fingerprintKeys:
        - "alert_name"
        - "host"
```

POST 到 `/api/v1/alerts/external/:source` 即可接收。

### 6.4 抑制规则

**拓扑抑制**：同一 Node 上的 Pod 告警在 Node 级别告警时被抑制。

**因果链抑制**：Kafka 故障自动抑制上游 Consumer 超时告警。

### 6.5 Owner / Stakeholder 双路由

- **Owner**（负责修复团队）：PagerDuty + Slack，立即处理
- **Stakeholder**（受影响业务方）：Slack/钉钉，仅通知，severity 自动降级

---

## 7. AI 诊断

### 7.1 混合诊断引擎

```
告警 → 规则评分 ──→ 置信度 ≥ 阈值? ──是──→ 直接返回
              ↓否                        ↑
              LLM ReAct Agent ───────────┘
```

**快速路径**：多维度置信度评分（规则诊断分 + 关联告警 + 业务关键度）。

**LLM 深度路径**：
- Eino ReAct Agent（字节跳动开源）
- 20 个 MCP 工具全量注册
- 自动降级：LLM 不可用时切换规则模式

### 7.2 MCP 工具列表（20 个）

| 工具名 | 功能 |
|--------|------|
| `query_topology` | 查询 K8s 资源拓扑关系 |
| `get_active_alerts` | 获取活跃告警列表 |
| `get_alert_detail` | 获取指定告警详情 |
| `run_diagnosis` | 执行 AI 诊断流程 |
| `inspect_resource` | 检查资源实时状态 |
| `get_inspection_report` | 获取最新巡检报告 |
| `list_resources_from_graph` | 从 NebulaGraph 列出资源 |
| `list_k8s_resources` | 从 K8s API 查询资源清单 |
| `list_resources_from_cache` | 从 Informer 缓存查询 |
| `get_resource_metrics` | 获取 Prometheus 指标快照 |
| `query_metric_timeseries` | 查询时序数据 |
| `get_system_health` | 获取系统健康状态 |
| `get_metric_catalog` | 获取指标目录 |
| `get_pod_logs` | 获取 Pod 日志（K8s API） |
| `get_pod_logs_es` | 获取 Pod 日志（ES） |
| `search_logs` | 日志全文搜索（ES） |
| `get_error_logs` | 获取 Error 级别日志 |
| `search_similar_cases` | 向量搜索历史案例 |
| `list_alerts` | 简化版告警列表 |
| `generate_retrospective` | 生成复盘报告 |

### 7.3 对话式诊断

Web UI 的 AI 诊断页面支持对话交互：

```bash
POST /api/v1/diagnosis/chat/ask
{
    "question": "这个告警对我的订单服务有什么影响？",
    "sessionId": "xxx"
}
```

### 7.4 SSE 流式输出

全流程 `text/event-stream`，前端：
- 30ms 增量缓冲
- 300s 总超时 / 15s 首 token 超时 / 60s 空闲超时
- 流中断可重试

---

## 8. 巡检系统

### 8.1 3 类检查类型

| 类型 | 说明 |
|------|------|
| `min_rows` | 查询结果行数是否达到阈值（如确保副本数 ≥ 2） |
| `field_contains` | 字段值是否包含指定子串（如证书 CN 是否包含预期域名） |
| `command` | 外部插件脚本，stdin/stdout JSON 接口，任意语言编写 |

### 8.2 6 类内置规则

| 规则 | 检查内容 |
|------|---------|
| 证书过期 | TLS 证书有效期检查 |
| 单点故障 | 无冗余的 Deployment/StatefulSet |
| 数据孤岛 | 孤立存储卷、未挂载的 PVC |
| 资源配额 | CPU/内存超配额 |
| 监控盲点 | 未被 Prometheus 覆盖的 Pod |
| 镜像审计 | 使用 latest 标签或过期镜像 |

### 8.3 执行方式

**自动执行**（Cron 调度）：
```yaml
inspection:
  cronSpec: "0 */6 * * *"    # 每 6 小时
```

**手动触发**：
```bash
mutongctl inspect run
```

### 8.4 报告对比与趋势

```bash
# 对比两份报告
mutongctl inspect compare <id1> <id2>

# 查看趋势（7/14/30/60/90 天）
mutongctl inspect trend --days 30
```

---

## 9. 自愈执行器

### 9.1 支持的操作（11 种）

| 操作 | 风险 | 说明 |
|------|------|------|
| `restart_pod` | Low | 优雅重启（Eviction → Delete） |
| `delete_pod` | High | 强制删除 Pod |
| `scale_deployment` | Medium | 扩缩容（有上限保护） |
| `create_hpa` | Medium | 创建 HPA |
| `update_hpa` | Medium | 更新 HPA 配置 |
| `update_configmap` | Medium | 更新 ConfigMap |
| `update_secret` | High | 更新 Secret |
| `update_resource_limits` | Medium | 调整 CPU/内存限制 |
| `update_deployment_image` | High | 变更容器镜像 |
| `update_annotations` | Low | 修改注解 |
| `update_labels` | Low | 修改标签 |

### 9.2 执行模式

```yaml
executor:
  enabled: true
  autoMode: false           # false=手动审批（默认）, true=自动执行
```

### 9.3 安全保护

- 副本扩缩容有 `maxReplicas` 上限
- 同一资源有冷却时间（`coolDownMinutes`）
- 单 Pod 最大重启次数限制（`maxRestartCount`）
- Pod 删除使用 graceful 模式（`gracePeriodSec`）

### 9.4 审计日志

所有操作写入 PostgreSQL：操作人、时间、目标资源、操作类型、执行结果、风险等级。

```bash
mutongctl exec audit --action restart_pod
```

---

## 10. 复盘分析

### 10.1 复盘内容

- **事件时间线**：含 MTTD 标注
- **因果链 DAG**：故障传播路径
- **LLM 复盘报告**：WhatWentWell / WhatWentWrong / ContributingFactors
- **业务拓扑图**：G6 渲染，故障/上游/下游三色显示
- **指标快照**：CSS 条形图展示诊断时指标
- **关键日志**：ES 日志卡片，分级颜色
- **改进项看板**：Prevent / Detect / Mitigate

### 10.2 图增强混合检索

```
pgvector 语义搜索 ──→ 融合排序 ──→ 结果
         ↑                        ↑
NebulaGraph 拓扑搜索 ──────────────┘
```

同 Deployment / 同 Node 的历史相似故障自动发现。

### 10.3 复盘-资源双向关联

```bash
# 按资源 UID 查询所有历史复盘
GET /api/v1/retrospective/resource/{resourceUID}/postmortems

# 按关键词搜索
mutongctl retro search "OOMKilled"
```

### 10.4 UPSERT

同一 fingerprint 重复生成自动覆盖，不产生重复报告。

---

## 11. 业务拓扑

### 11.1 数据流

```
Beyla (eBPF 采集) ──→ OTel Collector ──→ Kafka ──→ Mutong
                                                     ├── BusinessLabelSyncer
                                                     └── TraceTopologySyncer
                                                      ↓
                                                NebulaGraph
```

### 11.2 业务应用模型

```go
type BusinessApp struct {
    UID          string
    AppName      string        // 应用名，如 order-service
    Namespace    string
    Criticality  string        // critical / high / medium / low
    Environment  string        // production / staging / development
    Team         string        // 负责团队
    BusinessUnit string        // 业务单元
}
```

### 11.3 归属识别

系统通过以下优先级将 K8s 资源映射到 BusinessApp：
1. 标签 `app.mutong.io/name` → 匹配 BusinessApp
2. 标签 `app.kubernetes.io/name` → 匹配 BusinessApp
3. `BelongsToApp` 边直接关联
4. Owner 链向上查找：Pod → ReplicaSet → Deployment → BusinessApp

### 11.4 查询

```bash
# CLI
mutongctl biz apps --team "交易平台组"
mutongctl biz graph --businessUnit "core"

# Web UI
拓扑图 → 业务视图（下拉切换）

# API
GET /api/v1/business-topology/apps
GET /api/v1/business-topology/graph
```

### 11.5 部署业务拓扑组件

详见 [deploy/README.md](./../deploy/README.md)。

前置：Beyla + OTel Collector Helm Chart。

---

## 12. 日志与指标

### 12.1 日志查询

**Elasticsearch**：
- Pod 日志检索：GET `/api/v1/logs/pod`
- 关键词搜索：GET `/api/v1/logs/search`
- Error/Warn/Info 分级查询

**K8s API 直查**：Web 终端或 CLI `mutongctl logs pod`

### 12.2 指标查询

**Prometheus**：
- Pod / Node / Deployment / StatefulSet / DaemonSet / Service 指标
- 阈值判断、HPA 状态
- 时序数据查询

**自定义 PromQL**（资源画像 `config.profiles.yaml`）：
```yaml
resourceProfiles:
  Pod:
    metrics:
      - name: cpu_usage_cores
        promql: 'sum(rate(container_cpu_usage_seconds_total{pod="{{name}}"}[5m]))'
```

---

## 13. Docker 部署

### 13.1 一键部署

```bash
docker compose up -d
```

启动以下服务：
| 服务 | 镜像 | 用途 |
|------|------|------|
| PostgreSQL 16 + pgvector | `pgvector/pgvector:pg16` | 关系数据 + 向量 |
| NebulaGraph | `vesoft/nebula-graphd:v3.8.0` | 图数据库 |
| Redpanda | `vectorized/redpanda:v24.2.5` | 消息队列 |
| Redis | `redis:7-alpine` | 会话缓存 |
| Mutong | 本地构建 | 主应用 |

### 13.2 部署业务拓扑组件

```bash
helm install beyla grafana/beyla -n beyla --create-namespace \
  -f deploy/helm-beyla-values.yaml
helm install otel-collector open-telemetry/opentelemetry-collector \
  -n otel-collector --create-namespace \
  -f deploy/helm-otelcol-values.yaml
```

---

## 14. 故障排查

### 14.1 启动问题

**Q: 启动失败，报错 `cannot connect to NebulaGraph`**
- 检查 NebulaGraph 是否运行：`nebula-console -addr localhost -port 9669 -u root -p nebula`
- 确认 `config.core.yaml` 中的 `nebula.host` 和 `nebula.port` 正确

**Q: 拓扑页面无数据**
- 检查 Kafka 是否正常运行
- 确认 `config.infra.yaml` 中 Kafka broker 和 topic 配置正确
- Informer 首次同步需要 1-2 分钟

**Q: 集群状态显示"未知"**
- 检查 kubeconfig 路径是否正确
- 确认 `config.core.yaml` 中 `kubernetes.config` 指向有效 kubeconfig

### 14.2 AI 诊断问题

**Q: AI 诊断不可用，提示 LLM 未连接**
- 确认 `config.diagnosis.yaml` 中 `provider` 不为 `none`
- 确认 `apiKey` 已设置、`baseURL` 可访问
- 推荐通过 `MUTONG_LLM_API_KEY` 环境变量设置密钥

**Q: 语义搜索不生效**
- 确认 `config.diagnosis.yaml` 中 `embedding.model` 已配置
- 不配置 embedding 时，图结构搜索路径仍可正常工作

### 14.3 日志与指标

**Q: Prometheus 指标无数据**
- 确认 `config.diagnosis.yaml` 中 `prometheus.enabled` 为 `true`
- 确认 `prometheus.url` 可访问

**Q: 日志查询返回空**
- 确认 `config.infra.yaml` 中 ES 的 `addresses` 和 `indexPattern` 正确
- 确认 ES 中存在对应索引

### 14.4 性能优化

**Q: 系统响应缓慢**
- 调整 BigCache 的 `hardMaxCacheSize`（建议 4096 MB）和 `shards`（建议 256）
- 定期执行 Nebula 清理（`nebulaCleanup.enabled: true`）
- PostgreSQL 连接池建议保持 `pool: 100`

---

## 15. API 参考

### 15.1 K8s 资源 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/k8s/resources/{name}` | 获取指定资源详情 |
| GET | `/k8s/resources/graph/nodes` | 获取所有资源节点 |
| GET | `/k8s/resources/graph/edges` | 获取所有资源关系 |
| GET | `/k8s/resources/graph/search` | 搜索资源关系 |

### 15.2 告警管理 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/alerts/webhook` | 接收 Alertmanager Webhook |
| POST | `/api/v1/alerts/external/:source` | 接收外部系统告警 |
| GET | `/api/v1/alerts` | 获取活跃告警列表 |
| GET | `/api/v1/alerts/{fingerprint}` | 获取告警详情 |

### 15.3 AI 诊断 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/diagnosis/run` | 执行 AI 诊断 |
| POST | `/api/v1/diagnosis/chat/ask` | AI 诊断问答 |
| GET | `/api/v1/diagnosis/result/{id}` | 获取诊断结果 |
| GET | `/api/v1/diagnosis/tools` | 列出所有 MCP 工具 |

### 15.4 巡检 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/inspection/execute` | 手动触发巡检 |
| GET | `/api/v1/inspection/report` | 获取最新报告 |
| GET | `/api/v1/inspection/trend` | 趋势数据 |

### 15.5 自愈执行器 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/executor/execute` | 执行自愈操作 |
| POST | `/api/v1/executor/toggle` | 切换自动/手动模式 |
| GET | `/api/v1/executor/audit` | 审计日志列表 |

### 15.6 复盘分析 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/retrospective/postmortem/{fingerprint}` | 生成复盘报告 |
| GET | `/api/v1/retrospective/timeline/{fingerprint}` | 事件时间线 |
| GET | `/api/v1/retrospective/knowledge/search` | 图增强混合检索 |

### 15.7 其他 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/business-topology/apps` | 业务应用列表 |
| GET | `/api/v1/business-topology/graph` | 业务拓扑图数据 |
| GET | `/api/v1/logs/search` | 日志搜索 |
| GET | `/api/v1/metrics/pod` | Pod 指标 |
| GET | `/api/v1/terminal/ws` | WebSocket 终端 |
| GET | `/api/v1/trace/query` | 链路查询 |
| GET | `/api/v1/system/status` | 系统状态 |

---

## 16. 安全

### 16.1 认证安全

- **Argon2id** 密码哈希（内存/时间/并行度可配置）
- **HMAC-SHA256** Session Cookie 签名
- **PKCE** OAuth2 授权码流程
- **登录限流**：IP 每分钟 10 次
- **验证码**：3 次失败后触发
- **锁定**：5 次失败锁定 15 分钟

### 16.2 数据安全

- **PAT/SAT 哈希存储**：SHA-256 后存 DB，原始值不可逆
- **nGQL 注入防护**：所有输入参数安全转义
- **RBAC**：Casbin 权限模型（admin/operator/viewer）

### 16.3 操作安全

- 所有自愈操作需审计日志
- 高风险操作（delete_pod 等）需 `--admin-key`
- 自动执行有置信度阈值限制
- 副本扩缩容有上限保护

---

> 详细架构设计参考 [PROJECT_ARCHITECTURE.md](./PROJECT_ARCHITECTURE.md)、认证设计 [auth-design.md](./auth-design.md)。
> mutongctl 完整用法参考 [mutongctl-usage.md](./mutongctl-usage.md)。
> 告警路由手册参考 [alert-routing-usage-manual.md](./alert-routing-usage-manual.md)。
