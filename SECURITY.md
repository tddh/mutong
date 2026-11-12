# 安全策略 / Security Policy

重明 (Mutong) 是一个拥有集群写权限的运维平台：它能读取 K8s 资源、开启容器终端、执行自愈动作。我们把安全视为功能的一部分，而非事后加固。本文说明支持版本、漏洞报告流程，以及当前代码中已落地的安全控制清单。

## 支持版本

| 版本 | 支持状态 |
|------|---------|
| `0.1.x`（即将发布首个 tag） | ✅ 活跃支持，接收安全修复 |
| `develop` 分支 | ✅ 活跃开发，安全修复优先合入 |
| `0.1.0` 之前的快照 / 未打 tag 的历史提交 | ❌ 不支持，请升级到最新发布版 |

安全修复只针对受支持版本发布。由于平台尚未发布 1.0，minor 版本之间可能存在破坏性变更（尤其是鉴权与配置默认值），升级前请阅读 [CHANGELOG.md](CHANGELOG.md) 的 `Changed (breaking)` 段落。

## 报告漏洞

**请不要在公开 Issue、Pull Request 或讨论区报告安全漏洞。**

- 邮箱：**mutong_cm@163.com**
- 邮件标题建议以 `[SECURITY]` 开头，便于分拣。
- 若条件允许，请使用 PGP 加密；可在首封邮件中索取公钥。

请在报告中尽量包含：

1. 受影响的版本 / commit，以及部署方式（裸机、Docker Compose、K8s）。
2. 漏洞类型与影响面（例如：未鉴权读取 Secret、会话伪造、越权执行、注入、XSS）。
3. 可复现的最小步骤或 PoC（请求报文、配置片段、日志）。
4. 你建议的修复方向（可选）。

**响应承诺**：

| 阶段 | 目标时间 |
|------|---------|
| 确认收到 | 3 个工作日内 |
| 初步定级与复现结论 | 7 个工作日内 |
| 修复或缓解方案 | 视严重程度而定，P0 优先处理 |
| 公开披露 | 修复发布后协调披露，默认不晚于修复后 90 天 |

我们不会因善意安全研究而追究报告者的责任。请避免在验证过程中访问真实业务数据、执行破坏性操作或对生产环境做压力测试；如需在真实集群验证，请先与我们沟通。

## 部署方安全责任

平台提供控制机制，但**默认配置不等于安全配置**。上线前请确认：

- [ ] 所有密码/密钥通过环境变量或密钥管理系统注入，不硬编码在提交到 Git 的配置文件中
- [ ] `MUTONG_SESSION_SECRET` 已显式设置；多副本部署共享同一密钥（否则会话只在签发副本上有效）
- [ ] `MUTONG_AUTH_SECRET` 已显式设置（否则重启后所有 OAuth2 token 失效）
- [ ] `MUTONG_WEBHOOK_SECRET` 已设置，且告警发送端已按签名方案改造；未使用 `MUTONG_WEBHOOK_ALLOW_UNSIGNED=true`
- [ ] 外部告警源已配置 `MUTONG_EXTERNAL_ALERT_TOKEN` 或 `MUTONG_WEBHOOK_TRUSTED_IPS`
- [ ] `MUTONG_ADMIN_KEY` 已设置，或已确认执行器危险操作回退为要求 `admin` 角色
- [ ] 跨机反向代理已配置 `MUTONG_TRUSTED_PROXIES`（默认只信任本机回环）
- [ ] `server.enableSwagger` 保持 `false`；`/metrics` 未随意设为公开
- [ ] `auth.rbac_mode` 为 `enforce`（`shadow` / `off` 仅用于灰度或应急）
- [ ] `executor.enabled` / `executor.autoMode` 按需开启，审批流程已验证
- [ ] 终端可达范围已确认：默认拒绝系统命名空间（`kube-system`/`kube-public`/`kube-node-lease`）。注意 `terminal.allowedNamespaces` 尚未在启动时注入，**无法靠该配置进一步收敛**，需要时用 RBAC / 网络策略补偿
- [ ] kubeconfig 文件不提交到 Git，且权限最小化（平台需要 exec 权限，请单独评估）
- [ ] 生产环境 `configs/*.yaml` 不包含真实凭据；镜像只包含 `*.example` 模板
- [ ] LLM API Key 通过 `MUTONG_LLM_API_KEY` 设置；数据库密码通过 `MUTONG_DB_PASSWORD` 设置
- [ ] 数据库 / NebulaGraph / Redis 口令已改默认值，端口暴露面已收敛
- [ ] 已启用 HTTPS（或可信反代终止 TLS），使会话 Cookie 带上 `Secure`

逐项变量语义、默认值与最小可用部署示例见 [README「安全配置」](README.md#安全配置)（英文：[README.en.md](README.en.md#security-configuration)）。

## 现有安全控制清单

以下控制均已对照当前代码核实，括号内为实现位置。

### 身份认证

- **默认拒绝鉴权**：除精确白名单外所有路由要求已验证身份，不再按 `/api/` 前缀推断（`controllers/auth_middleware.go` 的 `RequireAuthMiddleware` / `isPublicPath`）。
- **四种身份来源**：PAT/SAT、OIDC access token、本地 OAuth2 access token（fosite 内省）、会话 Cookie（`BearerTokenMiddleware`）。
- **会话 Cookie 强制验签**：HMAC-SHA256 签名 + 过期校验，**已删除「cookie 值即 UUID」的回退分支**，伪造 UUID 无法登录（`services/auth/session.go`、`services/auth/mutong_session.go`）。
- **会话持续复核**：`SessionGuard` 以 30 秒 TTL 缓存用户快照，禁用用户、变更角色或修改密码后会话失效（`services/auth/session_guard.go`）。
- **Cookie 属性**：显式 `SameSite=Lax` + `HttpOnly`，`Secure` 依据 TLS / `X-Forwarded-Proto` / `MUTONG_COOKIE_SECURE`（`controllers/auth_controller.go`）。
- **密码哈希**：Argon2id，memory 64 MiB、iterations 3、parallelism 4，常量时间比较（`services/auth/password.go`）。
- **登录防爆破**：基于 Redis 的 IP 限流 + 用户名失败计数与锁定 + 验证码门槛（`services/auth/login_limiter.go`）。未配置 Redis 时该保护不可用，启动日志会提示。
- **OAuth2 / OIDC**：授权码流程带 PKCE，支持 Token 内省与撤销（`services/auth/oauth2_provider.go`、`services/auth/zitadel_oidc.go`、`controllers/oauth2/`）。
- **初始管理员**：`MUTONG_INIT_ADMIN_PASSWORD` 只在账号不存在时创建，不会每次启动重置密码（`cmd/main.go`）。

### 授权

- **Casbin RBAC**：`admin` / `operator` / `viewer` 三角色，策略按用户 UUID 绑定，登录与启动时同步角色（`services/auth/casbin.go`、`controllers/casbin_middleware.go`）。
- **动作语义收紧**：终端 WebSocket、执行器、MCP 工具调用即使使用 GET 也按 `execute` 鉴权（`actionForRequest`）。
- **灰度开关**：`auth.rbac_mode` 支持 `enforce` / `shadow` / `off`，非法值回落 `enforce`。
- **执行器二次门禁**：危险操作与审批额外校验 `X-Admin-Key`，常量时间比较；未配置密钥时要求 `admin` 角色而非放行（`controllers/executor_controller.go` 的 `requireAdminKey`）。

### 输入与注入防护

- **nGQL 注入防护**：字符串字面量统一经 sanitizer 处理，禁止手工拼接（`pkg/ngql/sanitizer.go`，`services/ngql_sanitizer.go` 为兼容入口）。
- **PromQL 标签转义**：标签值转义后再拼入表达式，避免查询注入。
- **告警 Webhook 签名**：HMAC-SHA256 覆盖「时间戳头值 + `\n` + 原始 body」，±300 秒时间窗、签名重放拒绝、body 上限 1MB（超限 413），未配置 secret 时默认拒绝（`controllers/webhook_verifier.go`）。
- **外部告警源准入**：`X-Alert-Token` 常量时间比较，或来源 IP 命中 CIDR 白名单；两者都未配置时拒绝（`controllers/webhook_verifier.go`、`controllers/auth_middleware.go`）。
- **可信代理**：默认只信任 `127.0.0.1/32,::1/128`，防止 `X-Forwarded-For` 伪造客户端 IP（`cmd/main.go` 的 `setupMiddleware`）。
- **CORS 收敛**：默认拒绝跨域，`MUTONG_ALLOWED_ORIGINS` 中的 `*` 被忽略（与带凭据请求不兼容）。
- **全局限流**：`server.rateLimitPerSec`（默认 100）（`controllers/rate_limiter.go`）。

### 数据保护

- **Token 不可逆存储**：PAT/SAT 以 `crypto/rand` 生成，只存 SHA-256 哈希，支持过期与撤销（`services/auth/token.go`）。
- **Secret 脱敏**：资源详情与图谱接口返回前删除 Secret 的 `data` / `stringData`（`services/getResource.go` 的 `redactResourceDefineString`）。
- **XSS 防护**：`/device` 页面 `html.EscapeString`（`controllers/oauth2/device.go`）；诊断 Markdown 经 `marked.parse` 后由 DOMPurify 清洗（`view/src/diagnosis/diagnosis.js`）。
- **外部搜索脱敏**：出站查询做 PII 脱敏（占位符 + 内存映射账本）+ 泛化/截断 + 高 PII 与禁用词阻断，Tavily 入站结果做 prompt injection 过滤（`services/diagnosis/sanitizer.go`、`services/mcp/search_tools.go`）。**未闭合项**：GitHub 结果未过注入过滤；审计钩子 `AuditLogFunc` 在 `cmd/main.go` 传 nil，调用元数据不落库；`Restore()` 无调用方；`configs/config.diagnosis.yaml` 的 sanitizer 段用 snake_case 键而结构体标签是 camelCase，导致 `bannedTerms` / 注入模式实际为空。详见 [docs/external-search-plan.md §0](docs/external-search-plan.md)。
- **静态资源收敛**：只服务构建产物目录（`artifacts/view` / `view/dist`），不暴露 `view/` 源码树（`cmd/main.go` 的 `registerStaticRoutes`）。

### 执行与操作安全

- **风险分级与阈值**：13 类自愈操作均带 Low / Medium / High 风险等级与 `autoThreshold`，默认 `executor.enabled=false`、`autoMode=false`。
- **LLM 不可越权**：模型输出的 `risk` 被归一化、`auto_fixable` 恒为 false，置信度不作为执行授权依据（`controllers/deep_diagnosis.go`）。
- **提案组强制人工审批**：改镜像 / 调资源 / 删 Pod / rollout undo 只生成待审批记录，任何情况下都不自动执行；强制携带告警指纹，无诊断支撑直接拒绝，同指纹去重（`services/mcp/exec_tools.go`）。
- **执行后验证与自动回滚**：后台轮询 rollout / Pod Ready（最长 4 分钟），可回滚动作验证失败时恢复「最近已知健康版本」（`services/executor/verify.go`）。
- **全量审计**：操作人、时间、目标资源、动作、结果、风险等级写入 PostgreSQL（`services/executor/pg_store.go`、`services/audit/`）。
- **终端隔离与回收**：namespace 准入默认拒绝系统命名空间 + 会话/执行审计事件（当前写服务端日志，`SetAuditStore` 未注入故不落 `operation_audit_logs`）；连接关闭时回收 K8s exec 流与 goroutine，写操作串行化（`controllers/terminal_controller.go`、`services/terminal/session.go`）。

### 暴露面与供应链

- **Swagger 默认关闭**：`server.enableSwagger=false`，开启即公开完整 API 面（`cmd/main.go`）。
- **`/metrics` 默认鉴权**：仅 `MUTONG_METRICS_PUBLIC=true` 时放行给 Prometheus 抓取。
- **无鉴权健康检查**：`GET /healthz` 只返回 `ok`，不泄露组件与版本信息，在鉴权链之前注册（`cmd/main.go` 的 `registerHealthz`）。
- **容器非 root**：多阶段构建，运行阶段使用 `mutong` 用户，`HEALTHCHECK` 打 `/healthz`（`Dockerfile`）。
- **构建上下文排除敏感文件**：`.dockerignore` 排除真实 `configs/config.*.yaml`、`kubeconfig*`、`.envrc.local`、`agent.yaml`、`artifacts/`、`.git/` 等。
- **依赖锁文件**：`go.sum` 与 `view/bun.lock` 均入库，CI 使用 `bun install --frozen-lockfile`。
- **CI 安全门禁**：golangci-lint（启用 gosec）+ `go test -race` + 前端 lint/test/build + 镜像构建；`govulncheck` 作为独立 job 扫描可达漏洞（`.github/workflows/ci.yml`）。
- **发布流水线**：tag `v*` 触发 `release.yml`，镜像推送 `ghcr.io`（`.github/workflows/release.yml`）。

## 已知限制

诚实列出当前尚未闭合的部分，便于部署方自行补偿：

- **Alertmanager 原生 webhook 不支持自定义签名头**，需要在中间加一层签名代理；否则只能依赖 `MUTONG_WEBHOOK_TRUSTED_IPS` 网络隔离。
- **登录限流依赖 Redis**，未配置 Redis 时降级为无保护，请在生产环境务必提供 Redis。
- **Webhook 重放缓存是进程内存**，多副本部署下各副本独立去重；跨副本重放需依赖时间窗与上游幂等。
- **RBAC 粒度为角色级**，尚未支持按命名空间 / 资源实例的细粒度授权。
- **用户/角色管理只有读接口**，账号变更需走数据库或 OIDC 侧，缺少管理 UI 与 API。
- **`/healthz` 不探测下游依赖**，只表示进程存活；组件级健康请使用需鉴权的 `/api/v1/monitoring/health`。
- **终端 namespace 白名单未接线**：`terminal.allowedNamespaces` 与 `TerminalController.SetAllowedNamespaces` 已实现，但 `cmd/main.go` 未注入，实际生效的只有「默认拒绝系统命名空间」；终端审计同样只写服务端日志（`SetAuditStore` 未注入）。
- **外部搜索的审计与还原未接线**：见上文「数据保护」中的未闭合项与 [docs/external-search-plan.md §0](docs/external-search-plan.md)。

## 相关文档

- [README「安全」与「安全配置」](README.md#安全) — 控制概览、变量语义、最小可用部署示例、Webhook 签名方案
- [CHANGELOG.md](CHANGELOG.md) — 安全修复与破坏性变更记录
- [docs/remediation-plan.md](docs/remediation-plan.md) — 安全与发布工程修复规划及验收清单
- [docs/PROJECT_ARCHITECTURE.md](docs/PROJECT_ARCHITECTURE.md) — 分层架构与认证链路
- [CONTRIBUTING.md](CONTRIBUTING.md) — 提交前的安全自检要求