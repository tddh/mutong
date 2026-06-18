# 重明 (Mutong) 待办事项

> 最后更新：2026-06-04（含代码审计报告 + 竞品对标分析）

---

## 未形成功能闭环（有后端基础但链条不完整）

> 以下功能已具备部分后端能力，但缺少前端、API 参数或配置页面，用户无法端到端使用。

### 1. 多集群管理
- **后端**：`config.core.yaml` 支持 `clusters[]` 数组声明多集群
- **现状**：所有 K8s 操作（资源查询 / Terminal / Executor）只取 `for range` 的最后一个集群
- **缺口**：
  - K8s 资源 API 无 `cluster` 查询参数
  - `RequestContext` 无 cluster 字段
  - 前端无集群选择器，仅集群管理页展示列表（无切换能力）
  - 拓扑图、告警、诊断页面均未支持按集群过滤
- *估时：16h*

### 2. RBAC 用户 / 角色管理
- **后端**：三种角色（admin / operator / viewer）已注册，Casbin 中间件已生效
- **现状**：用户 API 仅 `GET /users`（返回当前用户），角色 API 仅 `GET /roles`
- **缺口**：
  - 缺少 POST/PUT/DELETE 端点
  - `UserService` 仅有 `CreateUserFromZitadel` 自动创建
  - 前端无用户管理页面
- *涉及：`controllers/user_controller.go`、`controllers/role_controller.go`、`services/user_service.go`*

### 3. 用户设置与 Token 管理
- **后端**：`/api/auth/tokens` 支持列出 / 创建 / 撤销 Token
- **现状**：无前端入口
- **缺口**：用户无 Web UI 可管理个人 Token（只能 CLI `mutongctl auth login` 获取）

### 4. 巡检自定义规则
- **后端**：`/api/v1/inspection/rules` 已实现 POST/PUT/DELETE/toggle 完整 CRUD；`configs/rules/inspection/*.yml` YAML 文件加载（YAML > Go 内置 > DB 优先级）
- **现状**：前端巡检页面以表格展示 5 类内置规则结果（证书/cert_expiry 仍由 Go 规则处理）
- **缺口**：无规则创建 / 编辑 / 删除 / 启停的 UI

### 5. 外部告警源管理
- **后端**：支持 4 种富化策略（node_affinity / service_graph / direct_business_app / business_labels），配置在 `config.alert.yaml`
- **现状**：API 仅 `GET /api/v1/alerts/sources` 列出已注册源
- **缺口**：
  - 无注册 / 修改 / 删除告警源的 API
  - 无前端告警源配置页面
  - 新增告警源需修改 YAML 配置文件并重启

### 6. 第三方认证（Zitadel OIDC） — 已修复
- **修复内容**：
  1. OIDCController 已在 main.go 注册 ✅
  2. 白名单已补充 `/api/auth/oidc/login` 和 `/api/auth/oidc/callback` ✅
  3. 前端 SSO 按钮改为条件渲染（authMode === 'hybrid'） ✅
  4. `/api/auth/login-status` 端点已实现 ✅
  5. `LoginStatus` 以 YAML 配置 `cfg.Auth.Mode` 为权威来源 ✅
  6. 无论 OIDC 初始化成功与否，都会注册路由（失败时返回 503 而非 404） ✅
- **仍需改进**：
  - 用户/角色 CRUD（见 P2 待办项）
  - Zitadel/OIDC 文档需更完善（已在 user-manual.md 补充）
  - Zitadel 实际配置项（issuer, client_id 等）需在生产部署时填完

### 7. 链路追踪 — TraceController 未注册（完全不可用）
- **后端**：`TraceController` 存在，`OTelQueryService` (TraceQuerier) 已实现
- **现状**：`main.go` 中从未实例化或注册 `TraceController`，`/api/v1/trace/*` 全 404
- **缺口**：前端 `/trace/index.html` 页面存在，但后端路由未注册
- **需修复**：根据 `config.infra.yaml` 的 `opentelemetry.collectorURL` 初始化 OTelQueryService，注册 TraceController
- *涉及：`controllers/trace_controller.go`、`services/trace/otel_query.go`、`cmd/main.go`*

### 8. K8s 资源查询未区分集群
- **后端**：`K8sResoureService` 持有 cluster 配置
- **现状**：`Get()` 方法不区分集群，返回所有 Informer 缓存数据
- **缺口**：API 不支持按 cluster 参数过滤资源

---

## 待完成

### P1（高优先级）

- [ ] **初始版本标签**：`git tag v0.1.0` + Release 说明
  *估时：10min*

### P2（中等优先级）

- [ ] **前端页面交互优化**：15 个页面均为基础功能实现，缺乏交互体验优化。包括：加载状态/骨架屏、空状态占位、错误边界处理、表格分页/排序/筛选增强、表单验证反馈、动画过渡效果、响应式布局适配（平板/手机）、深色模式支持、键盘快捷键、无障碍访问（alt/aria 标签）
  *涉及：`view/src/` 下全部页面及共享组件*
  *估时：8h*

- [ ] **用户/角色 CRUD**：当前仅 GET /users 和 GET /roles，需补充 POST/PUT/DELETE + DB 表操作
  *涉及文件：`controllers/user_controller.go`、`controllers/role_controller.go`、`services/user_service.go`、`services/role_service.go`、`models/`*

- [ ] **ServiceAccount 管理 API**：当前 `models/service_account.go` 数据模型已定义且通过 GORM AutoMigrate，但缺少 CRUD 端点（`/api/auth/service-accounts`）。OAuth2 认证（fosite Provider、Zitadel OIDC、PAT/SAT Token）已完整实现
  *涉及文件：`controllers/auth_controller.go`（新增端点）、`services/auth/token.go`（已有 SAT 创建能力，需对接 API）*

- [ ] ~~**模块路径迁移**~~（暂缓）：`gitee.com/tddh/mutong` → 目标平台路径
  *涉及：`go.mod` 第 1 行 + 120+ 个 .go 文件的 import 语句 + README/CONTRIBUTING 中的 clone URL*

### P3（低优先级）

- [ ] **诊断模块单元测试 Phase 2-4**：engine.go / context_collector.go / eino_agent.go 补测
  *涉及文件：`services/diagnosis/*_test.go`*

- [ ] **API 文档全量注解**：当前仅完成项目级配置和 Swagger UI，handler 注解待全面推进

- [ ] **前端测试框架搭建**：vitest + @vue/test-utils + jsdom，为核心页面写冒烟测试
  *涉及：`view/package.json`、新建 `vitest.config.ts`、`view/src/**/*.test.ts`*
  *估时：3h*

- [ ] **替换废弃依赖 gogo/protobuf**：→ `google.golang.org/protobuf`
  *涉及：`go.mod` + 依赖该库的源文件*
  *估时：1h*

---

## 代码审计发现（2026-06-04）

### 废弃代码待清理

- [ ] **清除 6 个 `//nolint:unused` 废弃函数/类型** — `controllers/diagnosis_controller.go` 中 `startChat`、`streamChat`、`sendChatMessage`、`streamLLMResponse` 及 2 个响应结构体
  *估时：5min*

- [ ] **清除 2 个 `// 未调用` 遗留方法** — `controllers/controller.go` 中 `K8sResourceCollect()` 和 `K8sResourceRelationship()`
  *估时：5min*

- [ ] **清除 1 个空函数** — `RegisterControllersRoutes()` 方法体已被注释
  *估时：1min*

### 零测试的关键模块

- [ ] **巡检服务测试** — `services/inspection/` 下 12 个文件（6 服务 + 6 规则）完全无测试
  *风险：🔴 高。巡检引擎为核心业务功能*
  *估时：2h*

- [ ] **MCP Server 测试** — `services/mcp/server.go`（20 个工具处理函数）无测试
  *风险：🟡 中*
  *估时：1h*

- [ ] **核心服务层测试** — `k8sresource_service.go`（~500 行）、`business_topology_service.go`、`query_builder.go`（~300 行）、`role_service.go`、`user_service.go`
  *估时：2h*

- [ ] **其他无测试模块** — `services/audit/`、`services/terminal/`、`services/trace/`、`services/logsearch/`、`services/prompt/`
  *估时：1.5h*

### P0 — 商业竞标必备缺失（所有竞品标配但 Mutong 没有）

- [ ] **成本管理模块** — 集成 OpenCost（Apache 2.0 / CNCF Incubating），实现按 Namespace/Label/Deployment 粒度的实时成本分配 + Right-Sizing 推荐 + 闲置资源检测
  *涉及：新增 `services/cost/` 模块 + OpenCost API 网关 + 前端成本仪表板页*
  *涉及配置：`config.cost.yaml.example`*
  *参考：Kubecost Helm Chart → Prometheus → 自定义 UI*
  *估时：3h*

- [ ] **安全合规扫描** — 集成 Kubescape（CNCF），实现 K8s 安全态势扫描（CIS/NSA Benchmark）、容器镜像漏洞扫描（CVE）、RBAC 风险分析。利用 NebulaGraph 做 RBAC 关系图谱
  *涉及：新增 `services/security/` 模块 + Kubescape Operator 集成 + 前端安全扫描页*
  *参考：Kubescape Operator + MCP Server*
  *估时：3h*

- [ ] **SLO/SLI 管理** — 新增 SLO 定义（CRD/YAML）、错误预算计算引擎、燃烧率告警（burn rate）、部署前 SLO 评估门禁
  *涉及：新增 `services/slo/` 模块 + `config.slo.yaml.example` + 前端 SLO 管理页*
  *参考：Grafana SLO API + Keptn EvaluationDefinition*
  *估时：3h*

### P1 — 显著差异化能力

- [ ] **ML 无监督异常检测** — 对每个关键指标训练轻量异常模型（参考 Netdata 18 模型共识机制），实现零配置即开即用的异常识别。作为 LLM 诊断引擎的额外信号源
  *涉及：新增 `services/anomaly/` 模块 + 时序特征提取 + 多模型投票引擎*
  *参考：Netdata anomaly detection / Datadog Watchdog*
  *估时：4h*

- [ ] **变更关联根因** — 接入 Git/CI 变更事件（Webhook），自动关联部署/配置变更与异常指标，识别"什么变更导致了故障"
  *涉及：新增 `/api/v1/change-tracking` 端点 + Git Webhook 接收 + 指标变更相关性分析*
  *参考：Datadog Faulty Deployment Detection / PagerDuty Change Correlation*
  *估时：3h*

- [ ] **自动化 Runbook 引擎** — 低代码工作流：告警触发 → 自动执行诊断步骤 → 附带上下文 → 必要时升级人工审批
  *涉及：新增 `services/runbook/` 模块 + Runbook 定义模板 + 执行引擎*
  *参考：PagerDuty Runbook Automation / Dynatrace Workflow*
  *估时：4h*

- [ ] **多集群统一管理** — 跨集群拓扑聚合、多集群健康仪表板、全局成本对比视图、配置策略同步
  *依赖：先完成"多集群管理"基础能力（见未完成功能 #1）*
  *涉及：聚合层 `services/federator/` + 跨集群 Nebula 查询 + 全局仪表板*
  *参考：Grafana Cloud / Datadog Kubernetes Federation*
  *估时：4h*

- [ ] **自动复盘调度器** — `auto_retrospective.go` 模型已定义但无消费逻辑。新增后台调度器：根据规则（如 P0 告警/MTTD 超阈值）自动触发复盘，生成并归档报告
  *涉及：`services/retrospective/auto_scheduler.go` + Cron 调度 + Webhook 触发*
  *估时：2h*

### P2 — 长期差异化

- [ ] **预测性告警** — 基于时序预测（Prophet/ARIMA）提前 N 小时预警资源瓶颈（CPU/内存/磁盘）
  *参考：Datadog Watchdog Forecast / Dynatrace*
  *估时：3h*

- [ ] **AI Agent 自治运维** — 参考 Dynatrace Intelligence / BigPanda L1 Agent，构建可自主执行检测 → 诊断 → 建议修复 → 审批执行的 Agent 回路
  *涉及：增强现有 MCP 工具 + Agent 回路编排（Eino Agent 框架）*
  *估时：4h*

- [ ] **On-Call 集成** — 不自建排班系统，通过 API 集成 PagerDuty 或 Opsgenie 作为值班管理后端
  *涉及：`services/oncall/` 适配器层（PagerDuty API + Opsgenie API）*
  *参考：PagerDuty Events API v2*
  *估时：2h*

- [ ] **智能工作负载 (Business-centric)** — 参考 New Relic Intelligent Workloads，将 K8s 技术指标映射到业务 KPI（订单量、用户转化率），AI 优先展示影响业务的关键信号
  *涉及：增强 `services/prometheus/` 业务指标定义 + BusinessApp 关联*
  *估时：2h*

- [ ] **GitOps 漂移修复** — 集成 ArgoCD/Flux，自动检测 YAML 漂移并触发自愈回滚
  *涉及：`services/gitops/` 适配器 + 定期对比声明态 vs 实际态*
  *参考：ArgoCD selfHeal / Flux reconcile*
  *估时：3h*

- [ ] **碳成本追踪** — 按云区域/资源类型追踪碳排放成本（响应 ESG/碳中和合规需求）
  *参考：OpenCost 碳成本功能*
  *估时：1h*

- [ ] **MCP Server 测试** — `services/mcp/server.go`（20 个工具处理函数）无测试
  *风险：🟡 中*

- [ ] **核心服务层测试** — `k8sresource_service.go`（~500 行）、`business_topology_service.go`、`query_builder.go`（~300 行）、`role_service.go`、`user_service.go`
  *估时：8h*

- [ ] **其他无测试模块** — `services/audit/`、`services/terminal/`、`services/trace/`、`services/logsearch/`、`services/prompt/`
  *估时：6h*

---

## 业界竞品差距 — 待补齐的能力（对标 14 个 AIOps 竞品）

> 对标平台：OpenCost/Kubecost/Kubescape/Coroot/K8sGPT/Datadog/Dynatrace/Grafana/BigPanda/PagerDuty/NewRelic/Splunk/Netdata/Keptn

### P0 — 商业竞标必备缺失（所有竞品标配但 Mutong 没有）

- [ ] **成本管理模块** — 集成 OpenCost（Apache 2.0 / CNCF Incubating），实现按 Namespace/Label/Deployment 粒度的实时成本分配 + Right-Sizing 推荐 + 闲置资源检测
  *涉及：新增 `services/cost/` 模块 + OpenCost API 网关 + 前端成本仪表板页*
  *涉及配置：`config.cost.yaml.example`*
  *参考：Kubecost Helm Chart → Prometheus → 自定义 UI*
  *估时：12h*

- [ ] **安全合规扫描** — 集成 Kubescape（CNCF），实现 K8s 安全态势扫描（CIS/NSA Benchmark）、容器镜像漏洞扫描（CVE）、RBAC 风险分析。利用 NebulaGraph 做 RBAC 关系图谱
  *涉及：新增 `services/security/` 模块 + Kubescape Operator 集成 + 前端安全扫描页*
  *参考：Kubescape Operator + MCP Server*
  *估时：12h*

- [ ] **SLO/SLI 管理** — 新增 SLO 定义（CRD/YAML）、错误预算计算引擎、燃烧率告警（burn rate）、部署前 SLO 评估门禁
  *涉及：新增 `services/slo/` 模块 + `config.slo.yaml.example` + 前端 SLO 管理页*
  *参考：Grafana SLO API + Keptn EvaluationDefinition*
  *估时：12h*

### P1 — 显著差异化能力

- [ ] **ML 无监督异常检测** — 对每个关键指标训练轻量异常模型（参考 Netdata 18 模型共识机制），实现零配置即开即用的异常识别。作为 LLM 诊断引擎的额外信号源
  *涉及：新增 `services/anomaly/` 模块 + 时序特征提取 + 多模型投票引擎*
  *参考：Netdata anomaly detection / Datadog Watchdog*
  *估时：16h*

- [ ] **变更关联根因** — 接入 Git/CI 变更事件（Webhook），自动关联部署/配置变更与异常指标，识别"什么变更导致了故障"
  *涉及：新增 `/api/v1/change-tracking` 端点 + Git Webhook 接收 + 指标变更相关性分析*
  *参考：Datadog Faulty Deployment Detection / PagerDuty Change Correlation*
  *估时：10h*

- [ ] **自动化 Runbook 引擎** — 低代码工作流：告警触发 → 自动执行诊断步骤 → 附带上下文 → 必要时升级人工审批
  *涉及：新增 `services/runbook/` 模块 + Runbook 定义模板 + 执行引擎*
  *参考：PagerDuty Runbook Automation / Dynatrace Workflow*
  *估时：16h*

- [ ] **多集群统一管理** — 跨集群拓扑聚合、多集群健康仪表板、全局成本对比视图、配置策略同步
  *依赖：先完成"多集群管理"基础能力（见未完成功能 #1）*
  *涉及：聚合层 `services/federator/` + 跨集群 Nebula 查询 + 全局仪表板*
  *参考：Grafana Cloud / Datadog Kubernetes Federation*
  *估时：20h*

- [ ] **自动复盘调度器** — `auto_retrospective.go` 模型已定义但无消费逻辑。新增后台调度器：根据规则（如 P0 告警/MTTD 超阈值）自动触发复盘，生成并归档报告
  *涉及：`services/retrospective/auto_scheduler.go` + Cron 调度 + Webhook 触发*
  *估时：8h*

### P2 — 长期差异化

- [ ] **预测性告警** — 基于时序预测（Prophet/ARIMA）提前 N 小时预警资源瓶颈（CPU/内存/磁盘）
  *参考：Datadog Watchdog Forecast / Dynatrace*
  *估时：12h*

- [ ] **AI Agent 自治运维** — 参考 Dynatrace Intelligence / BigPanda L1 Agent，构建可自主执行检测 → 诊断 → 建议修复 → 审批执行的 Agent 回路
  *涉及：增强现有 MCP 工具 + Agent 回路编排（Eino Agent 框架）*
  *估时：20h*

- [ ] **On-Call 集成** — 不自建排班系统，通过 API 集成 PagerDuty 或 Opsgenie 作为值班管理后端
  *涉及：`services/oncall/` 适配器层（PagerDuty API + Opsgenie API）*
  *参考：PagerDuty Events API v2*
  *估时：6h*

- [ ] **智能工作负载 (Business-centric)** — 参考 New Relic Intelligent Workloads，将 K8s 技术指标映射到业务 KPI（订单量、用户转化率），AI 优先展示影响业务的关键信号
  *涉及：增强 `services/prometheus/` 业务指标定义 + BusinessApp 关联*
  *估时：10h*

- [ ] **GitOps 漂移修复** — 集成 ArgoCD/Flux，自动检测 YAML 漂移并触发自愈回滚
  *涉及：`services/gitops/` 适配器 + 定期对比声明态 vs 实际态*
  *参考：ArgoCD selfHeal / Flux reconcile*
  *估时：12h*

- [ ] **碳成本追踪** — 按云区域/资源类型追踪碳排放成本（响应 ESG/碳中和合规需求）
  *参考：OpenCost 碳成本功能*
  *估时：6h*
