# 重明 (Mutong) AIOps 平台架构文档

> 最后更新: 2026-05-26。全量重写，反映当前代码库实际状态。

---

## 一、项目概述

重明 (Mutong) 是一款面向 Kubernetes 集群的资源可视化与智能运维（AIOps）平台。以 **"看见、理解、修复、沉淀"** 为核心理念，从资源可视化出发，经由混合 AI 诊断引擎定位根因、自动执行修复、将故障复盘沉淀为可检索的知识资产，形成运维闭环。

### 技术栈

| 分类     | 技术                          | 版本/说明                                   |
| ------ | --------------------------- | --------------------------------------- |
| 后端语言   | Go                          | 1.25                                    |
| Web 框架 | Gin                         | v1.10                                   |
| 图数据库   | NebulaGraph                 | v3 (vesoft-inc/nebula-go/v3)            |
| 关系数据库  | PostgreSQL + pgvector       | GORM v1.31 驱动，连接池 100                   |
| 消息队列   | Kafka                       | franz-go v1.21，死信队列 + 指数退避重试            |
| 内存缓存   | BigCache                    | v3.1，分片高性能缓存                            |
| 会话缓存   | Redis                       | go-redis/v9                             |
| 日志     | Zap + Lumberjack            | 结构化日志 + 自动轮转                            |
| 监控     | Prometheus + gops           | client_golang v1.23                     |
| 链路追踪   | OpenTelemetry + SkyWalking  | otlp v1.10, skywalking-go v0.6          |
| AI 框架  | Eino (cloudwego/eino)       | v0.8.13, ReAct Agent + Function Calling |
| LLM    | MiniMax M2.5 / OpenAI（兼容协议） | 可配置，自动降级                                |
| eBPF   | Beyla (Grafana)             | 应用调用关系自动采集                              |
| 性能分析   | Pyroscope                   | pyroscope-go v1.2.7                     |
| 前端     | Vue 3 + Vite                | 多页面 SPA, 15 个独立页面                       |
| 可视化    | @antv/g6 + force-graph + d3 | 拓扑图 / DAG / 力导向图                        |
| 构建     | Just                        | 多平台交叉编译                                 |
| CLI 框架 | Cobra + Viper               | 命令路由、配置管理、Shell 补全                      |
| 容器编排   | client-go                   | v0.34.1, dynamic informer               |

---

## 二、系统架构

### 2.1 架构总览

```
┌──────────────────────────────────────────────────────────────────────┐
│                          前端 (Vue 3 + Vite)                          │
│  15个独立页面 (index/topology/alerts/diagnosis/inspection/inspection-history/...)       │
│  @antv/g6 | force-graph | d3 | marked                                │
└──────────────────────────────────┬───────────────────────────────────┘
                                    │ HTTP/WebSocket (/api/*, /k8s/*)
┌─────────────────┐                 │
│ mutongctl CLI   │─── HTTP/JSON ───┤
│ Cobra + Viper   │                 │
│ 19 个命令模块    │                 │
└─────────────────┘                 ▼
┌──────────────────────────────────────────────────────────────────────┐
│                       Gin HTTP Server (Gin v1.10)                    │
│  ┌──────────────┬───────────────┬──────────────────────────────────┐ │
│  │ Auth MW      │ Rate Limiter  │ CORS Middleware                  │ │
│  └──────────────┴───────────────┴──────────────────────────────────┘ │
└──────────────────────────────────┬───────────────────────────────────┘
                                   │
         ┌─────────────────────────┼─────────────────────────┐
         ▼                         ▼                         ▼
┌─────────────────┐  ┌─────────────────────┐  ┌────────────────────────┐
│  Controllers    │  │  Services (核心)     │  │  Interfaces (契约)      │
│  26个文件（含oauth2/子包共31个）│  │  ┌───────────────┐   │  │  Logger/GraphDB/MQ/    │
│  - alert        │  │  │ alert 告警管道 │   │  │  Cache/K8s/LLM/...     │
│  - diagnosis    │  │  ├───────────────┤   │  │                         │
│  - inspection   │  │  │ diagnosis AI  │   │  │  依赖反转设计           │
│  - executor     │  │  ├───────────────┤   │  │  所有服务依赖接口       │
│  - retrospective│  │  │ retrospective│   │  └────────────────────────┘
│  - k8sresource  │  │  ├───────────────┤   │
│  - log/metrics  │  │  │ mcp 工具服务  │   │
│  - terminal     │  │  ├───────────────┤   │
│  - trace        │  │  │ inspection   │   │
│  - cluster      │  │  ├───────────────┤   │
│  - user/role    │  │  │ executor     │   │
│  - status/stats │  │  ├───────────────┤   │
│  - business_topo│  │  │ prometheus   │   │
│                  │  │  ├───────────────┤   │
│                  │  │  │ logsearch    │   │
│                  │  │  ├───────────────┤   │
│                  │  │  │ k8sresource  │   │
│                  │  │  └───────────────┘   │
└─────────────────┘  └─────────┬─────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          ▼                    ▼                    ▼
┌──────────────────┐  ┌──────────────┐  ┌──────────────────────┐
│ NebulaGraph v3   │  │ PostgreSQL   │  │ Kafka (franz-go)     │
│ 图拓扑存储        │  │ pgvector     │  │ - mutong30 (资源)    │
│ - 节点: Tag      │  │ 结构化+向量  │  │ - mutong_trace_01    │
│ - 边: Edge Type  │  │ GORM ORM    │  │ - business-workloads  │
└──────────────────┘  └──────────────┘  └──────────────────────┘

          ┌────────────────────┬────────────────────┐
          ▼                    ▼                    ▼
┌──────────────────┐  ┌──────────────┐  ┌──────────────────────┐
│ Kubernetes API   │  │ Prometheus   │  │ Elasticsearch        │
│ client-go 0.34   │  │ 指标查询     │  │ 日志查询              │
│ Dynamic Informer │  │ PromQL      │  │ k8s-logs-* 索引      │
└──────────────────┘  └──────────────┘  └──────────────────────┘
```

### 2.2 分层架构

采用经典的 Controller → Service → Model → Interface 分层架构：

- **Controllers (控制器层)**: 处理 HTTP 请求，参数校验，调用 Service 返回 JSON。26 个文件（含 oauth2/ 子包共 31 个）。
- **Services (服务层)**: 核心业务逻辑，编排多个数据源和处理步骤。
- **Models (模型层)**: 数据结构定义，GORM 模型，业务实体。
- **Interfaces (接口层)**: 依赖反转，所有服务依赖接口而非具体实现。

---

## 三、前端架构

### 3.1 页面清单（15页）

| 序号 | 页面 | 入口文件 | 路由 | 功能描述 |
|------|------|----------|------|----------|
| 1 | 仪表盘 | `src/index.html` | `/view/index.html` | 资源概览、集群统计、健康概览 |
| 2 | 拓扑图 | `src/topology/index.html` | `/view/topology.html` | K8s 资源拓扑交互式可视化（flex布局body） |
| 3 | 资源列表 | `src/resource-table.html` | `/view/resource-table.html` | 资源表格展示，多条件筛选，退场资源过滤 |
| 4 | 告警管理 | `src/alerts/index.html` | `/view/alerts.html` | 活跃告警列表、详情、抑制状态 |
| 5 | AI 诊断 | `src/diagnosis/index.html` | `/view/diagnosis.html` | AI 诊断交互（flex布局），MCP 工具调用追踪 |
| 6 | 巡检报告 | `src/inspection/index.html` | `/view/inspection.html` | 立即执行巡检 + 展示最新报告 |
| 7 | 巡检历史 | `src/inspection-history/index.html` | `/view/inspection-history.html` | 历史报告列表、筛选、详情、对比、趋势 |
| 8 | 复盘分析 | `src/retrospective/index.html` | `/view/retrospective.html` | 业务拓扑/指标/日志/因果DAG/影响评估 |
| 9 | 复盘历史 | `src/retrospective-history/index.html` | `/view/retrospective-history.html` | 历史报告筛选、详情、编辑、导出 |
| 10 | 日志查询 | `src/logs/index.html` | `/view/logs.html` | ES 日志检索 |
| 11 | 监控面板 | `src/monitoring/index.html` | `/view/monitoring.html` | 指标可视化、时序图 |
| 12 | Web 终端 | `src/terminal/index.html` | `/view/terminal.html` | K8s 容器 WebSocket 终端 |
| 13 | 链路追踪 | `src/trace/index.html` | `/view/trace.html` | 分布式追踪查看 |
| 14 | 集群管理 | `src/clusters/index.html` | `/view/clusters.html` | 集群健康与统计 |
| 15 | 系统状态 | `src/status/index.html` | `/view/status.html` | 服务组件状态监控 |

### 3.2 共享组件

| 组件 | 文件 | 说明 |
|------|------|------|
| NavBar | `src/components/SharedComponents.js` | 全局导航栏, sticky定位, 含登出/登录入口 |
| StatCard | `src/components/StatCard.vue` | 统计卡片复用组件 |

### 3.3 CSS 文件

| CSS 文件 | 应用页面 | 说明 |
|----------|----------|------|
| `src/styles/common.css` | 全局（所有页面导入） | 通用样式、NavBar sticky、布局重置 |
| `src/topology/topology.css` | 拓扑图页面 | 拓扑图交互样式 |
| `src/diagnosis/diagnosis.css` | AI 诊断页面 | 诊断面板、MCP工具追踪样式 |
| `src/monitoring/monitoring.css` | 监控面板页面 | 指标可视化、时序图样式 |
| `src/resource-table.css` | 资源表格页面 | 表格、筛选、退场资源标记样式 |
| `src/retrospective-history/retrospective-history.css` | 历史复盘页面 | 历史列表、详情、编辑样式 |
| `src/terminal/terminal.css` | Web 终端页面 | 终端容器、resize 样式 |

### 3.4 NavBar 粘性布局

NavBar 使用 `position: sticky; top: 0; z-index: 100` 实现粘性导航。拓扑图页面和诊断页面使用 flex 布局 body 配合 NavBar：

```css
/* common.css */
.navbar {
  position: sticky;
  top: 0;
  z-index: 100;
}
```

拓扑图页面 body 使用 flex 布局确保 NavBar 正常粘性定位。所有页面均导入 `common.css`。

### 3.5 Vite 多页面配置

```javascript
// vite.config.js - 15 个入口
export default defineConfig({
  root: resolve(__dirname, 'src'),
  base: '/view/',
  plugins: [vue()],
  build: {
    rollupOptions: {
      input: {
        index: 'src/index.html',
        topology: 'src/topology/index.html',
        alerts: 'src/alerts/index.html',
        inspection: 'src/inspection/index.html',
        'inspection-history': 'src/inspection-history/index.html',
        'resource-table': 'src/resource-table.html',
        status: 'src/status/index.html',
        logs: 'src/logs/index.html',
        terminal: 'src/terminal/index.html',
        trace: 'src/trace/index.html',
        monitoring: 'src/monitoring/index.html',
        diagnosis: 'src/diagnosis/index.html',
        retrospective: 'src/retrospective/index.html',
        'retrospective-history': 'src/retrospective-history/index.html',
        clusters: 'src/clusters/index.html',
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:8888',
      '/k8s': 'http://localhost:8888',
      '/metrics': 'http://localhost:8888',
    },
  },
})
```

### 3.6 前端依赖

| 库 | 说明 |
|----|------|
| vue | Vue 3 (runtime + compiler) |
| @antv/g6 | 2D 交互拓扑图 |
| force-graph | 3D 力导向图 |
| d3 | 自定义 SVG 布局 |
| marked | Markdown 渲染 |

---

## 四、核心模块设计

### 4.1 告警服务 (alert)

#### 文件结构

```
services/alert/
├── alert_service.go     # 核心服务：Process 管道编排、20并发worker
├── enricher.go          # 拓扑富化：UID/标签/拓扑缓存查询
├── suppressor.go        # 抑制器：拓扑感知 + 因果链双重抑制
├── router.go            # 路由器：Owner/Stakeholder 双维度分派
├── notifier.go          # 通知器：Slack/PagerDuty/钉钉/邮件
├── aggregator.go        # 聚合器：窗口聚合 + 批量通知
├── metrics.go           # Prometheus 指标暴露
├── storage.go           # 内存存储实现 (MemoryAlertStorage)
├── pg_storage.go        # PostgreSQL 存储实现 (PgAlertStorage)
├── *_test.go           # 测试文件
```

#### 关键结构体

```go
// AlertService - 告警管道核心
type AlertService struct {
    logger     interfaces.Logger
    graphDB    interfaces.GraphDB
    enricher   alert_interfaces.AlertEnricher
    suppressor alert_interfaces.AlertSuppressor
    router     alert_interfaces.AlertRouter
    notifier   alert_interfaces.AlertNotifier
    storage    alert_interfaces.AlertStorage
    metrics    *AlertMetrics
    aggregator *AlertAggregator
    autoDiagnosisPipeline AutoDiagnosisPipelineInterface
    diagnosisCacheInvalidator DiagnosisCacheInvalidator
    db         *gorm.DB
}

// AlertEnricher - 拓扑富化器
type AlertEnricher struct {
    logger         interfaces.Logger
    graphDB        interfaces.GraphDB
    topologyCache  *bigcache.BigCache
    bizCtxProvider interfaces.BusinessContextProvider
}

// AlertSuppressor - 抑制器
type AlertSuppressor struct {
    logger           interfaces.Logger
    graphDB          interfaces.GraphDB
    activeAlerts     map[string]*alert_models.EnrichedAlert
    bizAppAlertIndex map[string]map[string]struct{}
    config           SuppressionConfig
}

// AlertRouter - 路由器
type AlertRouter struct {
    logger interfaces.Logger
    config RoutingConfig
}

// SuppressionConfig - 抑制配置
type SuppressionConfig struct {
    TimeWindowSeconds     int      // 300 秒
    MaxDepth              int      // 最大抑制深度 3
    SeverityExceptions    []string // 不抑制的严重级别
    CriticalityExceptions []string
    MaxAlertAgeMinutes    int      // 120
    CleanupIntervalSec    int      // 60
}
```

#### 核心方法

| 方法 | 说明 |
|------|------|
| `Process(ctx, payload)` | 告警处理主入口，20并发worker处理 |
| `processSingleAlert(ctx, a)` | 单条告警管道: Enrich→Suppress→Route→Notify |
| `HandleWebhook(ctx, payload)` | Alertmanager Webhook 处理 |
| `GetActiveAlerts(ctx, filters)` | 获取活跃告警列表 |
| `GetAlertByFingerprint(ctx, fp)` | 按指纹获取告警详情 |
| `GetStatus()` | 获取服务状态与指标 |
| `GetSuppressionStatus()` | 获取抑制器状态 |
| `StartCleanupScheduler()` | 启动定时清理 |
| `StartStatsSync(interval)` | 启动DB统计同步 |
| `StartAggregator()` | 启动聚合调度器 |

#### 处理流程

```
Alertmanager Webhook
        │
        ▼
┌─────────────────┐
│  AlertService   │
│  Process()      │  20并发worker
│  ┌─────────────┐│
│  │ per-alert   ││
│  │ 1. Enrich   ││──► 查询NebulaGraph拓扑关系
│  │    ↓        ││    富化ResourceUID/Type/Name/Namespace
│  │ 2. Suppress ││    应用BusinessContext
│  │    ↓        ││    ──
│  │ 3. Route    ││──► 拓扑感知抑制 (same_node/calls_app_upstream)
│  │    ↓        ││    因果链抑制 (Kafka→Consumer级联)
│  │ 4. Save     ││    ──
│  │    ↓        ││──► Owner路由 (按team/service)
│  │ 5. Notify   ││    Stakeholder路由 (受影响业务方)
│  │    ↓        ││    严重级别→渠道映射 (critical→PagerDuty)
│  │ 6. Auto     ││    ──
│  └─────────────┘│──► 写入存储 (内存/PostgreSQL)
└─────────────────┘    ──
                       Slack/PagerDuty/钉钉/邮件通知
                       ──
                       触发自动诊断 (AutoDiagnosisPipeline)
```

#### 富化策略 (enricher.go)

富化采用三级降级策略：

1. **UID优先** (仅Pod): 使用 Prometheus relabel 提供的 `kubernetes_uid` 直接查询
2. **标签匹配**: 提取 K8sLabels (ResourceKind/Name/Namespace) 进行 NebulaGraph 查询
3. **业务上下文**: 调用 BusinessContextProvider 获取团队/关键度/环境信息

拓扑缓存使用 BigCache，TTL 10分钟，减少 NebulaGraph 重复查询。

#### 抑制策略 (suppressor.go)

双重抑制机制：

1. **拓扑感知抑制** - 同作用域内自动抑制衍生告警：
   - `same_node`: 同节点上 Pod→Node→Node级联抑制
   - `calls_app_upstream`: Kafka 告警抑制上游 Consumer 超时告警
   - `same_owner`: 同 Deployment 下 Pod 告警合并

2. **因果链抑制** - 识别上下游因果关系，key 告警抑制级联告警:
   - 时间窗口: 300秒
   - 最大深度: 3
   - critical 级别永不被抑制

#### 路由策略 (router.go)

双维度路由：

1. **Owner 路由** - 中间件告警通知负责方：
   - 按 `team` 标签 → TeamRouting 映射
   - 按 `service` 标签 → ServiceRouting 映射
   - 按 BusinessContext.Team → BusinessContextRouting

2. **Stakeholder 路由** - 基础设施告警通知受影响业务方：
   - Node 告警 → 查询节点上运行的 Pod → 通知所有业务Owner
   - 严重级别 → 渠道规则映射 (critical→PagerDuty, error→Slack)

#### 通知器 (notifier.go)

支持4种通知渠道：

| 渠道 | 配置项 | 说明 |
|------|--------|------|
| Slack | slackWebhookUrl | Webhook 推送 |
| PagerDuty | pagerdutyApiKey | 关键告警 |
| 钉钉 | dingtalkWebhook | Webhook 推送 |
| 邮件 | emailConfig (SMTP) | 标准邮件通知 |

支持聚合通知：按窗口合并告警批量发送。

---

### 4.2 诊断服务 (diagnosis)

#### 文件结构

```
services/diagnosis/
├── engine.go            # 诊断引擎：规则→置信度→LLM降级
├── eino_agent.go        # Eino ADK Runner (ReAct Agent + SafeTool + ModelRetryConfig + Callback)
├── eino_provider.go     # Eino LLM Provider 适配
├── eino_tools.go        # MCP 工具注册为 InvokableTool (含日志)
├── llm_http.go          # HTTP LLM Provider (OpenAI兼容)
├── eino_model_wrapper.go # TokenTrackingChatModel 包装器 (token追踪+日志)
├── llm_mock.go          # LLM Mock 实现
├── topology.go           # 拓扑查询 (NebulaGraph)
├── impact.go             # 三层影响评估
├── knowledge_base.go     # 知识库管理
├── hybrid_retriever.go   # pgvector语义 + NebulaGraph拓扑双路检索
├── vector_retriever.go   # pgvector 向量语义检索
├── context_collector.go  # 上下文收集并行管道
├── chat_session.go       # 聊天诊断会话管理
├── redis_storage.go      # Redis 会话存储
├── pg_storage.go         # PostgreSQL 诊断结果存储
├── tool_selector.go      # 智能工具选择器
├── tool_converter.go     # MCP工具→Eino工具转换器
├── tool_examples.go      # 工具使用示例
├── metrics.go            # 诊断服务 Prometheus 指标
├── *_test.go            # 测试文件
```

#### 关键结构体

```go
// Engine - 诊断引擎
type Engine struct {
    logger              interfaces.Logger
    graphDB             interfaces.GraphDB
    alertStorage        alert_interfaces.AlertProcessor
    knowledgeBase       *KnowledgeBase
    llmProvider         interfaces.LLMProvider
    metricsQuerier      interfaces.MetricsQuerier
    logQuerier          interfaces.LogQuerier
    confidenceThreshold float64      // 0.8
    querier             *TopologyQuerier
    assessor            *ImpactAssessor
    db                  *gorm.DB
    metrics             *DiagnosisMetrics
    agent               *DiagnosisAgent     // Eino Agent
    vectorRetriever     *VectorRetriever
    hybridRetriever     *HybridRetriever
    collector           *ContextCollector
}

// DiagnosisAgent - Eino ReAct Agent
type DiagnosisAgent struct {
    chatModel model.BaseChatModel
    tools     []tool.InvokableTool
    timeout   time.Duration    // 120s
    maxSteps  int              // 10
    logger    interfaces.Logger
}

// HybridRetriever - 混合检索器
type HybridRetriever struct {
    logger          interfaces.Logger
    vectorRetriever *VectorRetriever
    topoQuerier     *TopologyQuerier
    db              *gorm.DB
}

// ContextCollector - 并行上下文收集器
type ContextCollector struct {
    logger          interfaces.Logger
    alertStorage    alert_interfaces.AlertProcessor
    metricsQuerier  interfaces.MetricsQuerier
    logQuerier      interfaces.LogQuerier
    k8sClient       *kubernetes.Clientset
    querier         *TopologyQuerier
    assessor        *ImpactAssessor
    knowledgeBase   *KnowledgeBase
    hybridRetriever *HybridRetriever
    semaphore       chan struct{}
}
```

#### 诊断流程

```
用户请求 (fingerprint/namespace/resource)
        │
        ▼
┌─────────────────────────────────────────┐
│           DiagnosisEngine.Diagnose()     │
│                                          │
│  ① 获取告警 → 提取 K8s 标签上下文        │
│       │                                  │
│       ▼                                  │
│  ② ContextCollector 并行采集证据：        │
│     ┌──────────┬──────────┬────────────┐ │
│     │ Topology │ Metrics  │ Logs       │ │
│     │ Snapshot │ Snapshot │ Collection │ │
│     └──────────┴──────────┴────────────┘ │
│       │                                  │
│       ▼                                  │
│  ③ HybridRetriever 混合检索：            │
│     ┌──────────────┬──────────────────┐  │
│     │ pgvector语义 │ NebulaGraph拓扑查询│  │
│     │ (向量相似度) │ (同Deployment/Node)│  │
│     └──────┬───────┴────────┬─────────┘  │
│            └──── RRFFusion ─┘            │
│       │                                  │
│       ▼                                  │
│  ④ 规则诊断 (KnowledgeBase 匹配)：       │
│     - 多维度自动置信度评分                │
│     - 规则诊断分 + 关联告警 + 业务关键度  │
│       │                                  │
│  ┌────▼─────┐                            │
│  │置信度≥阈值│──Yes──► 直接返回结果       │
│  └────┬─────┘                            │
│       │No                                │
│       ▼                                  │
│  ⑤ Eino ReAct Agent (LLM 深度分析)：     │
│     - Function Calling 自动调用MCP工具    │
│     - 多轮推理 (maxSteps=10)             │
│     - LLM不可用时自动降级到纯规则         │
│       │                                  │
│       ▼                                  │
│  ⑥ 持久化结果 → PostgreSQL + pgvector    │
│     缓存 (TTL=900s) → Redis/BigCache     │
└─────────────────────────────────────────┘
```

#### Eino ReAct Agent

使用 cloudwego/eino ADK 框架构建的 ReAct Agent，通过 `adk.NewRunner` 执行（非直接 `agent.Run()`）：

- **执行方式**: `adk.NewRunner` + `runner.Run()`，保证全流程 `EnableStreaming` 一致
- **MaxIterations**: 10 轮
- **Timeout**: 120 秒（内部），外层 180 秒兜底
- **Tool 集合**: 20 个 MCP 工具全量注册为 InvokableTool（不再按场景过滤）
- **容错机制**:
  - `ModelRetryConfig`: LLM 调用失败自动重试（MaxRetries=3，指数退避 2s/4s/6s）
  - `safeToolHandler`: 工具执行错误转为 `[Tool Error]` 文本返回给 LLM，不中断 Agent 流程
  - `event.Err` 处理：记录 Warn 日志但不终止，让 Eino 内部自行处理
- **可观测性**: Agent Callback（OnStart/OnEnd）分阶段 tracing + eventCount 日志
- **流式输出**: `text/event-stream` + `X-Accel-Buffering: no`，SSE 事件对齐 eino-examples（stream_chunk / tool_call / action）
- **System Prompt**: 动态生成，包含拓扑快照、影响评估、知识库匹配

#### HybridRetriever 混合检索

双通路检索融合，同时利用语义和拓扑信息发现历史相似故障：

```
HybridRetriever.Search()
    ├── 路径 1：pgvector 向量语义搜索
    │       └── VectorRetriever.SearchSimilarByFingerprint()
    │           └── SELECT * FROM fault_report_vectors WHERE embedding <=> $vec < 0.65
    │
    ├── 路径 2：NebulaGraph 拓扑结构搜索（仅 Pod 资源生效）
    │       ├── searchBySameOwner() → 同 Deployment 的兄弟 Pod 历史故障
    │       └── searchBySameNode()  → 同 Node 的邻居 Pod 历史故障
    │
    └── 融合排序
            combinedScore = semanticScore × 0.6 + topologicalScore × 0.4
```

**Embedding 配置**：向量化模型独立于对话 LLM，支持跨厂商配置：

```yaml
# configs/config.diagnosis.yaml
diagnosis:
  llm:
    provider: "openai"
    model: "deepseek-v4-pro"           # 对话模型
    apiKey: "sk-deepseek-xxx"
    baseURL: "https://api.deepseek.com"
    embedding:                          # 向量化配置（独立）
      model: "text-embedding-3-small"  # OpenAI embedding
      apiKey: "sk-openai-xxx"          # 为空时复用 llm.apiKey
      baseURL: "https://api.openai.com" # 为空时复用 llm.baseURL
```

不配置 embedding 时，语义搜索路径不可用（`VectorRetriever` 返回空），但图结构搜索路径和复盘报告生成不受影响。

**触发时机**：AI 诊断时 `ContextCollector` 自动调用；复盘知识检索通过 API 参数触发。

**Nebula 索引维护**：当前有效的 TAG INDEX 为 `name_space`、`kind_name_space`、`kind_name`、`kind_name_name_space`、`is_deleted_idx`、`kind_is_deleted_idx`、`label_key`、`label_value`、`label_key_value`、`bizapp_name`、`bizapp_ns`。冗余索引（`resource_by_kind`、`resource_by_ns` 等）已移除。

#### ContextCollector 并行管道

并发采集诊断所需上下文：TopologySnapshot (NebulaGraph)、MetricsSnapshot (ResourceProfiles自定义PromQL)、LogCollection (ES/K8s API)、BusinessCalls (上下游调用链)、ImpactAssessment (三层影响评估)。

#### ResourceProfiles 自定义 PromQL

支持 `{{name}}`、`{{namespace}}` 模板变量替换，可扩展资源类型（Pod/Node/Deployment/Service/PVC/Ingress）。

#### MCP 工具调用日志 (InvokableRun)

每次工具调用记录：执行开始/结果大小/耗时/摘要。`list_resources_from_cache` 自动过滤终止态 Pod (Succeeded/Failed/Unknown) 和 DeletionTimestamp 非空的资源。

---

### 4.3 巡检服务 (inspection)

#### 文件结构

```
services/inspection/
├── inspection_engine.go    # 引擎：规则注册、并行执行
├── inspection_service.go   # 服务：Cron调度、报告管理
├── inspection_reporter.go  # 报告器：报告生成、对比、趋势
├── yaml_engine.go          # YAML声明式规则引擎
└── rules/
    ├── cert_expiry.go          # 证书过期检查
    ├── single_point_failure.go # 单点故障检测
    ├── cmdb_data_silo.go       # CMDB数据孤岛识别
    ├── resource_quota.go       # 资源配额监控
    ├── monitoring_blindspot.go # 监控盲点扫描
    └── image_audit.go          # 镜像审计
```

#### 关键结构体

```go
type InspectionEngine struct {
    logger  interfaces.Logger
    graphDB interfaces.GraphDB
    rules   map[string]interfaces.InspectionRule
}
```

#### 6 类内置规则（均已实现，配置文件默认启用 3 类）

| 规则 | 检查类型 | 说明 |
|------|----------|------|
| cert_expiry | min_rows | 检查 TLS 证书是否即将过期 |
| single_point_failure | min_rows | 检查 Deployment 副本数 ≥ 2 |
| cmdb_data_silo | min_rows | 检查资源是否关联 CMDB 归属 |
| resource_quota | min_rows | 检查节点 Pod 数是否超限 |
| monitoring_blindspot | min_rows | 检查是否有未接入监控的命名空间 |
| image_audit | field_contains | 检查镜像是否符合安全策略 |

支持 3 种检查类型：min_rows、field_contains、command (外部插件)。

Cron 调度：`enabled: true, cronSpec: "0 */6 * * *"` (每6小时执行)，支持手动触发。

---

### 4.4 Prometheus 查询 (prometheus)

#### 文件

`services/prometheus/query_service.go` + cache 实现。

#### 核心方法

| 方法 | 说明 |
|------|------|
| `Query(ctx, promql)` | 即时查询 (Instant Query) |
| `QueryRange(ctx, promql, start, end, step)` | 范围查询 (Range Query) |
| `GetPodMetrics(ctx, pod, ns)` | Pod 指标 (CPU/内存/网络/重启) |
| `GetNodeMetrics(ctx, node)` | Node 指标 |
| `GetDeploymentMetrics(ctx, deploy, ns)` | Deployment 指标 |
| `GetHPAStatus(ctx, deploy, ns)` | HPA 状态查询 |

查询结果使用 BigCache 缓存，支持 ResourceProfiles 自定义 PromQL。

---

### 4.5 执行器服务 (executor)

#### 文件结构

```
services/executor/
├── executor.go       # K8sExecutor 核心
├── auto_diagnosis.go # 自动诊断触发
├── bridge.go         # 诊断→执行桥接
├── pg_store.go       # PostgreSQL 审计存储
├── templates.go      # 操作模板
└── executor_test.go
```

#### 关键结构体

```go
type K8sExecutor struct {
    clientset  *kubernetes.Clientset
    dynamic    dynamic.Interface
}

type ExecutionPlan struct {
    ID           string     // 计划ID
    Action       ActionType // 11 种操作类型（见下表）
    Target       string
    Namespace    string
    ResourceName string
    Reason       string     // AI诊断结论
    Confidence   float64
    Risk         RiskLevel  // low|medium|high
    Replicas     int32
    MinReplicas  int32
    MaxReplicas  int32
    TargetCPU    int32
    TargetMemory int32
    ConfigData   map[string]string // ConfigMap/Secret 数据
    Annotations  map[string]string // 注解操作
    Labels       map[string]string // 标签操作
    Image        string            // 镜像变更
    ContainerName string           // 指定容器
}
```

#### 11 种操作类型

| 操作 | 风险等级 | 自动阈值 | 说明 |
|------|----------|----------|------|
| restart_pod | low | 0.70 | Pod 优雅驱逐重启 (Eviction API) |
| scale_deployment | medium | 0.85 | Deployment 副本扩缩容 |
| delete_pod | high | 1.00 | 强制删除 Pod |
| create_hpa | medium | 0.90 | 创建 HPA |
| update_hpa | medium | 0.85 | 更新 HPA 配置 |
| update_configmap | high | 1.00 | 更新 ConfigMap 数据（需人工审批） |
| update_secret | high | 1.00 | 更新 Secret 数据（需人工审批） |
| update_resource_limits | medium | 0.90 | 调整容器 CPU/Memory 资源限制 |
| update_deployment_image | high | 1.00 | 变更容器镜像（需人工审批） |
| update_annotations / update_labels | medium | 0.85 | 修改资源注解和标签 |

#### 安全防护

- **冷却时间**: 同一资源操作间隔限制
- **重启限制**: Pod 短时间内最大重启次数
- **副本上限**: Deployment 扩容上限
- **手动审批**: 默认手动模式，需审批
- **自动模式**: 基于风险阈值自动执行
- **完整审计**: 操作人/时间/目标/结果/风险写入 PostgreSQL

---

### 4.6 复盘服务 (retrospective)

#### 文件结构

```
services/retrospective/
├── retrospective_service.go       # 核心复盘服务 (1228行)
└── retrospective_service_test.go  # 测试
```

#### 关键结构体

```go
type Service struct {
    logger          interfaces.Logger
    graphDB         interfaces.GraphDB
    alertStorage    alert_interfaces.AlertProcessor
    db              *gorm.DB
    llmProvider     interfaces.LLMProvider
    diagCache       DiagnosisCache
    hybridRetriever *diagnosis_svc.HybridRetriever
}
```

#### 核心方法

| 方法 | 说明 |
|------|------|
| `BuildTimeline(ctx, fingerprint)` | 构建事件时间线 (MTTD标注) |
| `AnalyzeCausalChain(ctx, fingerprint)` | 因果链DAG分析 |
| `GeneratePostmortem(ctx, fingerprint)` | 生成LLM复盘报告 |
| `UpdatePostmortem(ctx, fingerprint, report)` | 人工修正报告 (UPSERT) |
| `SearchKnowledge(ctx, query, limit)` | 图增强混合检索故障知识 |
| `GetPostmortemsByResource(ctx, resourceUID)` | 按资源UID回溯历史复盘 |
| `enrichFromDiagnosisCache()` | 从诊断缓存提取完整上下文 |
| `enrichWithLLM()` | LLM 三段式反思 (WhatWentWell/Wrong/ContributingFactors) |

#### 复盘报告包含

- **事件时间线**: 基于告警指纹的时间线，含 MTTD 延迟
- **因果链 DAG**: 可视化故障传播路径
- **业务拓扑图**: G6 渲染业务调用链 (故障红色→上游橙色→下游蓝色)
- **影响评估**: 爆炸半径 + 三层分层影响表
- **指标快照**: CSS 条形图展示诊断时的 Prometheus 指标
- **关键日志**: 可折叠 ES 日志卡片 (ERROR红色/WARN橙色/INFO蓝色)
- **改进项看板**: Prevent/Detect/Mitigate 三列看板，含 ExitCriteria+Owner+DueDate
- **图增强混合检索**: pgvector + NebulaGraph 双路历史案例发现

---

### 4.7 日志搜索 (logsearch)

ES 日志查询服务。核心方法：`SearchLogs` (关键词搜索)、`GetPodLogs` (Pod 日志ES)、`GetErrorLogs` (Error级别)、`GetWarnLogs` (Warn级别)、`GetInfoLogs` (Info级别)、`SearchLogsByBusiness` (按业务上下文跨 namespace 搜索)。索引模式: `k8s-logs-*`，支持 `serviceToIndex` 多索引路由（如 Kafka→`kafka-logs-*`），按 `kubernetes.namespace_name`、`kubernetes.pod_name`、`kubernetes.container_name`、`level` 字段过滤。

---

### 4.8 MCP 服务器 (mcp)

#### 文件

`services/mcp/server.go` (1429行) - 单一文件实现

#### 完整工具列表 (19个基础 + 1个条件启用)

| # | 工具名 | 说明 |
|---|--------|------|
| 1 | `query_topology` | NebulaGraph 查询K8s资源拓扑关系 (1-3跳) |
| 2 | `get_active_alerts` | 活跃告警列表 (分页/筛选) |
| 3 | `get_alert_detail` | 按指纹获取告警详情 |
| 4 | `run_diagnosis` | 执行AI诊断 |
| 5 | `inspect_resource` | 检查K8s资源实时状态 + Prometheus指标 |
| 6 | `get_inspection_report` | 最新巡检报告 |
| 7 | `list_resources_from_graph` | NebulaGraph 查询资源清单 |
| 8 | `list_k8s_resources` | K8s API 实时查询资源 (过滤终止Pod) |
| 9 | `list_resources_from_cache` | Informer 缓存查询 (秒级新鲜度, 过滤Succeeded/Failed/Unknown) |
| 10 | `get_resource_metrics` | 资源当前指标 (CPU/内存/网络) |
| 11 | `query_metric_timeseries` | PromQL 时序数据 |
| 12 | `get_system_health` | 系统组件健康状态 |
| 13 | `get_metric_catalog` | 可用指标目录 |
| 14 | `get_pod_logs` | K8s API 获取 Pod 日志 |
| 15 | `get_pod_logs_es` | ES 获取 Pod 历史日志 |
| 16 | `search_logs` | ES 关键词全文搜索 |
| 17 | `get_error_logs` | ES Error 日志 |
| 18 | `search_similar_cases` | pgvector 向量相似历史案例 |
| 19 | `list_alerts` | 活跃告警简化列表 |
| 20 | `generate_retrospective` | 故障复盘报告生成 |

#### ExecuteTool 日志

每次工具调用记录: 开始 (tool+args)、成功 (tool+result_len+duration+summary)、失败 (tool+duration+error)。`summary` 截取结果前200字符。

#### list_resources_from_cache 过滤

自动过滤终止态 Pod：`phase == Succeeded/Failed/Unknown` → 排除；`DeletionTimestamp != nil` → 排除。

#### list_k8s_resources 过滤

从 K8s API 查询时自动过滤 `phase == Succeeded/Failed` 的 Pod。

---

### 4.9 终端服务 (terminal)

WebSocket PTY 会话服务：WebSocket 升级、Pod 内多容器自由切换、终端 resize 事件同步、独立会话生命周期管理。使用 SPDY 协议连接 K8s exec API。

---

### 4.10 追踪服务 (trace)

OTel 查询服务，支持 `QueryTraces`、`GetServices`、`GetTraceDetail`，兼容 SkyWalking OAP 查询。

---

### 4.11 业务标签同步 (business_label_syncer)

Kafka Consumer 服务，消费 `business-workloads` Topic：从 K8s 标签 (`app.kubernetes.io/name`、`app.kubernetes.io/part-of`、`owner`) 提取业务归属，支持 WorkloadKinds: Deployment, StatefulSet, DaemonSet, Job, CronJob。

---

### 4.12 Trace拓扑同步 (trace_topology_syncer)

SkyWalking Consumer 服务，消费 `mutong_trace_01` Topic：解析 OTLP Traces → 构建调用关系 (Beyla eBPF → OTel Collector → Kafka) → 写入 NebulaGraph。

---

### 4.13 基础设施服务 (k8sresource)

#### 文件结构

```
services/
├── k8sresource_service.go        # 核心资源服务 (实现 K8sResourceInterface)
├── k8sresource_collector.go      # 资源采集 (Informer + API)
├── k8sresource_kafka.go          # Kafka 消息消费 (50MB消息, 5次重试)
├── k8sresource_nebula.go         # NebulaGraph 写入 (节点+边)
├── k8sresource_relationships.go  # 资源关系识别 (30+关系类型)
├── getResource.go                # 单资源查询
├── query_builder.go              # nGQL 查询构建器 (安全注入防护)
├── ngql_sanitizer.go             # nGQL 注入清洗器
├── resource_cache.go             # Informer 本地缓存
├── namespace_mapper.go           # 命名空间→业务映射
├── business_label_syncer.go      # 业务标签同步
├── business_topology_service.go  # 业务拓扑服务
├── trace_topology_syncer.go      # Trace拓扑同步
├── user_service.go               # 用户服务
├── role_service.go               # 角色服务
```

#### 关键方法

| 方法 | 说明 |
|------|------|
| `Get(RequestContext)` | 获取指定资源详情 |
| `Collect(namespace)` | 全量资源采集 |
| `Relationship(namespace)` | 构建拓扑关系 |
| `GetAllResources(ctx)` | 获取所有资源节点 |
| `GetAllRelationships(ctx)` | 获取所有边关系 |
| `GetKindsAndNamespaces(ctx)` | 获取类型/命名空间元数据 |
| `SearchResourceRelationship(ctx)` | 按条件搜索资源关系 |
| `GetResourceDefine(ctx)` | 获取资源类型定义 |
| `SuggestResources(ctx)` | 资源名称补全建议 |
| `ConsumeKafkaMessages()` | 消费 Kafka 消息 |
| `CollectWithContext(ctx, ns)` | 带上下文的采集 |
| `Stop()` | 优雅停止 |

#### 资源关系类型

完整 Schema 定义见 [`docs/ngql.md`](ngql.md)（TAG、EDGE、INDEX 的 DDL 语句及常用查询示例）。核心边类型：

| 关系 | 源 → 目标 | 状态 |
|------|----------|------|
| OwnedBy | Pod → ReplicaSet → Deployment / StatefulSet / DaemonSet / Job / CronJob | ✅ |
| BelongsTo | Resource → Namespace | ✅ |
| RunsOn | Pod → Node | ✅ |
| SvcToPods | Service → Pod (via Endpoints/EndpointSlice) | ✅ |
| SvcToEp / EpToPods / EpSliceToPods | Service ↔ Endpoints ↔ Pod | ✅ |
| MountsPVC | Pod → PVC | ✅ |
| MountsConfig | Pod/Ingress → ConfigMap | ✅ |
| MountsSecret | Pod/Ingress → Secret | ✅ |
| BoundToPV | PVC → PV | ✅ |
| BelongsToStorageClass | PV → StorageClass | ✅ |
| ScRefCSIDriver | StorageClass → CSIDriver | ✅ |
| RegisteredON | CSINode → Node | ✅ |
| BelongsToIngressClass / RoutesToSvc / UsesTLS | Ingress 相关 | ✅ |
| AutoScales | HPA → ScaleTarget (含 min/max/current replicas) | ✅ |
| BelongsToApp | K8sResource → BusinessApp | ✅ |
| CallsApp | BusinessApp → BusinessApp | ✅ |
| Events | K8sResource → Event (含 reason/note/type，TTL 24h) | ✅ |

#### 标签过滤

支持按标签排除：`app.kubernetes.io/managed-by: Helm`、`app.kubernetes.io/component: controller-eligible`、`apps.kubernetes.io/pod-index`。

### 4.14 CLI 工具 (mutongctl)

`mutongctl` 是基于 Cobra + Viper 的命令行工具，提供与 Web UI 对等的运维能力，适用于 CI/CD 集成、批量脚本和值班快速排查。

#### 设计原则

| # | 原则 | 说明 |
|---|------|------|
| 1 | **动词-资源命名**（kubectl 风格） | K8s 资源类型增长但操作集封闭：`get/list` + 6 种资源 |
| 2 | **默认 JSON，人性化可选** | 管道默认 JSON（结构化），TTY 默认表格（人类可读） |
| 3 | **stderr ≠ stdout** | 数据走 stdout，日志/错误/进度走 stderr——永不相混 |
| 4 | **零副作用默认** | 查询不改变状态，变更需 `--confirm` |
| 5 | **一条命令做完一件事** | 避免多步交互式流程 |

**行业借鉴**：kubectl（动词-资源命名 + 动态补全）、gh（`--json --jq` 内建筛选 + runF 测试注入）、docker（双轨制命令）、stern（流式日志）、aws-cli（服务自动发现）。

#### 命令架构

```
mutong
├── get      <resource> [name]          # 资源详情
├── list     <resource>                 # 资源列表（pods/nodes/deployments/statefulsets/daemonsets/services）
├── topo     get|search                 # 拓扑
├── alert    list|get|suppression       # 告警
├── diagnose run|status                 # 诊断
├── inspect  run|report|list|get|compare|trend  # 巡检
├── retro    generate|list|get|export|search|timeline|causal-chain  # 复盘
├── logs     pod|search|errors          # 日志
├── metrics  resource|timeseries        # 指标（pod/node/deployment/statefulset/daemonset/service）
├── cluster  list|health|stats          # 集群
├── system   status|health              # 系统
├── terminal [--pod]                    # WebSocket 终端（仅人类交互）
├── exec     execute|audit|status       # 执行器
├── biz      apps|graph                 # 业务拓扑
├── stats    overview|sync              # 统计
├── debug    info                       # 环境诊断
├── config   set|get|contexts|use-context  # 配置
└── completion <shell>                  # 补全（bash/zsh/fish/powershell）
```

#### 认证链

```
auth login (OAuth2) → ~/.mutong/tokens.json → --token flag → MUTONG_TOKEN 环境变量
```

多环境切换：`~/.mutong/config.yaml` 中定义多个 context，通过 `--context` flag 或 `config use-context` 切换。

#### 全局选项

| 选项 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| `--server` | `-s` | 服务地址 | `MUTONG_SERVER` env 或 `http://localhost:8888` |
| `--token` | `-t` | 认证 Token | `MUTONG_TOKEN` env 或 `~/.mutong/config.yaml` |
| `--output` | `-o` | 输出格式：`json`/`table`/`yaml`/`md` | TTY=table, 管道=json |
| `--no-color` | — | 禁用颜色 | 管道时自动 true |
| `--confirm` | — | 变更操作确认 | false |

> **注**：`--namespace` (-n) 等资源相关标志由各命令独立定义，非全局选项。

#### 退出码

| 退出码 | 含义 |
|--------|------|
| 0 | 成功 |
| 1 | 一般错误（API 返回非预期状态） |
| 2 | 命令使用错误（flag 缺失/格式错误） |
| 3 | 认证失败 |
| 4 | 网络错误（连接超时/拒绝） |
| 5 | 资源不存在 |

#### 实现模式（gh 风格三段式）

```go
// Step 1: Options 结构体——持有所有依赖和 flag 值
type ListPodsOptions struct {
    IO         *iostreams.IOStreams
    HttpClient *client.Client
    Namespace  string
    Format     string
}

// Step 2: NewCmd——绑定 flags + 注册补全
func NewCmdListPods(f *cmdutil.Factory, runF func(*ListPodsOptions) error) *cobra.Command { ... }

// Step 3: Run——纯业务逻辑，零 Cobra 依赖
func listPodsRun(opts *ListPodsOptions) error { ... }
```

`runF` 注入允许测试直接注入 mock 函数，无需 HTTP server。

#### 目录结构

```
cmd/mutongctl/
├── main.go              # 入口 + rootCmd
├── root.go              # 全局 flags 注册 + Factory 依赖注入
├── get/ alert/ list/ inspect/ diagnose/ retro/ logs/ metrics/
├── cluster/ system/ topo/ exec/ biz/ stats/ terminal/ config/
└── internal/
    ├── api/              # API 客户端封装（13 个资源模块）
    ├── client/           # HTTP client （对接所有 /api/v1/* 端点）
    ├── iostreams/        # IO 流抽象（stdin/stdout/stderr + TTY 检测）
    ├── config/           # ~/.mutong/config.yaml 读写
    └── format/           # 多格式输出（json/table/yaml/markdown）
```

#### Agent Skills 集成

13 个 opencode Skill 定义，供 AI Agent 通过 Function Calling 自动调用 CLI。典型排查流程：

```
用户: "生产环境 nginx 为什么频繁重启？"

Agent 执行链:
1. mutongctl alert list --namespace production -o json     → 找到相关告警
2. mutongctl diagnose run --fingerprint <fp> -o json       → AI 诊断根因
3. mutongctl metrics pod nginx-xxx -n production -o json   → 查看资源指标
4. mutongctl logs pod nginx-xxx -n production --tail 50     → 查看最近日志
5. 综合以上信息，回复用户
```

#### 设计决策记录

| 决策 | 选项 | 选择 | 理由 |
|------|------|------|------|
| 命名模式 | 动词-资源 / 资源子命令 | **动词-资源** | K8s 资源类型增长但操作封闭 |
| 单复数 | list pods / list pod | **list pods** | 与 kubectl 一致 |
| 输出默认 | 表格 / JSON | **TTY=table, 管道=json** | 人机兼顾 |
| 命令层级 | 扁平 / 嵌套2层 | **嵌套2层** | 平衡可发现性和简洁性 |
| 认证配置 | flag / env / config | **flag > env > config** | 灵活性最高 |
| jq 集成 | 内置 / 依赖外部 jq | **内置 gojq** | 零外部依赖 |
| 测试注入 | runF 函数注入 | **gh 风格 runF** | 单元测试无需 HTTP mock |

---

## 五、数据模型

### 5.1 告警模型 (models/alert/)

```go
type Alert struct {
    Status       string            // firing | resolved
    Labels       map[string]string
    Annotations  map[string]string
    StartsAt     time.Time
    EndsAt       time.Time
    GeneratorURL string
    Fingerprint  string
}

type WebhookPayload struct {
    Version           string
    GroupKey          string
    Status            string
    Receiver          string
    GroupLabels       map[string]string
    CommonLabels      map[string]string
    CommonAnnotations map[string]string
    ExternalURL       string
    Alerts            []Alert
}

type EnrichedAlert struct {
    Alert
    ResourceUID     string
    ResourceType    string           // Pod/Node/Service/...
    ResourceName    string
    Namespace       string
    NodeName        string
    OwnerKind       string
    OwnerName       string
    RelatedAlerts   []string
    TopologyPath    []string
    EnrichTags      map[string]string
    BusinessContext BusinessContext
    BusinessImpact  BusinessImpact
    BusinessCalls   *BusinessAppCalls
    Stakeholders    []StakeholderInfo
}

type BusinessContext struct {
    UID, AppName, Namespace, Criticality, Environment, Team, BusinessUnit, Source, ServiceType string
}

type SuppressionResult struct {
    IsSuppressed      bool
    SuppressionReason string
    SuppressedBy      []string
    SuppressedAt      time.Time
}

type RoutingResult struct {
    Receiver           string
    NotifyChannel      string
    Severity           string
    Priority           int
    NotifyUsers        []string
    NotifyGroups       []string
    StakeholderNotices []StakeholderNotice
}

type ProcessedAlert struct {
    EnrichedAlert
    Suppression SuppressionResult
    Routing     RoutingResult
    ProcessedAt time.Time
    RuleChainID string
}
```

### 5.2 诊断模型 (models/diagnosis/)

```go
type DiagnosisRequest struct {
    Fingerprint  string
    Namespace    string
    Resource     string
    Query        string
    ResourceKind string
    ResourceName string
    AlertName    string
    Severity     string
}

type DiagnosisResult struct {
    ID                    string
    Timestamp             time.Time
    Request               DiagnosisRequest
    RootCauses            []RootCause
    Impact                ImpactAssessment
    Remediations          []RemediationSuggestion
    TopologySnapshot      *TopologySnapshot
    RelatedAlerts         []string
    Summary               string
    Metrics               []MetricEntry
    RecentLogs            []LogEntry
}

type RootCause struct {
    ResourceType string
    ResourceName string
    Namespace    string
    Confidence   float64
    Evidence     []string
}

type ImpactAssessment struct {
    Severity           string
    BlastRadius        int
    AffectedResources  []string
    DirectImpact       []ResourceRef
    IndirectImpact     []ResourceRef
    PotentialImpact    []ResourceRef
    AffectedServices   []string
    AffectedIngresses  []string
    UserFacingImpact   bool
    BusinessContext    string
    BusinessImpact     *BusinessImpactAnalysis
}

type RemediationSuggestion struct {
    Action      string
    Description string
    Steps       []string
    RiskLevel   string
    AutoFixable bool
    HPA         *HPARecommendation
}

type TopologySnapshot struct {
    Nodes []TopologyNode
    Edges []TopologyEdge
}

type TopologyNode struct {
    UID, Kind, Name, Namespace string
    IsDeleted bool
}

type TopologyEdge struct {
    From, To, Type string
    Rank int64
}
```

### 5.3 执行器模型 (models/executor/)

```go
type ActionType string  // restart_pod | scale_deployment | delete_pod | create_hpa | update_hpa
type RiskLevel string   // low | medium | high

type ExecutionPlan struct {
    ID, Action, Target, Namespace, ResourceName, Reason string
    Confidence float64
    Risk       RiskLevel
    ApprovedBy string
    Replicas, MinReplicas, MaxReplicas, TargetCPU, TargetMemory int32
    CreatedAt time.Time
}

type ExecutionResult struct {
    Success   bool
    Message   string
    AuditID   string
    Timestamp time.Time
}

type AuditLog struct {
    ID           string
    Plan         ExecutionPlan
    Result       ExecutionResult
    AutoExecuted bool
    ApprovedBy   string
    Timestamp    time.Time
}
```

### 5.4 复盘模型 (models/retrospective/)

```go
type TimelineEvent struct {
    Timestamp   time.Time
    EventType   string    // alert_triggered | related_alert | diagnosis | remediation
    Description string
    Source      string
    Severity    string
    Resource    string
}

type CausalLink struct {
    Cause, Effect string
    Confidence    float64
    Evidence      []string
}

type CausalChain struct {
    RootCause string
    Links     []CausalLink
    Impact    string
}

type PostmortemReport struct {
    ID, IncidentTitle, Duration, Severity      string
    StartTime, EndTime                         time.Time
    Timeline                                   []TimelineEvent
    CausalChain                                CausalChain
    ImpactedSvc                                []string
    RootCause, Resolution                      EditableField
    LessonsLearned                             []string
    ActionItems                                []ActionItem
    TopologySnapshot                           *diagnosis.TopologySnapshot
    DiagnosisMetrics                           []diagnosis.MetricEntry
    DiagnosisLogs                              []diagnosis.LogEntry
    ImpactAssessment                           *diagnosis.ImpactAssessment
    DetectionMethod, MTTD                      string
    WhatWentWell, WhatWentWrong, ContributingFactors []string
}

type EditableField struct {
    AIGenerated string  // LLM 原始输出
    Final       string  // 人工修正后
}
```

### 5.5 其他模型

```go
// models/business_app.go - BusinessApp (UID/AppName/Namespace/Criticality/Environment/Team/BusinessUnit)
// models/k8s_models.go - K8s 资源表示
// models/user_models.go - User (用户名/密码/Token/角色)
// models/role_models.go - Role (名称/权限列表/仪表盘)
// models/audit/ - 审计日志模型
// models/inspection/ - 巡检结果模型 (InspectionResult/InspectionReport)
```

---

## 六、接口设计

### 6.1 基础接口 (interfaces/dependencies.go)

```go
// Logger - 日志抽象 (实现: config.ZapLoggerAdapter)
type Logger interface {
    Debug(msg string, fields ...zap.Field)
    Info(msg string, fields ...zap.Field)
    Warn(msg string, fields ...zap.Field)
    Error(msg string, fields ...zap.Field)
}

// GraphDB - 图数据库抽象 (实现: config.NebulaGraphAdapter)
type GraphDB interface {
    Execute(query string) (*nebula.ResultSet, error)
    ExecuteAndCheck(query string) (*nebula.ResultSet, error)
}

// MessageQueue - 消息队列抽象 (实现: config.KafkaAdapter)
type MessageQueue interface {
    Publish(ctx context.Context, topic string, key string, message []byte) error
    PollFetches(ctx context.Context) FetchResult
    MarkCommit(msg *Message)
    PublishDeadLetter(ctx context.Context, originalMsg *Message, errMsg string) error
}

// Cache - 缓存抽象 (实现: config.BigCacheAdapter)
type Cache interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte) error
    Delete(key string) error
    Stats() CacheStats
}

// KubernetesClient - K8s 抽象 (实现: config.K8sClientAdapter)
type KubernetesClient interface {
    ListResources(gvr schema.GroupVersionResource, namespace string, opts interface{}) ([]unstructured.Unstructured, error)
    WatchResources(gvr schema.GroupVersionResource, namespace string, handler cache.ResourceEventHandler) error
    GetAPIResources() ([]schema.GroupVersionResource, error)
}

// Closer - 可关闭资源
type Closer interface { Close() error }

// BusinessContextProvider - 业务上下文提供者
type BusinessContextProvider interface {
    EnrichAlertByResourceUID(resourceUID string) BusinessAppContext
}
```

### 6.2 告警接口 (interfaces/alert/processor.go)

```go
type AlertProcessor interface {
    Process(ctx context.Context, payload *alert.WebhookPayload) ([]*alert.ProcessedAlert, error)
    GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert.ProcessedAlert, error)
    GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert.ProcessedAlert, error)
}

type AlertEnricher interface {
    Enrich(ctx context.Context, a *alert.Alert) (*alert.EnrichedAlert, error)
}

type AlertSuppressor interface {
    CheckSuppression(ctx context.Context, a *alert.EnrichedAlert) (*alert.SuppressionResult, error)
}

type AlertRouter interface {
    Route(ctx context.Context, a *alert.EnrichedAlert) (*alert.RoutingResult, error)
}

type AlertNotifier interface {
    Notify(ctx context.Context, a *alert.ProcessedAlert) error
}

type AlertStorage interface {
    Save(ctx context.Context, a *alert.ProcessedAlert) error
    GetActiveAlerts(ctx context.Context, filters map[string]string) ([]*alert.ProcessedAlert, error)
    GetAlertByFingerprint(ctx context.Context, fingerprint string) (*alert.ProcessedAlert, error)
    CleanupResolvedAlerts(olderThan time.Duration) int
    GetStats() map[string]int64
}
```

### 6.3 LLM Provider 接口 (interfaces/llm_provider.go)

```go
type LLMProvider interface {
    Diagnose(ctx context.Context, prompt DiagnosisPrompt) (*LLMDiagnosisResult, error)
    GeneratePostmortemInsights(ctx context.Context, prompt PostmortemInsightsPrompt) (*PostmortemInsightsResult, error)
    GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}
```

支持多 Provider: `none` (纯规则) / `openai` / `claude` / `custom` - 不可用时自动降级。

### 6.4 其他接口

```go
// interfaces/metrics_querier.go - MetricsQuerier (Query/QueryRange)
// interfaces/log_querier.go - LogQuerier (SearchLogs)
// interfaces/trace_querier.go - TraceQuerier (QueryTraces)
// interfaces/executor.go - Executor (Execute/GetStatus/ToggleAutoMode)
// interfaces/inspection.go - InspectionRule/InspectionProcessor
// interfaces/k8sresource_interface.go - K8sResourceInterface (Get/Collect/Relationship/.../Stop)
// interfaces/user_interface.go - UserInterface (CRUD)
// interfaces/role_interface.go - RoleInterface (CRUD + 权限管理)
```

---

## 七、完整 API 参考

### 7.1 K8s 资源 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/k8s/resources/{name}` | 获取指定资源详情 |
| GET | `/k8s/resources/graph/nodes` | 获取所有资源节点 |
| GET | `/k8s/resources/graph/edges` | 获取所有资源关系边 |
| GET | `/k8s/resources/graph/metadata` | 获取资源类型/命名空间元数据 |
| GET | `/k8s/resources/graph/search` | 搜索资源关系 |
| GET | `/k8s/resources/graph/resource-define` | 获取资源类型定义 |
| GET | `/k8s/resources/graph/suggest` | 资源搜索自动补全建议 |

### 7.2 告警管理 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/alerts/webhook` | 接收 Alertmanager Webhook（K8s 告警） |
| POST | `/api/v1/alerts/external/:source` | 接收外部系统告警（Ceph/Kafka/MySQL 等） |
| GET | `/api/v1/alerts` | 获取活跃告警列表 |
| GET | `/api/v1/alerts/{fingerprint}` | 获取告警详情 |
| GET | `/api/v1/alerts/health` | 告警服务健康检查 |
| GET | `/api/v1/alerts/suppression/status` | 获取当前抑制状态 |
| GET | `/api/v1/alerts/sources` | 列出所有注册的外部告警源 |

### 7.3 AI 诊断 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/v1/diagnosis/run` | 执行 AI 诊断 |
| POST | `/api/v1/diagnosis/rerun` | 重新执行诊断 |
| GET | `/api/v1/diagnosis/result/{id}` | 获取诊断结果 |
| GET | `/api/v1/diagnosis/status` | 诊断服务状态 |
| GET | `/api/v1/diagnosis/tools` | 列出所有 MCP 工具定义 |
| POST | `/api/v1/diagnosis/mcp/tool/{name}` | 执行指定 MCP 工具 |
| POST | `/api/v1/diagnosis/chat/ask` | AI 诊断问答（无状态，推荐） |
| POST | `/api/v1/diagnosis/chat/context` | 建立诊断上下文 |
| GET | `/api/v1/diagnosis/chat/session/by-alert` | 按告警指纹查询会话 |
| DELETE | `/api/v1/diagnosis/chat/{sessionId}` | 关闭会话 |

### 7.4 巡检服务 API

| 方法   | 路径                                                   | 功能       |
| ---- | ---------------------------------------------------- | -------- |
| POST | `/api/v1/inspection/execute`                         | 手动触发巡检   |
| GET  | `/api/v1/inspection/report`                          | 获取最新巡检报告 |
| GET  | `/api/v1/inspection/reports`                         | 历史报告列表   |
| GET  | `/api/v1/inspection/reports/{id}`                    | 指定报告详情   |
| GET  | `/api/v1/inspection/reports/{id}/compare/{other_id}` | 对比两份报告   |
| GET  | `/api/v1/inspection/trend`                           | 巡检趋势数据   |
| GET  | `/api/v1/inspection/rules`                           | 自定义规则列表  |
| POST | `/api/v1/inspection/rules`                           | 创建自定义规则  |
| PUT  | `/api/v1/inspection/rules/{id}`                      | 更新巡检规则   |
| DELETE | `/api/v1/inspection/rules/{id}`                    | 删除巡检规则   |
| POST | `/api/v1/inspection/rules/{id}/toggle`               | 启停巡检规则   |

### 7.5 自愈执行器 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/executor/status` | 执行器状态 |
| GET | `/api/v1/executor/audit` | 审计日志列表 |
| POST | `/api/v1/executor/execute` | 执行自愈操作 |
| POST | `/api/v1/executor/toggle` | 切换自动/手动模式 |
| POST | `/api/v1/executor/diagnose-and-execute` | 诊断并自动执行 |

### 7.6 日志查询 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/logs/pod` | 获取 Pod 日志 (ES) |
| GET | `/api/v1/logs/search` | 日志关键词搜索 |
| GET | `/api/v1/logs/errors` | 获取 Error 级别日志 |
| GET | `/api/v1/logs/warn` | 获取 Warn 级别日志 |
| GET | `/api/v1/logs/info` | 获取 Info 级别日志 |

### 7.7 指标与监控 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/metrics/pod` | Pod 指标 |
| GET | `/api/v1/metrics/node` | Node 指标 |
| GET | `/api/v1/metrics/deployment` | Deployment 指标 |
| GET | `/api/v1/metrics/statefulset` | StatefulSet 指标 |
| GET | `/api/v1/metrics/daemonset` | DaemonSet 指标 |
| GET | `/api/v1/metrics/service` | Service 指标 |
| GET | `/api/v1/monitoring/metrics/{resourceType}` | 资源类型指标 |
| GET | `/api/v1/monitoring/timeseries` | 指标时序数据 |
| GET | `/api/v1/monitoring/health` | 系统健康状态 |
| GET | `/api/v1/monitoring/catalog` | 可用指标目录 |

### 7.8 复盘分析 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/retrospective/timeline/{fingerprint}` | 事件时间线 |
| GET | `/api/v1/retrospective/causal-chain/{fingerprint}` | 因果链分析 |
| POST | `/api/v1/retrospective/postmortem/{fingerprint}` | 生成复盘报告 |
| PUT | `/api/v1/retrospective/postmortem/{fingerprint}` | 人工修正报告 |
| GET | `/api/v1/retrospective/postmortem/{fingerprint}/text` | 导出 Markdown |
| GET | `/api/v1/retrospective/list` | 历史报告分页列表 |
| GET | `/api/v1/retrospective/history/{fingerprint}` | 历史报告详情 |
| GET | `/api/v1/retrospective/knowledge/search` | 图增强混合检索 |
| GET | `/api/v1/retrospective/resource/{resourceUID}/postmortems` | 按资源查历史复盘 |

### 7.9 用户与角色 API

当前仅实现基础查询端点（完整 CRUD 规划中）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/users` | 获取用户信息（当前返回固定 ID） |
| GET | `/roles` | 获取角色信息（当前返回固定 ID） |

> 注：用户与角色路由当前注册在根路径下，不使用 `/api/v1` 前缀。

### 7.10 认证授权 API

| 方法 | 路径 | 功能 |
|------|------|------|
| POST | `/api/auth/login` | 用户登录（密码 + 验证码） |
| GET | `/api/auth/me` | 获取当前登录用户信息 |
| POST | `/api/auth/logout` | 用户登出 |
| POST | `/api/auth/tokens` | 创建 API Token（PAT） |
| GET | `/api/auth/tokens` | 列出当前用户的 API Token |
| DELETE | `/api/auth/tokens/:id` | 撤销指定 API Token |
| GET | `/api/auth/captcha` | 生成图片验证码 |
| POST | `/api/auth/captcha/verify` | 验证验证码 |

### 7.11 OAuth2 / OIDC API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET/POST | `/oauth2/authorize` | OAuth2 授权端点 |
| POST | `/oauth2/token` | OAuth2 Token 端点（Authorization Code + Refresh Token + Client Credentials） |
| POST | `/oauth2/introspect` | Token 内省端点 |
| POST | `/oauth2/revoke` | Token 撤销端点 |
| POST | `/oauth2/device/auth` | 设备授权请求 |
| GET/POST | `/device` | 设备授权确认页面 |
| GET | `/.well-known/openid-configuration` | OIDC Discovery 元数据 |

### 7.12 其他 API

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | `/api/v1/clusters` | 集群列表 |
| GET | `/api/v1/clusters/{name}/health` | 集群健康检查 |
| GET | `/api/v1/clusters/{name}/stats` | 集群统计信息 |
| GET | `/api/v1/stats/overview` | 资源概览统计 |
| GET | `/api/v1/stats/resource-total` | 资源总数统计 |
| POST | `/api/v1/stats/sync` | 手动同步资源 |
| GET | `/api/v1/terminal/namespaces` | 列出命名空间 |
| GET | `/api/v1/terminal/pods` | 列出 Pod |
| GET | `/api/v1/terminal/containers` | 列出容器 |
| GET | `/api/v1/terminal/ws` | WebSocket 终端连接 |
| GET | `/api/v1/trace/query` | 查询 Trace 链路 |
| GET | `/api/v1/trace/spans` | 获取 Span 详情 |
| GET | `/api/v1/trace/services` | 服务列表 |
| GET | `/api/v1/business-topology/apps` | 业务应用列表 |
| GET | `/api/v1/business-topology/calls` | 应用间调用关系 |
| GET | `/api/v1/business-topology/graph` | 业务拓扑图数据 |
| GET | `/api/v1/system/status` | 系统整体状态 |
| GET | `/metrics` | Prometheus 指标端点 |

---

## 八、配置指南

### 8.1 配置文件结构

配置采用 **目录模式**加载：程序启动时通过 `-c configs/` 加载整个目录，Viper 自动合并所有 `.yaml` 文件。每个配置文件提供 `.example` 模板，首次使用需复制：

```bash
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
cp agent.yaml.example agent.yaml
```

```
configs/
├── config.core.yaml.example       # 核心配置（数据库、缓存、日志、服务）
├── config.alert.yaml.example      # 告警系统配置（路由、抑制、通知、外部告警源）
├── config.diagnosis.yaml.example  # AI 诊断配置（LLM、Embedding、会话）
├── config.inspection.yaml.example # 巡检规则配置（Cron 调度）
├── config.infra.yaml.example      # 基础设施（ES、OTel、Executor）
├── config.business.yaml.example   # 业务拓扑配置（命名空间映射、RBAC）
├── config.profiles.yaml.example   # 资源指标 Profiles（自定义 PromQL）
├── config.auth.yaml.example   #   认证授权配置
├── config.retrospective.yaml.example # 复盘自动触发配置
└── prompts/                       # LLM Prompt 模板
```

所有配置段在 `config/config_base.go` 的 `Config` 结构体中统一定义，Viper 自动将 `configs/` 目录下所有 YAML 文件的键合并到单一 Config 实例：

### 8.2 核心配置段

| 配置段 | 说明 | 关键字段 |
|--------|------|----------|
| kubernetes | K8s 连接 | config (kubeconfig路径) |
| postgres | PostgreSQL | host, port, user, pass, database, pool:100 |
| nebula | NebulaGraph | host:9669, user, pass, space |
| nebulaCleanup | 清理策略 | enabled, retentionDays:7, cleanupInterval:24h, batchSize:1000 |
| kafka | 消息队列 | broker, topic, group, deadLetterTopic, maxRetries:5, retryBackoffMs:100, maxRecordBytes:50MB, traceTopic, businessWorkloadTopic |
| excludeLabels | 标签过滤 | 排除特定标签的资源 |
| cache | BigCache | lifeTime:40m, cleanWindow:10m, hardMaxCacheSize:4096MB, shards:256, resyncInterval:30m |
| log | 日志 | level, path, max_size:100MB, max_age:30d |
| alert | 告警 | routing, notifier, suppressor, cleanup, storage |
| inspection | 巡检 | enabled, cronSpec: "0 */6 * * *" |
| diagnosis | 诊断 | enabled, confidenceThreshold:0.8, cacheTTL:900, llm, session |
| opentelemetry | 链路追踪 | enabled, collectorURL, serviceName, timeout |
| prometheus | 指标查询 | enabled, url, timeout |
| resourceProfiles | 资源画像 | 自定义 PromQL，按资源类型定义指标/拓扑/证据 |
| elasticsearch | 日志 | addresses, indexPattern, timeout |
| roles | RBAC | enabled, defaultRole: viewer |
| executor | 执行器 | enabled, autoMode, auditLog, actions (risk+autoThreshold) |
| businessTopology | 业务拓扑 | enabled, knownServices, namespaceMapping |
| retrospective | 复盘 | retentionDays:180, autoTrigger |

### 8.3 环境变量

| 环境变量 | 覆盖配置 | 默认值 |
|----------|----------|--------|
| `MUTONG_PORT` | server.port | 8888 |
| `MUTONG_MAX_PROCS` | GOMAXPROCS | CPU核数 |
| `MUTONG_ALLOWED_ORIGINS` | CORS域名 | * |
| `MUTONG_DB_USER` | postgres.user | - |
| `MUTONG_DB_PASSWORD` | postgres.pass | - |
| `MUTONG_NEBULA_USER` | nebula.user | - |
| `MUTONG_NEBULA_PASS` | nebula.pass | - |
| `MUTONG_LLM_API_KEY` | diagnosis.llm.apiKey | - |

---

## 九、配置层详解

### 9.1 Config 结构体 (config_base.go)

```go
type Config struct {
    Kubernetes        KubernetesConfig
    Postgres          PostgresConfig
    Nebula            NebulaConfig
    NebulaCleanup     NebulaCleanupConfig
    Kafka             KafkaConfig
    ExcludeLabels     []string
    Cache             CacheConfig
    Log               LogConfig
    Alert             AlertConfig
    Inspection        InspectionConfig
    Diagnosis         DiagnosisConfig
    Opentelemetry     OTelConfig
    Prometheus        PrometheusConfig
    ResourceProfiles  map[string]ResourceProfileConfig
    Elasticsearch     ESConfig
    Roles             RBACConfig
    Executor          ExecutorConfig
    BusinessTopology  BusinessTopologyConfig
    Retrospective     RetrospectiveConfig
    Server            ServerConfig
}
```

### 9.2 初始化流程 (config.go)

1. 判断配置路径类型：
   - 若为**目录** → `loadConfigDir()` 扫描 `config.*.yaml` 文件，使用 `yaml.Unmarshal` 逐个解析（不使用 viper）
   - 若为**文件** → `loadConfigFile()` 使用 viper 加载单体配置文件
2. 环境变量覆盖：直接通过 `os.Getenv()` 读取覆盖对应配置字段（不使用 viper.BindEnv）
3. 反序列化到 `Config` 结构体
4. 验证必填字段，填充默认值

### 9.3 Adapter 适配器 (adapters.go)

| 适配器 | 实现接口 | 底层组件 |
|--------|----------|----------|
| `ZapLoggerAdapter` | `interfaces.Logger` | zap.Logger |
| `NebulaGraphAdapter` | `interfaces.GraphDB` | nebula-go/v3 |
| `KafkaAdapter` | `interfaces.MessageQueue` | franz-go |
| `BigCacheAdapter` | `interfaces.Cache` | bigcache/v3 |
| `K8sClientAdapter` | `interfaces.KubernetesClient` | client-go |
| `PostgresGormAdapter` | GORM DB | gorm.io/gorm |

---

## 十、数据流

### 10.1 告警处理流程

```
Prometheus Alertmanager → HTTP Webhook
    → AlertController.HandleWebhook()
    → AlertService.Process() [20并发worker]
        → Enricher.Enrich() [NebulaGraph查询 + BigCache拓扑缓存]
        → Suppressor.CheckSuppression() [拓扑感知 + 因果链]
        → Router.Route() [Owner + Stakeholder双维度]
        → Storage.Save() [内存/PostgreSQL]
        → Notifier.Notify() [Slack/PagerDuty/钉钉/邮件]
        → AutoDiagnosisPipeline.OnAlertProcessed() [异步触发诊断]
```

### 10.2 诊断流程

```
用户请求 → DiagnosisController.RunDiagnosis()
    → Engine.Diagnose()
        → ContextCollector [并行: Topology + Metrics + Logs]
        → HybridRetriever [pgvector语义 + NebulaGraph拓扑]
        → KnowledgeBase.Match() [规则匹配 + 置信度评分]
        → (置信度 < 阈值) → EinoAgent.RunStreamWithMessages() [adk.NewRunner + ReAct]
            → 全量 MCP 工具 [20 tools, SafeTool 容错, ModelRetryConfig 重试]
            → SSE 流式输出 [text/event-stream]
        → (LLM不可用) → 纯规则降级
        → 结果持久化 [PostgreSQL + pgvector + Redis会话]
```

### 10.3 资源采集流程

```
K8s API (Informer)
    → ResourceCollector → Kafka (topic: mutong30, 50MB消息)
        → Consumer.KafkaMessages()
            → NebulaGraph 写入 [节点+边]
            → ResourceCache 更新 [Informer本地缓存]
            → 关系构建 [Pod→Deployment, Service→Pod, ...]
            → BusinessLabelSyncer [标签→业务归属]

Beyla eBPF (HTTP/gRPC)
    → OTel Collector → Kafka (topic: mutong_trace_01)
        → TraceTopologySyncer.Consume()
            → 解析 OTLP Traces
            → 构建业务应用调用关系
            → NebulaGraph 写入 [BusinessApp 节点]
```

### 10.4 Trace 拓扑同步流程

```
SkyWalking OAP / OTel Collector
    → Kafka (mutong_trace_01)
        → TraceTopologySyncer
            → 解析 Trace Span
            → 提取 peer IP/域名 → knownServices 映射 → BusinessApp
            → 构建调用关系 (Upstream/Downstream)
            → NebulaGraph CALLS edge
```

---

## 十一、服务间依赖关系

```
                            ┌──────────┐
                            │  Main    │
                            └────┬─────┘
                                 │
               ┌─────────────────┼──────────────────┐
               ▼                 ▼                   ▼
        ┌──────────┐    ┌──────────────┐    ┌──────────────┐
        │ Gin HTTP │    │ Kafka        │    │ Informer     │
        │ Server   │    │ Consumer     │    │ Watcher      │
        └────┬─────┘    └──────┬───────┘    └──────┬───────┘
             │                 │                    │
    ┌────────┼─────────┐      │                    │
    ▼        ▼         ▼      ▼                    ▼
┌──────┐ ┌──────┐ ┌────────┐ ┌────────┐    ┌────────────┐
│Alert │ │Diag  │ │Inspect │ │Trace   │    │K8sResource │
│Service│ │Engine│ │Service │ │TopoSync│    │Service     │
└──┬───┘ └──┬───┘ └───┬────┘ └───┬────┘    └─────┬──────┘
   │        │         │          │               │
   └────────┼─────────┼──────────┼───────────────┘
            │         │          │
   ┌────────┼─────────┼──────────┼───────────────┐
   ▼        ▼         ▼          ▼               ▼
┌──────┐ ┌──────┐ ┌──────┐ ┌──────────┐ ┌─────────────┐
│Nebula│ │PostgreSQL│ │Kafka│ │BigCache │ │K8s client-go│
│ Graph│ │+pgvector│ │     │ │         │ │ + Redis     │
└──────┘ └──────────┘ └──────┘ └─────────┘ └─────────────┘
```

**依赖链**:
- `Main → Controllers → Services → Interfaces → Adapters → External Systems`
- `AlertService → Enricher/Suppressor/Router/Notifier → GraphDB/MQ/Storage`
- `DiagnosisEngine → ContextCollector → MetricsQuerier/LogQuerier/GraphDB/LLMProvider`
- `RetrospectiveService → DiagnosisCache/LLMProvider/HybridRetriever`
- `MCPServer → DiagnosisEngine/MetricsQuerier/K8sClient/LogQuerier/InspectionService`
- `TraceTopologySyncer → GraphDB (NebulaGraph)`
- `BusinessLabelSyncer → DB (PostgreSQL)`
- `K8sResourceService → Informer → Kafka → NebulaGraph/ResourceCache`

---

## 十二、部署架构

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                        │
│                                                              │
│  ┌─────────────┐    ┌──────────────┐    ┌──────────────┐   │
│  │  Beyla      │    │  OTel        │    │  Mutong      │   │
│  │  eBPF Agent │───►│  Collector   │───►│  AIOps       │   │
│  │  (DaemonSet)│    │  (Deployment)│    │  (Deployment)│   │
│  └─────────────┘    └──────┬───────┘    └──────┬───────┘   │
│                             │                    │           │
│                    ┌────────▼────────┐  ┌────────▼───────┐  │
│                    │    Kafka        │  │  PostgreSQL    │  │
│                    │  (Redpanda)     │  │  + pgvector    │  │
│                    └─────────────────┘  └────────────────┘  │
│                                                              │
│                    ┌──────────────────┐                     │
│                    │  NebulaGraph v3  │                     │
│                    │  (StatefulSet)   │                     │
│                    └──────────────────┘                     │
│                                                              │
│                    ┌──────────────────┐                     │
│                    │  Redis           │                     │
│                    │  (会话缓存)      │                     │
│                    └──────────────────┘                     │
│                                                              │
│                    ┌──────────────────┐                     │
│                    │  Elasticsearch   │                     │
│                    │  (日志存储)      │                     │
│                    └──────────────────┘                     │
│                                                              │
│                    ┌──────────────────┐                     │
│                    │  Prometheus      │                     │
│                    │  (指标存储)      │                     │
│                    └──────────────────┘                     │
└─────────────────────────────────────────────────────────────┘

数据流向:
  应用 Pod → Beyla eBPF → OTel Collector → Kafka(mutong_trace_01)
  K8s API → Mutong Collector → Kafka(mutong30) → Mutong Consumer → NebulaGraph
  Workload变更 → Kafka(business-workloads) → BusinessLabelSyncer
  Prometheus → Alertmanager → Mutong Webhook (/api/v1/alerts/webhook)
  Mutong → Prometheus (指标查询) + ES (日志查询) + K8s API (资源查询)
```

> **组件分级**：NebulaGraph 和 Kafka 为**必需组件**（缺失导致进程退出）。PostgreSQL、Redis、Elasticsearch、Prometheus 为**可选组件**（缺失时对应功能自动降级，不影响启动）。详细说明见 [README.md#前置条件](../README.md#前置条件)。

---

## 十三、资源关系与拓扑详情

### 13.1 资源采集架构

```
K8s API Server
    │
    ├── Dynamic Informer (Watch)
    │   └── ResourceEventHandler{Add/Update/Delete}
    │       ├── handleResourceEvent()
    │       │   ├── 创建 → seedToKafka() → gzip压缩 → Kafka(mutong30)
    │       │   ├── 更新 → seedToKafka()
    │       │   └── 删除 → MarkDeletedResource()
    │       └── UpdateRelationship() → 拓扑边构建
    │
    └── consumerLoop (50 并发 worker)
        └── ConsumeKafkaMessages()
            ├── DeadLetter 处理: 5次重试 → 指数退避 → DLQ
            ├── 反序列化: json-iterator FastJSON
            ├── insertK8sResourceToNebula() → INSERT VERTEX
            ├── InsertEdgeWithCleanup() → 版本化清理旧边
            └── UpdateDeletedResource() → 轮询已删除顶点
```

### 13.2 资源关系详细矩阵

| 源类型 | 关系类型 | 目标类型 | 构建方式 | 说明 |
|--------|----------|----------|----------|------|
| Pod | `RunsOn` | Node | spec.nodeName | Pod 调度到的节点 |
| Pod | `OwnedBy` | ReplicaSet | ownerReferences | 通过 RS 间接关联 Deployment |
| ReplicaSet | `OwnedBy` | Deployment | ownerReferences | RS owner 是 Deployment |
| Pod | `OwnedBy` | StatefulSet | ownerReferences | StatefulSet 管理 |
| Pod | `OwnedBy` | DaemonSet | ownerReferences | DaemonSet 管理 |
| Pod | `OwnedBy` | Job | ownerReferences | Job 管理 |
| Pod | `MountsPVC` | PVC | spec.volumes | 持久卷挂载 |
| PVC | `BoundTo` | PV | spec.volumeName | PV 绑定 |
| PV | `ProvisionedBy` | StorageClass | spec.storageClassName | 存储类 |
| Pod | `UsesSA` | ServiceAccount | spec.serviceAccountName | RBAC |
| Pod | `UsesConfigMap` | ConfigMap | spec.volumes + envFrom | 配置注入 |
| Pod | `UsesSecret` | Secret | spec.volumes + envFrom | 密钥注入 |
| Service | `SvcToPods` | Pod | EndpointSlice | 服务路由 |
| Service | `BelongsTo` | Namespace | metadata.namespace | 命名空间归属 |
| Ingress | `RoutesTo` | Service | spec.rules.backend | 入口路由 |
| Ingress | `UsesTLS` | Secret | spec.tls.secretName | TLS 证书 |
| HPA | `TargetsRef` | Deployment | spec.scaleTargetRef | 弹性伸缩 |
| PDB | `ProtectsApp` | Deployment | spec.selector | 中断预算 |
| Deployment | `BelongsTo` | Namespace | metadata.namespace | 命名空间归属 |
| ConfigMap | `BelongsTo` | Namespace | metadata.namespace | 命名空间归属 |
| BusinessApp | `CallsApp` | BusinessApp | Trace Span | 业务调用关系 |
| BusinessApp | `BELONGS_TO_APP` | K8sResource | Label Match | 业务→资源映射 |

### 13.3 业务拓扑服务

```go
// BusinessTopologyService - 业务拓扑
type BusinessTopologyService struct {
    graphDB    interfaces.GraphDB
    logger     interfaces.Logger
}

// 核心方法
func (s *BusinessTopologyService) GetApps()       // NebulaGraph查询所有BusinessApp
func (s *BusinessTopologyService) GetGraph()      // 查询BusinessApp + CALLS边
func (s *BusinessTopologyService) GetAppDetail()  // 查询单个应用详情 + 关联K8s资源
func (s *BusinessTopologyService) GetAppCalls()   // 查询应用上下游调用链
```

### 13.4 命名空间映射

通过 `namespaceMapping` 配置将 K8s 命名空间映射到业务属性：

```yaml
businessTopology:
  namespaceMapping:
    "kube-system":
      businessUnit: "基础设施"
      team: "platform-team"
      criticality: "critical"
      environment: "production"
    "prod-*":
      environment: "production"   # 支持 glob 模式匹配
    "staging-*":
      environment: "staging"
```

### 13.5 NebulaGraph Schema

```
Tag: K8sResource
  Properties: name, name_space, uid, kind, resource_version, creation_timestamp,
              labels (JSON), annotations (JSON), status (JSON),
              is_deleted (bool), deleted_at, cluster, node_name, owner_kind,
              owner_name, pod_ip, host_ip, api_version

Tag: BusinessApp
  Properties: app_name, namespace, team, business_unit, criticality,
              environment, source, updated_at

Edge: RunsOn, OwnedBy, SvcToPods, BelongsTo, MountsPVC, CallsApp,
      BELONGS_TO_APP, RoutesTo, BoundTo, UsesSA, UsesConfigMap, UsesSecret,
      UsesTLS, TargetsRef, ProtectsApp, ProvisionedBy
```

---

## 十四、PostgreSQL 数据库表结构

### 14.1 核心业务表

| 表名 | GORM Model | 说明 |
|------|------------|------|
| `alerts` | alert.ProcessedAlert | 告警存储 (JSONB: labels/annotations/topology_path/enrich_tags/business_ctx) |
| `alert_stats` | alert.StatsModel | 告警统计 (received/suppressed/notified) |
| `chat_sessions` | diagnosis.ChatSessionModel | 诊断会话 |
| `chat_messages` | diagnosis.ChatMessageModel | 会话消息历史 |
| `diagnosis_contexts` | diagnosis.DiagnosisContextModel | 诊断上下文 (拓扑/指标/日志 JSON) |
| `diagnosis_results` | diagnosis.DiagnosisResultModel | 诊断结果持久化 |
| `postmortem_reports` | retrospective.PostmortemReport | 复盘报告 (UPSERT by fingerprint) |
| `fault_report_vectors` | diagnosis.FaultReportVectorModel | pgvector 故障向量 (embedding 1536维) |
| `postmortem_resource_links` | retrospective.PostmortemResourceLink | 复盘→资源关联 |
| `executor_audit_logs` | executor.AuditLog | 执行器审计日志 |
| `decision_records` | retrospective.DecisionRecord | 告警管道决策记录 |
| `business_apps` | BusinessApp | 业务应用定义 |
| `inspection_reports` | inspection.InspectionReportModel | 巡检报告 |
| `inspection_results` | inspection.InspectionResultModel | 巡检结果明细 |
| `users` | User | 用户表 (用户名/密码/token/角色) |
| `roles` | Role | 角色表 (名称/权限JSON/仪表盘) |

### 14.2 GORM AutoMigrate 流程

```
SetupDB() → gorm.Open(postgres.Open(dsn))
  → AutoMigrate(
      Alert, AlertStats,
      ChatSession, ChatMessage, DiagnosisContext, DiagnosisResult,
      PostmortemReport, FaultReportVector, PostmortemResourceLink,
      AuditLog, DecisionRecord,
      BusinessApp,
      InspectionReport, InspectionResult,
      User, Role,
    )
  → 索引: idx_alerts_fingerprint, idx_alerts_status, idx_fault_vectors_embedding (IVFFlat)
```

---

## 十五、构建与测试

### 15.1 Just 构建命令

| 命令 | 说明 |
|------|------|
| `just build` | 编译 linux-amd64 + darwin-arm64 |
| `just build-mac` | 仅编译 macOS arm64 (本地开发) |
| `just build-skywalking` | 带 SkyWalking Agent 编译 |
| `just build-ui` | 前端生产构建 (输出到 artifacts/view) |
| `just dev-ui` | 前端开发模式 (热重载, port 3000) |
| `just run` | 运行项目 (artifacts 目录) |
| `just run-config <file>` | 指定配置文件运行 |
| `just run-debug` | 调试模式 (log-level=debug) |
| `just test` | 运行所有单元测试 (-race) |
| `just test-alert` | 运行告警系统 API 测试 |
| `just test-cover` | 运行测试并生成覆盖率报告 |
| `just test-module <module>` | 运行指定模块测试 |
| `just smoke-test` | 运行动态冒烟测试 |
| `just init-nebula` | 初始化 Nebula Graph 空间与 Schema |
| `just update-nebula` | 更新 nGQL 语句 |
| `just lint` | 静态代码检查 (go vet) |
| `just fmt` | 格式化 Go 代码 (gofmt) |
| `just all` | 一键: fmt + test + build |
| `just full-build` | 后端 + 前端完整构建 |
| `just clean` | 清理构建产物 |

### 15.2 测试覆盖

```
services/alert/      - service_test.go, enricher_test.go, suppressor_test.go,
                       router_test.go, storage_test.go, causal_suppression_test.go,
                       stakeholder_test.go, integration_test.go
services/diagnosis/  - engine_test.go, hybrid_retriever_test.go, llm_http_test.go,
                       impact_test.go, topology_test.go, confidence_test.go,
                       integration_test.go
services/executor/   - executor_test.go
services/retrospective/ - retrospective_service_test.go
services/            - k8sresource_collector_test.go, k8sresource_kafka_test.go,
                       k8sresource_nebula_test.go, k8sresource_ready_gate_test.go,
                       resource_cache_test.go,
                       ngql_sanitizer_test.go, trace_topology_syncer_test.go,
                       business_label_syncer_test.go
controllers/         - webhook_verifier_test.go
```

---

## 十六、编码规范与约定

### 16.1 Go 代码规范

- **格式化**: `gofmt -w .` (强制)
- **命名**: 驼峰命名法 (CamelCase)，导出函数首字母大写
- **注释**: 所有导出函数、类型、常量必须添加文档注释
- **错误处理**: 显式处理所有 error，使用 `zap` 记录结构化日志
- **静态检查**: `go vet` (just lint) 通过后方可提交
- **并发控制**: 使用信号量 (buffered channel)，默认 20-50 并发上限

### 16.2 前端规范

- **组件**: Vue 3 Composition API，单文件组件
- **样式**: 纯 CSS Custom Properties，无预处理器
- **API 调用**: 统一 `fetch()` 封装 (utils/api.js)，每接口自动 `.catch(() => null)`
- **响应式**: CSS 媒体查询断点: 900px / 700px / 480px

### 16.3 中间件链

```
Gin Engine
  ├── TraceIDMiddleware        // 提取或生成 X-Trace-ID
  ├── CORSMiddleware           // CORS 跨域 (gin-contrib/cors)
  ├── RateLimiter              // 令牌桶限流 (100 req/s)
  ├── BearerTokenMiddleware    // Bearer token (PAT/OAuth2) + session cookie 回退
  ├── RequireAuthMiddleware    // /api/ 拦截 + 白名单(login/captcha/webhook/external)
  └── Recovery                 // panic 恢复 (gin.Recovery)
```

---

## 十七、关键业务流程详解

### 17.1 告警→诊断→执行闭环

```
                    ┌─────────────────────┐
                    │  Alertmanager       │
                    │  (Prometheus触发)    │
                    └──────────┬──────────┘
                               │ POST /api/v1/alerts/webhook
                               ▼
                    ┌─────────────────────┐
                    │  AlertService       │
                    │  Process()          │
                    │  Enrich→Sup→Route   │
                    └──────────┬──────────┘
                               │ OnAlertProcessed()
                               ▼
                    ┌─────────────────────┐
                    │  AutoDiagnosis      │
                    │  Pipeline           │
                    │  (队列缓冲 100)      │
                    └──────────┬──────────┘
                               │ Engine.Diagnose()
                               ▼
                    ┌─────────────────────┐
                    │  DiagnosisEngine    │
                    │  Collect→Retrieve   │
                    │  →Rule→LLM          │
                    └──────────┬──────────┘
                               │ DiagnosisResult
                               ▼
                    ┌─────────────────────┐
                    │  RemediationBridge  │
                    │  CreatePlanFrom     │
                    │  Diagnosis()        │
                    └──────────┬──────────┘
                               │ AutoFixable=true
                               ▼
                    ┌─────────────────────┐
                    │  K8sExecutor        │
                    │  Execute(plan)      │
                    │  restart/scale/hpa  │
                    └─────────────────────┘
```

### 17.2 聊天诊断会话流程

```
用户 → POST /api/v1/diagnosis/chat/ask (推荐，无状态)
  → DiagnosisController.askChat()
    → EinoLLMProvider → askChatWithEino()
      → 全量工具注册 (GetEinoToolDefsAndHandlers, 不再 SelectTools 过滤)
      → NewDiagnosisAgent(chatModel, einoTools) → RunStreamWithMessages()
        → adk.NewRunner + runner.Run() (非直接 agent.Run())
        → ModelRetryConfig (MaxRetries=3) + safeToolHandler
        → callback → writeLine(fmt.Sprintf) → flusher.Flush()
        → SSE 事件: stream_chunk / tool_call / action(exit) / error
      → Content-Type: text/event-stream + X-Accel-Buffering: no
    → 前端 fetch + ReadableStream reader 消费 SSE
      → 30ms 增量渲染 buffer → v-html Markdown
      → 工具调用浮动面板 (pulse 动画 + 3s 自动消失)
      → 分层超时: 总 300s / 首 token 15s / 空闲 60s

遗留路径:
  POST /api/v1/diagnosis/chat/start → 创建会话
  GET  /api/v1/diagnosis/chat/{id}/stream → SSE 流式 (旧版)
  POST /api/v1/diagnosis/chat/{id}/message → 追加消息
```

### 17.3 复盘生成完整链路

```
用户请求 → POST /api/v1/retrospective/postmortem/{fingerprint}
  → RetrospectiveController.GeneratePostmortem()
    → Service.GeneratePostmortem():
        1. BuildTimeline(fingerprint)
           └── AlertStorage + AlertProcessor 获取告警历史
        2. AnalyzeCausalChain(fingerprint)
           └── NebulaGraph: MATCH 拓扑路径 → 因果推断
        3. enrichFromDiagnosisCache(fingerprint)
           └── Redis: diagnose:result:{fingerprint}
           └── 提取: RootCauses/TopologySnapshot/Metrics/Logs
        4. enrichBusinessCallsFromNebula(fingerprint)
           └── NebulaGraph: BusinessApp→CallsApp 查询
        5. enrichWorkloadContext(fingerprint)
           └── NebulaGraph: Pod→OwnedBy→Deployment 同级健康状态
        6. enrichWithLLM(report)
           └── LLMProvider.GeneratePostmortemInsights()
           └── System Prompt + Timeline + Metrics + Topology
           └── 输出: WhatWentWell/Wrong/ContributingFactors + ActionItems
        7. savePostmortem(report)
           └── PG: postmortem_reports UPSERT
           └── pgvector: FaultReportVector INSERT
           └── PostmortemResourceLink INSERT
```

### 17.4 Kafka 消息处理流程

```
Producer (Collector):
  K8s Informer Event → json-iterator Marshal → gzip压缩
    → config.KafkaAdapter.Publish(topic: "mutong30", key: uid, value: compressedBytes)
    → maxRecordBytes: 52,428,800 (50MB)
    → maxBrokerWriteBytes: 104,857,600 (100MB)

Consumer (50 workers):
  PollFetches(ctx) → FetchResult
    → EachRecord(fn):
      → 解压 (gzip/gunzip)
      → 反序列化 (json-iterator Unmarshal)
      → processKafkaMessage():
          ├── insertK8sResourceToNebula() → INSERT VERTEX
          ├── buildRelationships() → INSERT EDGE
          └── onError:
              ├── 重试 (maxRetries: 5, 指数退避: 100ms × 2^n)
              └── 超过重试 → PublishDeadLetter() → DLQ Topic
    → MarkCommit(msg) → 确认消费

清理:
  nebulaCleanup:
    StartPeriodicCleanup() → 每24h执行
      → LOOKUP ON K8sResource WHERE is_deleted == true
      → DELETE VERTEX (batchSize: 1000)
      → 保留天数: 7 (retentionDays)
```

---

## 十八、扩展与自定义

### 18.1 添加新资源类型

1. 在 `k8sresource_collector.go` 的 `getAPIResources` 中添加新的 GVR (GroupVersionResource)
2. 在 `k8sresource_relationships.go` 中实现关系识别逻辑
3. 更新 `query_builder.go` 中的 nGQL 查询模板
4. 在 `k8sresource_controller.go` 中添加 API 端点
5. 前端 `topology.js` 中更新图例和渲染配置

### 18.2 添加新巡检规则

在 `configs/config.inspection.yaml` 中添加规则定义即可，无需修改 Go 代码:

```yaml
rules:
  - name: "check-new-rule"
    type: "min_rows"
    query: |
      MATCH (n:K8sResource{kind:'Pod',is_deleted:false})
      WHERE ... RETURN n
    check:
      threshold: 1
    severity: "warning"
    suggestion: "建议措施"
```

若内置类型不够，编写 command 插件 (stdin JSON → stdout JSON)，放到 `/etc/mutong/plugins/`。

### 18.3 自定义指标 (ResourceProfiles)

在 `configs/config.profiles.yaml` 中添加:

```yaml
resourceProfiles:
  MyCustomType:
    metrics:
      - name: custom_metric
        promql: 'my_metric{resource="{{name}}",ns="{{namespace}}"}'
        unit: "ops/sec"
    topology:
      - relation: "MyRelation"
        direction: "outbound"
    evidence:
      - source: "elasticsearch"
        index: "my-index-*"
        query: 'field:"{{name}}"'
```

### 18.4 添加 MCP 工具

在 `services/mcp/server.go` 中:

1. 实现 `ToolHandler` 函数 (签名: `func(ctx, args) (string, error)`)
2. 在 `registerTools()` 中注册: `s.tools["tool_name"] = s.handleMyTool`
3. 在 `ListTools()` 中添加 `ToolDefinition` (含 Name/Description/Parameters)
4. 添加 nGQL 注入防护: `sanitizer.EscapeString(param)`

---

## 十九、已知限制与设计约束

### 19.1 架构限制

| 限制 | 说明 | 缓解措施 |
|------|------|----------|
| 单集群 Kubeconfig | 当前仅支持单 kubeconfig 文件 | 多集群通过 Beyla+OTel Collector 间接支持 |
| 内存存储 | 告警默认使用 sync.Map 内存存储 | 可切换 PostgreSQL (storage.type: postgres) |
| LLM 依赖 | AI 诊断深度分析需 LLM (MiniMax/OpenAI/Claude) | 纯规则降级路径：未配置 LLM 时自动使用规则引擎 + 拓扑分析，仍可给出置信度评分 |
| 无水平扩展 | 单实例部署，无分布式协调 | 通过 Kafka 消费组支持多实例 |
| Informer 内存 | Agent 工具查询 Informer 缓存，可能滞后 | list_k8s_resources 实时查询作为补充 |

### 19.2 安全措施

| 措施 | 实现位置 | 说明 |
|------|----------|------|
| nGQL 注入防护 | `ngql_sanitizer.go` | 转义所有用户输入的 nGQL 参数 |
| 资源名校验 | `mcp/server.go` | 正则: `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`, ≤253 |
| 命名空间校验 | `mcp/server.go` | 同正则, ≤63 |
| Token 认证 | `auth_middleware.go` | Bearer Token 验证 |
| 限流 | `rate_limiter.go` | 令牌桶 100 req/s |
| 审计日志 | `executor/pg_store.go` | 所有执行操作完整审计 |

---

> **文档版本**: v3.0 | **项目**: 重明 (Mutong) AIOps 平台 | **许可证**: MIT
