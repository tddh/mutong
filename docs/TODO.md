# 重明 (Mutong) 待办事项

> 最后更新：2026-09-04（前端菜单收敛 + Grafana 内嵌调研结论 + tf001 后端可用性实测；含此前的代码审计报告与竞品对标分析）

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

### 7. 链路追踪 — 后端未接通（已修正描述，2026-09-04）
- **代码现状**：`cmd/main.go:635-642` 已按配置条件注册 `TraceController`（`/api/v1/trace/query|spans|services`），此前"从未注册"的说法已过时
- **实际缺口**：注册前提是 `opentelemetry.enabled && collectorURL != ""`，而 `configs/config.infra.yaml` 默认 `enabled: false`
- **tf001 实测**：启动日志 `OpenTelemetry not configured, tracing API disabled`；`http://localhost:4318` 无监听 → 追踪页确无后端可用
- **待办**：接入真实 OTel Collector / Tempo，确认 `OTelQueryService` 查询协议与后端匹配（现有实现走 collectorURL，需验证是否支持 trace 读取），再恢复菜单入口
- *涉及：`controllers/trace_controller.go`、`services/trace/otel_query.go`、`configs/config.infra.yaml`*
- **临时处置**：菜单入口已隐藏（见"前端菜单收敛"章节）

### 8. K8s 资源查询未区分集群
- **后端**：`K8sResoureService` 持有 cluster 配置
- **现状**：`Get()` 方法不区分集群，返回所有 Informer 缓存数据
- **缺口**：API 不支持按 cluster 参数过滤资源

---

## 前端菜单收敛与监控重建（2026-09-04）

### 已落地：三个菜单入口暂时隐藏

`view/src/components/SharedComponents.js` 的 NavBar 模板中，**监控 / 日志 / 链路追踪** 三个 `<a>` 已各自包进跨三行注释块，统一标记 `[暂时隐藏-勿删]`（可 `grep -rn "\[暂时隐藏-勿删\]" view/` 定位）。注释块内含隐藏原因、页面代码位置与恢复方法；**页面代码全部保留**（`src/monitoring/`、`src/logs/`、`src/trace/`），只收菜单入口。

- 已构建并部署 tf001（`/root/code/mutong/artifacts/view`，备份 `view.bak.20260904-134620-pre3`）
- 验证方式：用产物自身的模板跑 `@vue/compiler-dom`（生产构建 `comments:false` 丢弃注释节点）→ logs / monitoring / trace 三项 0 refs，其余菜单项各 2 refs。**未在浏览器目视确认**

### tf001 后端可用性实测（2026-09-04，pid 307048，:8888）

| 页面 | 后端状况 | 证据 |
|---|---|---|
| 监控 | **后端可用**，Prometheus 已配 | 启动日志 `Prometheus metrics API configured`（`cmd/main.go:619-625`）；`/api/v1/monitoring/*` 已注册（瞬时值 + `/timeseries`） |
| 日志 | **路由已注册但数据源不可达** | `configs/config.infra.yaml` 配 `elasticsearch.addresses: [http://localhost:9200]`；`logsearch.NewESQueryService`（`services/logsearch/es_query.go:25-59`）只构造 client、**不做连通性探测**，故 `LogController` 正常注册；而 tf001 本机 `curl http://localhost:9200` 返回 000（无监听）→ 日志查询必然报错 |
| 追踪 | **路由未挂载** | 启动日志 `OpenTelemetry not configured, tracing API disabled`；`curl http://localhost:4318` 返回 000 |

> 注：以上三端 curl 均需鉴权（直接访问返回 401），故路由结论取自启动日志与配置，而非 HTTP 状态码。

### 待办

- [ ] **监控页重建为 Grafana 内嵌**（方向已定，见下）
- [ ] **日志页**：为测试/生产环境配置真实可用的 Elasticsearch 地址（`config.infra.yaml`），或让 `initializeElasticsearch` 在探测失败时返回 nil 以彻底关闭路由，避免"页面能开、查询全错"
- [ ] **修复 `view/src/monitoring/index.html:8-12` 的悬空 preload**：硬编码了 4 个构建产物名（`modulepreload-polyfill-B5Qt9EMX.js`、`common-Co0gRJD4.js`、`api-CWUkSNXl.js`、`common-Cxju-u-v.css`），这些文件在 dist 中已不存在（既存问题，非本次引入；其余页面由 Vite 注入 hash）
- [ ] **去掉 `view/src/monitoring/monitoring.js:43` 的 `pageSize=5000` 全量扫**：该请求打的是 `/k8s/resources/graph/nodes`（Nebula 全量），是内存压力诱因之一；改由 Grafana 面板自带资源选择器后此调用应整体删除
- [ ] **Nebula 清理做成可选功能**（需求未闭合）：现有 `StartPeriodicCleanup`（`services/k8sresource_nebula.go:437`）用 `time.NewTicker`，首次触发在"启动 + interval"，代码中**没有"启动即清理"路径**；用户描述的"每次运行之后会清掉已删除资源"与代码不符，需先澄清观察来源。注意 `CleanupInterval` 为 int（小时），配 0.5 会被解析成 0 并回落 24h；调成 1h 会让每 tick 的 `ForceSyncResources` 全量扫变成 24 次/天

### Grafana 内嵌 — 调研结论与设计草案（只调研，未实现）

**目标效果**：添加一个 Grafana 实例地址后，可直接在 Mutong 页面内看到面板；分"可修改面板"与"只读面板"两种形态。

**硬约束（官方文档 + 社区实践核实，非推测）**：
1. `allow_embedding` 默认 false → Grafana 响应带 `X-Frame-Options: deny`；该头由**浏览器**执行，与是否登录无关 → 跨域直接 iframe 不可行，**必须同源反向代理**
2. `cookie_samesite` 默认 `lax`，官方明确仅同 site 可用；跨站嵌入需 `None` + `cookie_secure=true`（即 Grafana 侧必须 HTTPS）
3. **Service Account token（`glsa_`）只认证 API，不认证 UI**——带 SA token 的 iframe 页面仍会 302 到登录页（官方论坛原话）
4. 页面不认 basic auth，不能靠 URL 里的 `user:pass@` 登录
5. Grafana 11+ 存在 kiosk 模式回归反馈（社区报告），版本需实测

**设计草案**：
- mutong 增加 `/grafana/*` 反向代理，挂在 `RequireAuthMiddleware` 之后 → 复用 Mutong 自身登录态做准入
- 登录态注入二选一：
  - **首选 `auth.proxy`**：反代注入 `X-WEBAUTH-USER`，Grafana 侧配 `auto_sign_up` + `whitelist`（**必须配 whitelist，否则任何人伪造头即可登录**）；不改密码、不需 Grafana 存凭证，但**需要能改 grafana.ini**
  - **退化方案**：mutong 服务端 `POST /login` 换 `grafana_session` cookie 转发；不改 grafana.ini，但需在配置里存一个 Viewer 密码
- SA token 仅用于后端侧 API 调用（列 dashboard、自检数据源连通性），不参与 UI 鉴权
- 只读形态：`/d-solo/<uid>?panelId=N&kiosk`；可修改形态：`/d/<uid>?kiosk`。读写区分靠 Grafana 侧 Viewer / Editor 角色（两个不同身份的 proxy user），而非 URL 参数
- 无 `grafana-image-renderer`（已在全集群查证不存在）→ 不能走"服务端渲染 PNG"路线

**流量控制约束（回应"反代是否流量过高"）**：
- 带宽可控：Grafana 静态资源首次约 1MB 后走浏览器缓存；持续流量 = 面板数 × 查询响应大小 ÷ 刷新间隔
- 真正的风险不是带宽，是 **vmselect 的 PromQL 压力**与反代与 mutong 主业务争抢同一进程
- 约束：单页 ≤10 面板、刷新间隔 ≥1min、面板进入视口才加载（懒加载）、**不代理 `/api/live/push`**（长连接会占满连接）
- 顺带收益：删掉 `pageSize=5000` 的 Nebula 全量扫后，mutong 自身负载下降

**待用户确认的三个问题（阻塞实现）**：
1. `dg-one/grafana` 的 `grafana.ini` 能否修改？→ 决定走 `auth.proxy` 还是"密码换 session cookie"
2. 提供 Grafana SA token 以核实：里面是否有现成可嵌入的 dashboard、数据源是否已指向 vmselect（`dg-one/vm-victoria-metrics-cluster-vmselect`，集群内 LB `10.225.14.191:8481`）——**这是当前最大未知数**，若里面是空的，"填地址就能看"不成立
3. 嵌入粒度：整盘（`/d/<uid>?kiosk`）还是按面板拼装（`d-solo` + 自建布局）？以及是否接受 Mutong 页面内的 Editor 角色可直接修改面板（RBAC 粒度问题）

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

### 安全债（认证 / 告警接入，2026-09-02 排查发现）

- [ ] **告警 webhook 实际公开无鉴权** — `/api/v1/alerts/webhook` 与 `/api/v1/alerts/external/` 在 `RequireAuthMiddleware` 白名单里（`controllers/auth_middleware.go`），而本应保护它的两个中间件 `AuthMiddleware`（IP 白名单 `MUTONG_WEBHOOK_TRUSTED_IPS`）和 `WebhookSignatureMiddleware`（HMAC 签名 `MUTONG_WEBHOOK_SECRET`，`controllers/webhook_verifier.go`）**都写了却从未 `.Use()` 挂载 = 死代码**。任何人都能伪造告警打入。
  *修复方向：在 webhook 路由组挂载签名或 IP 白名单中间件，并同步配置 Alertmanager 侧的签名/来源；或二者择一*
  *风险：🔴 高（可伪造告警触发自动诊断/自愈提议）*

- [ ] **`X-Admin-Key` 未设置即放行** — `controllers/executor_controller.go` `requireAdminKey()` 只读 `MUTONG_ADMIN_KEY`，环境变量未设置时**直接 `return true` 放行**危险执行操作。注释声称"same as MUTONG_API_KEY"，但 `MUTONG_API_KEY` 全仓仅存在于该注释、代码从未读取——文档与实现不符。
  *修复方向：未配置 admin key 时应拒绝而非放行（或强制启动时校验）；对齐注释与实际读取的环境变量*
  *风险：🟡 中（依赖部署方是否设置了 MUTONG_ADMIN_KEY）*

- [ ] **登录 access_token 作为 Bearer 不可用（认证"双轨"表象）** — `/api/auth/login` 返回的 `access_token` 与 `mutong_session` cookie 是同一个 HMAC session token，但 `BearerTokenMiddleware`（`controllers/auth_middleware.go`）的 Bearer 分支只校验 PAT/OIDC/OAuth2，从不对 Bearer 调 `sessions.Verify()`，导致同一 token 放 cookie 能用、放 Bearer 报 `invalid token`。前端全程靠 cookie，故不影响使用，但 access_token 名义签发实际不可用。
  *修复方向（方案 B，最小改动）：Bearer 分支末尾对 token 也调 `sessions.Verify()` 注入用户上下文，使 cookie/Bearer 等价*
  *风险：🟢 低（现状前端不受影响，属能力缺失而非漏洞）*

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

- [x] **AI Agent 自治运维** — 检测 → 诊断 → 建议修复 → 审批执行闭环已落地：执行类 MCP 工具（安全组走自动门禁，危险动作提案式强制人工审批，含删除 Pod / rollout undo 回滚），执行后后台自动验证就绪状态、可回滚动作验证失败自动恢复变更，见 `services/mcp/exec_tools.go`、`services/executor/verify.go`
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
