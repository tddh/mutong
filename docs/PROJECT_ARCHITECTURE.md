# 项目架构 / Project Architecture

> **核实基线**：2026-11-12 工作区代码（`develop`，HEAD `913d6e7` + 未提交改动）。本文所有分层、流程、路由、配置项均对照源码核实，括号内为实现位置。
> **定位**：面向新加入的开发者与运维，说明「代码怎么分层、请求怎么流动、配置从哪里来」。功能清单与使用方式见 [README.md](../README.md)，安全控制清单见 [SECURITY.md](../SECURITY.md)，演进思考见 [evolution.md](evolution.md)。
> **维护约定**：架构调整、数据流变更、路由增删必须同步更新本文（见 `.agents/AGENTS.md`）。

---

## 1. 分层架构与依赖方向

```
                 ┌──────────────────────────────────────────────┐
   浏览器 / CLI   │  view/ (Vue3 ESM + Vite 多页)  cmd/mutongctl  │
                 └───────────────────┬──────────────────────────┘
                                     │ HTTP / WebSocket / SSE
                 ┌───────────────────▼──────────────────────────┐
   接入层         │  controllers/  Gin handler + 中间件           │
                 │  auth / casbin / webhook 验签 / 限流 / 路由    │
                 └───────────────────┬──────────────────────────┘
                                     │ 调用接口，不反向依赖
                 ┌───────────────────▼──────────────────────────┐
   业务层         │  services/  alert · diagnosis · executor ·    │
                 │  inspection · mcp · retrospective · auth ·    │
                 │  terminal · logsearch · prometheus · trace    │
                 └───────────────────┬──────────────────────────┘
                                     │ 依赖抽象
                 ┌───────────────────▼──────────────────────────┐
   契约层         │  interfaces/  Logger · GraphDB · MessageQueue │
                 │  Cache · LLMProvider · Executor · *Querier    │
                 └───────────────────▲──────────────────────────┘
                                     │ 实现（适配器）
                 ┌───────────────────┴──────────────────────────┐
   基础设施       │  config/  装配 + 适配器（adapters.go）         │
                 │  PostgreSQL · NebulaGraph · Kafka · BigCache  │
                 │  Redis · K8s client-go · zap                  │
                 └──────────────────────────────────────────────┘

   横切：models/（数据结构 + GORM 表）  pkg/（ngql sanitizer、safe goroutine）
   装配：cmd/main.go 唯一负责把上述各层接起来
```

**依赖规则（核实自 import 关系）**

| 规则 | 说明 |
|------|------|
| `controllers` → `services` / `interfaces` / `models` | handler 只做参数校验、鉴权上下文提取、调用服务、序列化响应 |
| `services` → `interfaces` / `models` / `pkg` | 服务面向接口编程（`interfaces.Logger`、`interfaces.GraphDB` 等），便于 mock |
| `config` → `services` + `interfaces` | **唯一的反向装配点**：`config/adapters.go` 把 zap/Nebula/Kafka/BigCache 适配成接口；`config.InitAlertService` / `InitInspectionService` / `GetLLMProvider` 直接构造服务实例 |
| `cmd/main.go` → 全部 | 组装根：配置 → 服务 → 控制器 → 路由 → 后台协程 → 优雅退出 |
| `models` 不依赖 `services` | 纯数据结构 + GORM 标签，按模块分包 |
| `pkg` 不依赖业务 | `pkg/ngql`（nGQL 字符串字面量 sanitizer）、`pkg/safe`（带 recover + 堆栈的 goroutine 封装） |

> 注意：`config` 依赖 `services` 是有意为之的装配集中化，不是循环依赖——`services` 只依赖 `interfaces`，不 import `config` 的具体实现（例外：`services/search` 接收 `config.TavilyConfig` / `config.GitHubConfig` 值对象作为参数）。

---

## 2. 目录职责表

| 目录 | 职责 | 关键文件 |
|------|------|----------|
| `cmd/` | 可执行入口 | `main.go`（Web 服务装配）、`mutongctl/`（CLI，20 个命令模块）、`resetadmin/`（管理员重置）、`mergebizapp/`（BusinessApp 顶点合并维护命令） |
| `controllers/` | HTTP 接入层：路由注册、中间件、请求/响应 shaping | `auth_middleware.go`、`casbin_middleware.go`、`webhook_verifier.go`、`rate_limiter.go`、`diagnosis_controller.go`、`oauth2/`（authorize/token/introspect/discovery/device） |
| `services/` | 业务逻辑 | `alert/`（enricher→suppressor→router→notifier）、`diagnosis/`（引擎 + Eino Agent + 混合检索 + sanitizer）、`executor/`（自愈执行 + 验证回滚 + 自动诊断管道）、`inspection/`、`mcp/`、`retrospective/`、`auth/`、`terminal/`、`search/`、`logsearch/`、`prometheus/`、`trace/`、`audit/`、`prompt/`、`httpclient/` |
| `services/*.go`（根） | K8s 资源与拓扑核心 | `k8sresource_service.go`、`k8sresource_collector.go`（Informer→Kafka）、`k8sresource_kafka.go`（消费→Nebula）、`k8sresource_nebula.go`（nGQL 写入/边清理）、`k8sresource_relationships.go`、`business_label_syncer.go`、`trace_topology_syncer.go`、`flow_topology_syncer.go`、`resource_cache.go`、`query_builder.go`、`getResource.go`（Secret 脱敏） |
| `models/` | 数据结构与持久化模型 | `user.go`、`api_token.go`、`service_account.go`、`role.go`、`k8s_models.go`；子包 `alert/ audit/ diagnosis/ executor/ inspection/ oauth2/ retrospective/` |
| `interfaces/` | 依赖反转契约 | `dependencies.go`（Logger/GraphDB/MessageQueue/Cache）、`llm_provider.go`、`executor.go`、`inspection.go`、`log_querier.go`、`metrics_querier.go`、`trace_querier.go`、`request_context.go`、`alert/` |
| `config/` | 配置加载 + 基础设施装配 + 适配器 | `config_base.go`（`Config` 结构体）、`config.go`（加载/初始化/默认值）、`adapters.go`、`sanitizer_config.go`、`hpa_templates.json` |
| `configs/` | 运行时 YAML（`config.*.yaml` + `.example` 模板） | 见 §5；子目录 `profiles/`、`prompts/`、`rules/inspection/`、`casbin_model.conf` |
| `pkg/` | 可复用基础库 | `ngql/sanitizer.go`、`safe/safe.go` |
| `view/` | 前端（Vue 3 ESM + Vite 多页，无 SFC） | `src/components/nav.config.js`（导航单一真源）、`vite.config.js`（17 个 HTML 入口） |
| `scripts/` | 运维脚本 | `schema.ngql`（Nebula DDL）、`init_postgres.sh`、`init_es_template.sh`、`update_nebula.sh`、`smoke_test.sh` |
| `deploy/` | 部署配套 | Beyla / OTel Collector Helm values、`entrypoint.sh`、`Dockerfile` |
| `docs/` | 设计与规划文档 | 本文、`ngql.md`、`mutongctl-usage.md`、`TODO.md`、`remediation-plan.md`、`business-topology-plan.md`、`external-search-plan.md`、`evolution.md` |

---

## 3. 启动装配顺序（`cmd/main.go`）

1. `GOMAXPROCS`（`MUTONG_MAX_PROCS` 可覆盖）→ `config.NewConfig(-c configs/)`：加载 YAML → `SetDefaultConfig()`（日志 → 资源画像 → BigCache → 环境变量覆盖 → 基础设施 → 各类默认值）。
2. `startDiagnostics`：`--scope-server-addr` 启用 Pyroscope、`--gops` 启用 gops；两者失败都只记日志，不阻断启动。
3. `initOAuth2`：构造 fosite provider + Redis 客户端（`MUTONG_REDIS_ADDR` 优先，其次 `diagnosis.session.redis`）。
4. `resolveSessionSecret`：`MUTONG_SESSION_SECRET` → `auth.global_secret` → `MUTONG_SESSION_SECRET_FILE`（默认 `data/session_secret`）→ 新生成 32 字节并以 0600 落盘。
5. `initializeServices`：RoleService / UserService / K8sResoureService / AlertService / BusinessLabelSyncer / TraceTopologySyncer / FlowTopologySyncer。
6. `initializeControllers` + `InitCollectWithContext`：启动 Informer 采集协程与 Kafka 消费协程。
7. `InitInspectionService` + `Start()`：Cron 调度巡检。
8. `initializeGin`：`registerHealthz`（**先于中间件链**）→ `setupMiddleware` → 静态资源 → OAuth2/OIDC/auth 路由 → Casbin 初始化 → 各业务控制器 → 诊断引擎/执行器/自动诊断管道/复盘服务。
9. 启动 HTTP Server（`MUTONG_ADDR` / `MUTONG_PORT` 可覆盖 `server.address/port`），注册 SIGINT/SIGTERM 优雅退出：停采集、执行器、诊断引擎、会话管理、自动诊断、终端清理，再 `srv.Shutdown`。

---

## 4. 关键流程

### 4.1 告警接入 → 诊断 → 执行提案 / 自动执行

```
Alertmanager / 外部系统
   │ POST /api/v1/alerts/webhook        POST /api/v1/alerts/external/:source
   ▼
[AuthMiddleware] 来源 IP 白名单（MUTONG_WEBHOOK_TRUSTED_IPS，可选）
[WebhookSignatureMiddleware]
   • webhook：HMAC-SHA256(secret, 时间戳头值 + "\n" + 原始 body)，hex 小写比对
     头：X-Alertmanager-Signature / X-Alertmanager-Timestamp；时间窗 ±300s；
     签名重放拒绝（进程内存）；body 上限 1MB（超限 413）；
     未设 MUTONG_WEBHOOK_SECRET 时默认拒绝，除非 MUTONG_WEBHOOK_ALLOW_UNSIGNED=true
   • external：X-Alert-Token 常量时间比对，或依赖 IP 白名单；两者都未配置则拒绝
   ▼
AlertService.Process → processSingleAlert（并发处理每条告警）
   enricher.Enrich(2s 超时，失败降级) → suppressor.CheckSuppression → router.Route
   → storage.Save → aggregator.Add → notifier.Notify（未抑制时）
   → go autoDiagnosisPipeline.OnAlertProcessed
   ▼
AutoDiagnosisPipeline（executor.enabled 且 AlertService 就绪时装配）
   入队（满则丢弃并告警日志）→ ShouldAutoDiagnose：
     status=firing ∧ severity∈{warning,critical} ∧ 未被抑制 ∧ ResourceType∈{Pod,Node,Deployment}
   → engine.Diagnose → 最高根因置信度 < 0.8 时调用 DeepDiagnoser（工具型深度排查）
   → 结果写 Redis 缓存 + PostgreSQL
   → RemediationBridge.CreatePlanFromDiagnosis(res, autoMode)
        ├ shouldAuto=true  → bridge.ExecuteRemediation → K8sExecutor.Execute
        └ shouldAuto=false → RecordAudit（"pending approval"，等待人工审批）
```

**诊断引擎双路径**（`services/diagnosis/engine.go`）：先跑规则根因分析（`analyzeRootCause`，按 Pod/Node/Deployment/Service 分支）+ `ContextCollector` 并行采集（拓扑、指标、日志、关联告警、知识库/向量检索，信号量默认 4）；加权置信度低于 `diagnosis.confidenceThreshold` 且配置了 LLM 时，走 Eino ReAct Agent（失败回退 HTTP provider，再失败回退纯规则路径）。

**执行门禁**（`services/executor/executor.go`）：

- 13 种动作：`restart_pod`、`delete_pod`、`scale_deployment`、`create_hpa`、`update_hpa`、`update_configmap`、`update_secret`、`update_resource_limits`、`update_deployment_image`、`update_annotations`、`update_labels`、`rollout_restart`、`rollout_undo`（`models/executor/executor.go`）。
- `plan.ApprovedBy` 非空 = 已人工审批，直接执行；否则必须 `autoMode=true` 且通过 `canAutoExecute`：**动作白名单只有 `restart_pod`** + 配置侧 `executor.actions.<action>.risk=low` 且 `autoThreshold>0` + 置信度达阈值。LLM 自报的 risk/confidence 不构成授权。
- 执行后 `startVerification` 后台轮询 rollout / 新 Pod Ready，失败时对可回滚动作恢复「最近已知健康版本」或变更前快照（`verify.go`）。
- 护栏：`maxRestartCount`（默认 5）、`coolDownMinutes`（默认 5）、`gracePeriodSec`（默认 30）、`maxReplicas`（默认 100）。
- HTTP 侧二次门禁：`/api/v1/executor/execute|toggle|diagnose-and-execute|audit/:id/approve` 均要求 `X-Admin-Key`（`MUTONG_ADMIN_KEY`，常量时间比较）；未配置该密钥时要求 `admin` 角色（`controllers/executor_controller.go`）。
- MCP 执行类工具分两组（`services/mcp/exec_tools.go`）：安全组 `restart_pod_safe`/`rollout_restart`/`scale_deployment` 走 autoMode+审批门禁；提案组 `update_deployment_image`/`adjust_resource_limits`/`delete_pod`/`rollout_undo` **只生成待审批记录，任何情况下不自动执行**，且强制携带告警指纹。
- 审计落库：`NewK8sExecutor` 构造时只要 DB 可用就自动接线操作审计表（`executor.auditLog.operationTable`，默认 `operation_audit_logs`）；执行记录本身仅在 `executor.auditLog.type=postgres` 时落 `auditLog.table`，否则存内存。（终端审计存储已在 `main.go` 经 `SetAuditStore` 注入，DB 可用时落 `operation_audit_logs`。）

### 4.2 认证与授权链路

**中间件顺序**（`setupMiddleware`，顺序即安全语义）：

| # | 中间件 | 作用 |
|---|--------|------|
| 1-3 | `gin.Logger` / `gin.Recovery` / `TraceIDMiddleware` | 日志、panic 恢复、`X-Trace-ID` 注入 |
| 4 | `SetTrustedProxies` | 默认只信 `127.0.0.1/32,::1/128`，`MUTONG_TRUSTED_PROXIES` 可扩展 |
| 5 | `cors.New` | `MUTONG_ALLOWED_ORIGINS` 显式列举，默认空（拒绝跨域），`*` 被忽略 |
| 6 | `AuthMiddleware` | 告警入口来源 IP 白名单 |
| 7 | `WebhookSignatureMiddleware` | 告警签名/时间窗/重放/body 上限（见 §4.1） |
| 8 | `RateLimitMiddleware` | 按 IP 令牌桶，`server.rateLimitPerSec`（默认 100） |
| 9 | `BearerTokenMiddleware` | 解析身份，写入 `user_id`/`user_uuid`/`role`/`token_type` |
| 10 |  legacy 重定向 | `/k8s/*`、`/users`、`/roles` → `/api/v1...`（308，**保留 query string**） |
| 11 | `RequireAuthMiddleware` | **默认拒绝**：非 `isPublicPath` 且无 `user_id` → 401 |
| 12 | `CasbinMiddleware` | RBAC；无 `user_uuid` 一律 403（即使 shadow 模式） |

`GET /healthz` 在中间件链之前注册，因此完全无鉴权、只返回 `200 ok`（容器 HEALTHCHECK 用）。公开路径白名单：`/`、`/login`、`/oidc/login`、`/device`、`/healthz`、`/api/auth/login`、`/api/auth/login-status`，前缀 `/api/auth/captcha`、`/api/auth/oidc/`、`/api/v1/alerts/webhook`、`/api/v1/alerts/external/`、`/oauth2/`、`/view/`；`/metrics` 仅当 `MUTONG_METRICS_PUBLIC=true` 时公开。

**四条身份来源**（`BearerTokenMiddleware`）：

1. **PAT / SAT**：`Authorization: Bearer mtp_…`（个人）或 `mts_…`（服务账号）。SHA-256 哈希查 `api_tokens`，校验未撤销/未过期，并复核所属用户或 ServiceAccount 仍 `active`；更新 `last_used_at`；scopes 全为 `read`/`read:*` 时拒绝写方法（403）。
2. **OIDC access token**：`ZitadelOIDCClient.ValidateAccessToken` → 按 `zitadel_user_id`（subject）查本地 active 用户。
3. **本地 OAuth2 access token**：fosite `IntrospectToken` → 按 `uuid` 或 `zitadel_user_id` 查 active 用户。
4. **会话 Cookie**：`mutong_session`，HMAC-SHA256 验签 + 过期校验 + `SessionGuard`（30s TTL 缓存用户快照，禁用/改角色/改密码即失效）。**不存在「cookie 值即 UUID」的回退分支**。

**本地登录**（`POST /api/auth/login`）：Redis 登录限流（IP + 用户名失败计数与锁定；限流器不可用时 **fail-closed 返回 503**）→ 查用户 → `status=active` → Argon2id 校验 → `SessionManager.Issue`（TTL 24h，含密码指纹）→ `SyncUserRole` 重绑 Casbin 角色 → 写 Cookie（`SameSite=Lax`、`HttpOnly`，`Secure` 依 TLS / `X-Forwarded-Proto` / `MUTONG_COOKIE_SECURE`）。初始管理员由 `MUTONG_INIT_ADMIN_PASSWORD` 触发，**仅在账号不存在时创建**。

**PAT 管理**：`POST /api/auth/tokens`（默认 type=pat、默认 90 天有效期）、`GET /api/auth/tokens`、`DELETE /api/auth/tokens/:id`。token 由 `crypto/rand` 生成 32 字节，明文只返回一次，库存 SHA-256 哈希 + 12 字符前缀（`services/auth/token.go`）。

**OIDC（Zitadel）**：`auth.mode` = `local`（默认）/ `hybrid` / `zitadel`。`GET /api/auth/oidc/login` 生成 PKCE state+verifier，写入 path 为 `/api/auth/oidc`、TTL 300s 的一次性 Cookie 后 302 到 IdP；`GET /api/auth/oidc/callback` 先清 Cookie，再常量时间比对 state，用 code+verifier 换 token，按 subject 查/建本地用户，签发会话 Cookie 后跳 `/view/index.html`。

**OAuth2 授权码 / 设备流**（fosite，`services/auth/oauth2_provider.go`）：

- 授权码 + PKCE（强制 S256，禁用 plain）、refresh token、client credentials；access token 1h、refresh token 168h、授权码 10min；issuer 取 `MUTONG_ISSUER_URL`（默认 `http://localhost:8888`）；签名密钥取 `MUTONG_AUTH_SECRET`（base64）或配置。
- **设备流（RFC 8628）当前行为**：
  1. `POST /oauth2/device/auth`（别名 `POST /oauth/v2/device_authorization`）带 `client_id`+`scope` → 校验客户端存在 → 持久化 `DeviceCode`（pending，TTL 10min），返回 `device_code`、8 位 `user_code`、`verification_uri`、`verification_uri_complete`、`expires_in=600`、`interval=5`。verification URI **取自 `MUTONG_ISSUER_URL`，不取请求 Origin 头**。
  2. 用户在 `GET/POST /device` 页面输入 user_code 批准/拒绝；未登录先跳登录页。批准把授权绑定到**已验证的批准用户**（`user_id` 来自中间件，不来自请求体），状态机 `pending → authorized/denied`，条件更新保证只能决定一次。
  3. 客户端轮询 `POST /oauth2/token`，`grant_type=urn:ietf:params:oauth:grant-type:device_code`。`Exchange` 校验有效期与 `client_id` 绑定，`authorized → completed` 原子单次消费。
  4. **token 端点签发真实持久化 PAT**：`authsvc.CreateToken(db, authz.UserID, nil, "device authorization", "pat", scopes, 1)`，响应 `access_token`（`mtp_` 前缀）、`token_type=Bearer`、`expires_in=86400`（1 天）、`scope`。**不返回 `refresh_token`**；该 PAT 与其他 token 走完全相同的校验/撤销路径。
  5. 错误码遵循 RFC 8628：`authorization_pending`、`expired_device_code`、`access_denied`、`invalid_client`、`invalid_grant`（已消费）。

**授权**：Casbin RBAC（`admin`/`operator`/`viewer`，策略按用户 UUID 绑定，模型 `configs/casbin_model.conf`）。`actionForRequest` 把终端 WebSocket、`/api/v1/executor/*`、`/api/v1/diagnosis/mcp/tool/*` 一律视为 `execute`（即使是 GET），其余 GET→`read`、写方法→`write`。`auth.rbac_mode`（或 `MUTONG_RBAC_MODE`）支持 `enforce`（默认）/`shadow`（只记日志）/`off`。

### 4.3 K8s 资源采集 → NebulaGraph 拓扑

```
client-go SharedInformerFactory（dynamic informer，全命名空间，resync 默认 30min）
   → addEventHandlers / handleResourceEvent（Add/Update/Delete）
   → seedToKafka：写 kafka.topic（随包 YAML 为 mutong30），失败重试 maxRetries/backoff
   → 同时按 workloadKinds 投递 kafka.businessWorkloadTopic
[另一协程] ConsumeKafkaMessagesWithContext
   → processKafkaMessage：反序列化 unstructured → insertK8sResourceToNebula（UPSERT 顶点）
   → Relationship(uid)：按 owner/selector/service 等推导边并 insertEdge
   → 删除事件标记 deleted，由 StartPeriodicCleanup 按 nebulaCleanup  retentionDays 批量清理
```

- 资源画像（`configs/profiles/*.yml`）决定每类资源的指标 PromQL、拓扑关系与证据查询；`resourceProfiles` 内联定义优先于文件。
- 读路径：`/api/v1/k8s/resources/graph/*` 走 Nebula 查询（`query_builder.go` + `pkg/ngql` sanitizer）；`list_resources_from_cache` 走本地 Informer 缓存（`resource_cache.go`），`WaitForResourceReady` 提供就绪门禁。
- Secret 的 `data`/`stringData` 在返回前删除（`services/getResource.go`）。
- 多集群：`kubernetes.clusters[]` 声明式 kubeconfig；当前终端/执行器/诊断取 map 中第一个集群客户端（按 cluster 参数过滤仍在 ROADMAP 中）。

### 4.4 业务拓扑同步（`businessTopology.enabled`）

| 同步器 | 数据源 | 产出 |
|--------|--------|------|
| `BusinessLabelSyncer` | `kafka.businessWorkloadTopic`（K8s workload 变更） | 按标签/命名空间映射（`namespace_mapper.go`）+ 名称归一化规则生成 `BusinessApp` 顶点与 `belongs_to_app` 边；批量 UPSERT，只有顶层控制器（Deployment/StatefulSet/DaemonSet/CronJob）写 owner |
| `TraceTopologySyncer` | `kafka.traceTopic`（OTel Collector 导出的 OTLP traces） | 解析 span 的 caller/peer，补全 `BusinessApp` 并写调用边；与 BLS 共享 `relationCache` 去重 |
| `FlowTopologySyncer` | `kafka.flowTopic`（Beyla L4 network flow metrics） | 复用 TraceTopologySyncer 的解析结果补 L4 调用关系 |

查询侧：`/api/v1/business-topology/{apps,calls,graph,app}`（`business_topology_service.go`）。

### 4.5 巡检

`inspection.enabled` + `cronSpec`（随包 YAML 为 `0 */6 * * *`；代码无内置默认值，留空则不调度）→ `InspectionService.Start()` 定时执行 `InspectionEngine.ExecuteAll`（`SkipIfStillRunning`，单次 30min 超时），报告写 PostgreSQL（`InspectionReporter`）。规则来源优先级（同名先到先得）：**YAML 文件 > Go 内置 6 类 > DB 自定义规则**。YAML 规则由 `YAMLEngine` 执行，检查类型 3 种：`min_rows`、`field_contains`、`command`；内置 6 类为单点故障、CMDB 数据孤岛、资源配额、监控盲区、证书过期、镜像审计（`configs/rules/inspection/*.yml` 与 `services/inspection/rules/`）。API 支持手动执行、报告历史/详情/对比/趋势，以及规则 CRUD + toggle。

### 4.6 AI 诊断对话（SSE）

`POST /api/v1/diagnosis/chat/ask` → Eino provider 时走 `askChatWithEino`：`mcpSrv.GetEinoToolDefsAndHandlers()` **全量注册工具**（不按场景过滤）→ `NewDiagnosisAgent` ReAct 循环 → SSE 增量输出（5s 心跳保活，外层 180s 超时，`server.sseWriteTimeoutSec` 默认 300s）；首个问题即创建会话（侧栏立即可见），会话与消息落 Redis + PostgreSQL。旧 HTTP provider 路径仍用 `SelectTools` 做场景过滤。MCP 工具共 **29 个 = 22 诊断 + 7 执行类**（`services/mcp/server.go` 的 `ListTools` + `exec_tools.go` 的 `execToolDefs`，后者仅在注入执行器后出现；`search_knowledge_base`/`search_github_issues` 仅在 `external_search.enabled` 且对应客户端就绪时注册）。

---

## 5. 配置体系

### 5.1 加载代码 vs 运行时 YAML

- **`config/`（Go 代码）**：`config_base.go` 定义 `Config` 及全部子结构体与 yaml/json 标签；`config.go` 负责加载与初始化。
  - `NewConfig(path)`：`LoadConfig` → `validate()`。`LoadConfig` 判断 path 是目录还是文件：目录模式 glob `config.*.yaml`，**按文件名字母序**逐个 `yaml.Unmarshal` 合并到同一个 `Config`（要求各文件顶层键不重叠）；文件模式走 viper 读单文件（向后兼容）。
  - 随后 `SetDefaultConfig()`：`initLogger` → `loadResourceProfiles` → `SetupBigCache` → **环境变量覆盖敏感值** → `initInfrastructure`（K8s 客户端 → PostgreSQL + AutoMigrate → Nebula 会话池 → 4 个 Kafka 客户端）→ `setServerDefaults` / `setKafkaInternalDefaults` / `setExecutorInternalDefaults` / `applyDefaults`。
  - `LoadConfig(path)`（包级函数）是**轻量加载器**，只读单文件 + `applyDefaults`，不初始化任何基础设施，供测试与工具使用。
- **`configs/`（运行时 YAML）**：`-c configs/` 指定目录；`*.example` 是入库模板，真实 `config.*.yaml` 不入库（`.dockerignore` 亦排除）。

| 文件 | 顶层键 |
|------|--------|
| `config.core.yaml` | `kubernetes` `postgres` `nebula` `nebulaCleanup` `kafka` `excludeLabels` `cache` `log` `server` |
| `config.alert.yaml` | `alert` `alertSources` |
| `config.auth.yaml` | `auth` |
| `config.business.yaml` | `businessTopology` `roles` |
| `config.diagnosis.yaml` | `diagnosis` `inspection` `prometheus` `external_search` `sanitizer` |
| `config.infra.yaml` | `elasticsearch` `opentelemetry` `executor` |
| `config.inspection.yaml` | `inspection` |
| `config.retrospective.yaml` | `retrospective` |
| `config.profiles.yaml` | 仅注释说明（画像已拆到 `profiles/`，可用 `resourceProfileFiles` 显式指定） |

> `inspection` 同时出现在 `config.diagnosis.yaml` 与 `config.inspection.yaml`，按字母序后者生效（当前两者取值一致）。新增配置请只放在一个文件里。

### 5.2 profiles / prompts / rules

| 目录 | 加载者 | 语义 |
|------|--------|------|
| `configs/profiles/*.yml` | `config.loadResourceProfiles()` | 每文件一个 `kind`，含 `metrics`（PromQL 模板，`{{namespace}}`/`{{name}}` 占位）、`topology`、`evidence`；`resourceProfileFiles` 显式列表优先，内联 `resourceProfiles` 不被文件覆盖 |
| `configs/prompts/*.md` | `prompt.NewManager(authsvc.ResolveConfigPath("configs/prompts"))` | 按资源类型渲染诊断 prompt（`diagnosis_{default,pod,node,deployment,service,ingress,pvc}.md`）+ `postmortem.md`；注入 `EinoLLMProvider` |
| `configs/rules/inspection/*.yml` | `config.scanRuleFiles()` | 声明式巡检规则；`inspection.ruleFiles` 为空时自动扫描该目录，支持 glob |
| `configs/casbin_model.conf` | `authsvc.InitCasbin` | RBAC 模型 |

`ResolveConfigPath` 依次尝试：相对路径 → `MUTONG_CONFIG_DIR` 下同名路径 → 可执行文件目录下同名路径，便于容器内布局。

### 5.3 环境变量覆盖（`SetDefaultConfig` / `main.go`）

`MUTONG_DB_USER` / `MUTONG_DB_PASSWORD` / `MUTONG_NEBULA_USER` / `MUTONG_NEBULA_PASS` / `MUTONG_REDIS_PASSWORD` / `MUTONG_REDIS_DB` / `MUTONG_TAVILY_KEY` / `MUTONG_GITHUB_TOKEN` 覆盖 YAML 对应值；`MUTONG_ADDR` / `MUTONG_PORT` 覆盖监听地址；`MUTONG_MAX_PROCS` 覆盖 GOMAXPROCS。其余安全相关变量（`MUTONG_SESSION_SECRET`、`MUTONG_AUTH_SECRET`、`MUTONG_WEBHOOK_SECRET`、`MUTONG_ADMIN_KEY`、`MUTONG_TRUSTED_PROXIES`、`MUTONG_ALLOWED_ORIGINS`、`MUTONG_ISSUER_URL`、`MUTONG_METRICS_PUBLIC` 等）语义与默认值见 [README「安全配置」](../README.md#安全配置)。

### 5.4 关键默认值（代码内建，非 YAML）

| 项 | 默认 | 位置 |
|----|------|------|
| `server.address/port` | `0.0.0.0` / `8888` | `setServerDefaults` |
| `server.read/write/idle/shutdownTimeoutSec` | 30 / 600 / 120 / 10 | 同上 |
| `server.sseWriteTimeoutSec` | 300 | 同上 |
| `server.rateLimitPerSec` | 100 | 同上 |
| `auth.mode` | `local` | `applyDefaults` |
| `executor.defaultNamespace` / `maxRestartCount` / `coolDownMinutes` | `default` / 5 / 5 | `setExecutorInternalDefaults` |
| `executor.gracePeriodSec` / `deleteGracePeriodSec` / `maxReplicas` | 30 / 5 / 100 | 同上 |
| `executor.defaultHPA` | min 1 / max 5 / targetCPU 50 | 同上 |
| `executor.auditLog.operationTable` | `operation_audit_logs` | 同上 |
| `nebulaCleanup` | retentionDays 7 / interval 24h / batch 1000 | `main.go` 兜底 |
| 诊断会话 Redis TTL / 结果缓存 TTL | 30min / 15min | `main.go` |
| 终端会话并发上限 / 清理 | 100 / 每 5min 清理超 30min 会话 | `terminal_controller.go`、`main.go` |

---

## 6. 主要 API 路由组

以下按各控制器 `RegisterRoutes` 核实（`cmd/main.go` 决定是否挂载）。除公开路径外均需鉴权 + RBAC。

| 路由组 | 方法与路径 | 挂载条件 | 实现 |
|--------|-----------|----------|------|
| 健康/指标 | `GET /healthz`；`GET /metrics` | 总是；`/metrics` 默认需鉴权 | `main.go`、`alert_controller.go` |
| 认证 | `POST /api/auth/login`、`GET /me`、`POST /logout`、`POST/GET /tokens`、`DELETE /tokens/:id`、`GET /login-status` | 总是 | `auth_controller.go` |
| OIDC | `GET /api/auth/oidc/login`、`GET /callback` | `auth.mode` 为 hybrid/zitadel（客户端初始化失败时仍注册，返回 503） | `oidc_controller.go` |
| OAuth2 | `GET/POST /oauth2/authorize`、`POST /oauth2/token`、`POST /oauth/v2/token`、`POST /oauth2/introspect`、`POST /oauth2/revoke`、`GET /.well-known/openid-configuration` | 总是注册；`cfg.DB` 为空时 provider 为 nil，端点不可用 | `oauth2/discovery.go` |
| 设备流 | `POST /oauth2/device/auth`、`POST /oauth/v2/device_authorization`、`GET/POST /device` | 总是注册；依赖 DB 中的 `oauth2_clients` 与 `device_codes` | `oauth2/device.go` |
| 用户/角色 | `GET /api/v1/users`、`GET /api/v1/roles` | 总是（**只读**，无 CRUD） | `user_controller.go`、`role_controller.go` |
| K8s 资源图谱 | `GET /api/v1/k8s/resources/graph/{nodes,edges,metadata,search,suggest,resource-define}`、`GET /api/v1/k8s/resources/:name` | 总是 | `k8sresource_controller.go` |
| 告警 | `POST /api/v1/alerts/webhook`、`POST /external/:source`、`GET /api/v1/alerts`、`GET /health`、`GET /sources`、`GET /:fingerprint` | AlertService 初始化成功 | `alert_controller.go` |
| 诊断 | `POST /api/v1/diagnosis/run`、`POST /rerun`、`GET /result/:id`、`GET /tools`、`POST /mcp/tool/:name` | AlertService 就绪 | `diagnosis_controller.go` |
| 诊断对话 | `POST /api/v1/diagnosis/chat/context`、`POST /chat/ask`、`GET /chat/sessions`、`GET/DELETE /chat/sessions/:id`、`GET /chat/session/by-alert`、`DELETE /chat/:sessionId` | 同上 | 同上 |
| 执行器 | `GET /api/v1/executor/status`、`GET /audit`、`POST /execute`、`POST /toggle`、`POST /diagnose-and-execute`、`POST /audit/:id/approve` | `executor.enabled` 且有 K8s 客户端 | `executor_controller.go` |
| 巡检 | `POST /api/v1/inspection/execute`、`GET /report`、`GET /reports`、`GET /reports/:id`、`GET /reports/:id/compare/:other_id`、`GET /trend`；`GET/POST /rules`、`PUT/DELETE /rules/:id`、`POST /rules/:id/toggle` | InspectionService 就绪 | `inspection_controller.go` |
| 复盘 | `GET /api/v1/retrospective/list`、`GET /timeline/:fingerprint`、`GET /causal-chain/:fingerprint`、`POST/PUT /postmortem/:fingerprint`、`GET /postmortem/:fingerprint/text`、`GET /history/:fingerprint`、`GET /knowledge/search`、`GET /resource/:resourceUID/postmortems` | AlertService 就绪 | `retrospective_controller.go` |
| 日志 | `GET /api/v1/logs/{pod,search,errors,warn,info}` | Elasticsearch 配置可用 | `log_controller.go` |
| 指标 | `GET /api/v1/metrics/{pod,node,deployment,statefulset,daemonset,service}` | 总是（未配置 Prometheus 时返回 503） | `metrics_controller.go` |
| 监控 | `GET /api/v1/monitoring/metrics/:resourceType`、`GET /timeseries`（额外要求 admin 角色）、`GET /health`、`GET /catalog` | 同上 | `monitoring_controller.go` |
| 链路追踪 | `GET /api/v1/trace/{query,spans,services}` | `opentelemetry.enabled` 且 `collectorURL` 非空 | `trace_controller.go` |
| 终端 | `GET /api/v1/terminal/{namespaces,pods,containers,ws}` | 总是（无 K8s 客户端时 503） | `terminal_controller.go` |
| 集群 | `GET /api/v1/clusters`、`GET /:name/health`、`GET /:name/stats` | 总是 | `cluster_controller.go` |
| 统计 | `GET /api/v1/stats/overview`、`GET /resource-total`、`POST /sync`、`POST /backfill-relationships` | 总是 | `stats_controller.go` |
| 状态 | `GET /api/v1/diagnosis/status`、`GET /alerts/suppression/status`、`GET /system/status` | AlertService + 诊断引擎就绪 | `status_controller.go` |
| 业务拓扑 | `GET /api/v1/business-topology/{apps,calls,graph,app}` | 总是 | `business_topology_controller.go` |
| 前端 | `GET /`→`/view/index.html`；`GET /view/*filepath`（`resolveViewDir` 按二进制同级 `view/dist` 定位、候选须含 `index.html`、`MUTONG_VIEW_DIR` 可覆盖，未登录跳 `login.html`）；`GET /login`、`GET/POST /oidc/login` 兼容重定向；`GET /swagger/*any` | Swagger 仅 `server.enableSwagger=true` | `main.go` |

---

## 7. 存储与外部依赖

| 依赖 | 用途 | 缺失时行为 |
|------|------|-----------|
| PostgreSQL（GORM） | 用户/角色/PAT/ServiceAccount/OAuth2 客户端与授权码/DeviceCode；诊断会话与结果、告警统计、复盘、巡检报告与规则、执行审计 | 告警持久化/审计/诊断归档降级为内存；auth 相关接口不可用 |
| NebulaGraph | K8s 资源顶点与关系边、BusinessApp 与调用边 | **硬依赖**，`nebula.host` 缺失会记 Error |
| Kafka（franz-go） | 4 个 topic：资源变更（`kafka.topic`）、trace（`traceTopic`）、business workload（`businessWorkloadTopic`）、flow（`flowTopic`） | 主客户端即使 broker 为空也会创建（`validate()` 只记 Warn），发布/消费随之失败；trace/business/flow 三个专用客户端在 topic 或 group 未配置时跳过创建，对应同步器不启用 |
| Redis | 诊断会话缓存、登录限流、OAuth2 相关客户端 | 会话仅内存；登录限流不可用 |
| BigCache | 资源缓存、BusinessApp 去重 relationCache | 由 `cache.enable` 控制 |
| Prometheus / Elasticsearch / OTel Collector | 指标 / 日志 / 链路查询 | 对应 API 返回 503 或不挂载 |
| LLM（OpenAI 兼容 / Eino） | 深度诊断、对话、复盘生成 | 诊断走纯规则路径 |
| Tavily / GitHub API | 外部知识检索 | `external_search.enabled=false` 时工具不注册 |

AutoMigrate 表清单见 `config.migrateDiagnosisTables` / `migrateAuthTables`；Nebula DDL 见 `scripts/schema.ngql` 与 [ngql.md](ngql.md)。

---

## 8. 已知实现缺口（文档如实记录，勿据此假设已生效）

| 缺口 | 现状 | 影响 |
|------|------|------|
| 外部搜索审计未落库 | `mcp.AuditLogFunc` 钩子已实现，`main.go` 传日志闭包；无 `external_search_audit` 表 | 调用元数据仅写服务端日志，不落库 |
| 搜索词提取器为截断实现 | `ExtractSearchFingerprint` 只按 `maxQueryLength` 截断，未实现 `external-search-plan.md` §5.2 的正则指纹提取 | 搜索词质量依赖 LLM 自身构造 |
| 多集群按参数过滤 | 终端/执行器/诊断取集群 map 的第一个客户端 | 多集群下操作目标可能非预期，见 ROADMAP v0.3.0 |

---

## 9. 相关文档

- [README.md](../README.md) / [README.en.md](../README.en.md) — 功能清单、部署、API 明细、安全配置
- [SECURITY.md](../SECURITY.md) — 安全控制清单与部署方责任
- [docs/ngql.md](ngql.md) — NebulaGraph Schema 与查询
- [docs/mutongctl-usage.md](mutongctl-usage.md) — CLI 使用
- [docs/business-topology-plan.md](business-topology-plan.md) / [docs/external-search-plan.md](external-search-plan.md) — 专项设计与落地状态
- [docs/remediation-plan.md](remediation-plan.md) / [docs/TODO.md](TODO.md) / [ROADMAP.md](../ROADMAP.md) — 修复规划与待办