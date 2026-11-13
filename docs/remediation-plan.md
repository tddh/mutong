# 重明修复规划（2026-10）

> 依据：2026-10-21 全面检查（构建/测试/竞态/静态分析/漏洞扫描/三路代码审计）
> 状态（2026-11 逐项对照代码复核）：**M0 安全止血已全部落地，M1 发布工程基本落地，M2 运行时质量已落地，M3 架构与测试部分落地**。§12 任务清单已按实际实现勾选；未勾选项为仍未完成或无法在本机验证的部分，均附剩余工作说明。安全验收脚本（§9.2）与预发冒烟仍需在真实环境执行一次。
> 范围：`controllers/`、`services/`、`cmd/`、`config/`、`view/`、CI 与部署文件
>
> 工具链现状：`go.mod` 声明 `go 1.27.4`，CI 使用 Go `1.27.x` + golangci-lint `v2.14.0`，`Dockerfile` 基于 `golang:1.27-alpine`。下文 §1 的 Go 1.25/1.26 数字是 **2026-10-21 的历史基线**，不代表当前状态。

---

## 1. 检查基线（2026-10-21 实测，历史快照）

| 项 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | 通过 |
| `go test ./...` / `go test -race ./...` | 全绿，无竞态报告 |
| 语句覆盖率 | 18.9%（mcp/terminal/logsearch/search/trace 为 0%） |
| `golangci-lint`（Go 1.26 工具链） | 9 项：2 errcheck、3 gofmt、1 gosec、1 gosimple、2 unused |
| `govulncheck` | 47 个可达漏洞（Go 1.26.0 标准库 + x/net、grpc、go-jose、pgx、otel） |
| 前端 `eslint` | 0 error / 57 warning |
| 前端 `vitest` | 2 文件 / 7 用例失败（Node 26 + vitest 4 的 localStorage 问题） |
| 前端 `vite build` | 成功，最大 chunk 1.45MB（@antv/g6） |
| CI | Go 1.25 + golangci-lint `latest`(v2) + v1 配置；无前端 job |

**2026-11 复核对照**（逐项核实位置见 §12）：

| 项 | 当前状态 |
|---|---|
| 工具链 | `go.mod` = `go 1.27.4`；CI Go `1.27.x`；`Dockerfile` = `golang:1.27-alpine` |
| CI job | lint / test / build / frontend / docker 五个 job 已就位（`.github/workflows/ci.yml`），本轮新增 govulncheck job 与 `release.yml` |
| 前端测试 | `view/vitest.setup.ts` 注入 jsdom Storage，`.nvmrc` 固定 Node 24，`view/src/__tests__/` 下 14 个测试文件 |
| 前端规范 | `view/eslint.config.js` + `.prettierrc` + `eslint-config-prettier`，`bun run lint` / `format` / `format:check` |
| 依赖锁文件 | `go.sum` 与 `view/bun.lock` 均入库，CI 用 `bun install --frozen-lockfile` |
| 覆盖率 / govulncheck 清零 | **未在本机复测**，不声称达标；需以 CI 产物为准 |

---

## 2. 目标与出口标准

**目标状态**

1. 四条 P0 攻击链不可复现（匿名 → 读 Secret/UUID → 伪造会话 → 驱动执行器）。
2. 任意登录用户不能执行写集群操作；授权按角色生效。
3. CI 三个 job + 前端 job 全绿；镜像可重复构建；前端测试 69/69 通过。
4. 已知运行时崩溃/泄漏点清除；覆盖率提升到 ≥30%（核心包优先）。

**出口标准（Release Gate）**

- 安全验收脚本全部通过（见 §9.2）。
- `go test -race ./...`、前端 `test/lint/build`、`govulncheck` 全绿。
- 预发环境完成一次「告警 → 诊断 → 审批 → 执行」与「终端」冒烟。

---

## 3. 修复原则

- **默认拒绝**：鉴权与授权以「注册点」为准，不再依赖路径前缀推断。
- **服务端权威**：风险等级、置信度、可执行性不得采信 LLM 或请求体自报值。
- **先观测后强制**：RBAC、webhook 校验先 shadow/log-only，再切 enforce。
- **小步可回滚**：每批独立提交，附回滚方式；安全修复优先于重构。
- **每项带验收**：没有可执行验收命令的条目不进入开发。

---

## 4. 里程碑总览

| 批次 | 工期 | 内容 | 出口标准 |
|---|---|---|---|
| **M0 安全止血** | 2–3 人日 | S0-1…S0-4、Secret 脱敏、XSS | 四条攻击链不可复现 |
| **M1 发布工程** | 2 人日 | Dockerfile、CI、锁文件、依赖、前端测试 | CI 全绿 + 镜像构建成功 |
| **M2 运行时质量** | 3–4 人日 | 终端泄漏、聚合器 panic、吞错、状态码、越界 | 压测无泄漏/无 crash |
| **M3 架构与测试** | 1–2 周 | config 拆分、goroutine 生命周期、测试补齐 | 覆盖率 ≥30%，巨型文件下降 |
| **M4 持续** | 常态化 | 依赖扫描、文档同步、前端包体 | 无新增 P0/P1 |

> M0 与 M1 可并行（不同文件集）；M2 依赖 M0 的鉴权改造先落地，避免返工。

---

## 5. M0：安全止血

### S0-1 路由鉴权矩阵（P0）

- **问题**：`RequireAuthMiddleware` 只拦 `/api/`；`/users`、`/roles`、`/k8s/resources/**`、`/metrics`、`/swagger/*any` 匿名可达。
- **证据**：`controllers/auth_middleware.go:202-205`、`k8sresource_controller.go:21-28`、`user_controller.go:19`、`role_controller.go:19`、`alert_controller.go:252`、`cmd/main.go:547`。
- **改动**
  1. 中间件改为**默认拒绝**：白名单用精确路径（`/api/auth/login`、`/api/auth/captcha*`、`/api/auth/oidc/*`、`/api/v1/alerts/webhook`、`/api/v1/alerts/external/:source` 需另加校验）；其余一律要求 `user_id`。
  2. 把 `/k8s/resources/**`、`/users`、`/roles` 迁到 `/api/v1/` 下；旧路径保留 308 重定向（保留 query string）。
  3. `/metrics` 仅内网或加 Bearer；`/swagger` 增加 `server.enableSwagger` 配置，默认 false。
  4. 路由注册改为表驱动，注册时声明 `public/auth/admin` 级别。
- **验收**：无凭证逐个 curl 新路由清单，全部 401/403；`auth_middleware_test.go` 增加表驱动用例。
- **回滚**：保留旧路由 308 重定向与白名单开关（`server.strict_auth: true`）。
- **估时**：1 人日（路由迁移 + 测试）。
- **落地情况（2026-11）**：1–3 已实现——`RequireAuthMiddleware` 改为默认拒绝 + `isPublicPath` 精确白名单；`/k8s/resources/**`、`/users`、`/roles` 已迁到 `/api/v1/` 并保留 308 重定向（保留 query string，避免 graph/search 必填参数丢失）；`server.enableSwagger` 默认 false，`/metrics` 需 `MUTONG_METRICS_PUBLIC=true` 才公开。**第 4 项表驱动路由注册未做**，各控制器仍自行 `RegisterRoutes`；`server.strict_auth` 开关也未引入（默认拒绝已是硬行为）。

### S0-2 会话认证（P0）

- **问题**：`SessionManager.Verify` 失败后把 cookie 原值当 UUID 查库；`sessions == nil` 时登录直接下发 UUID。
- **证据**：`controllers/auth_middleware.go:165-178`（关键 `:173`）、`auth_controller.go:96-108`。
- **改动**
  1. 删除 legacy 回退分支；cookie 只接受 HMAC 验签通过且未过期的 token。
  2. `sessions == nil` 时登录返回 503，并在启动日志给出明确错误（不得退化为 UUID）。
  3. Cookie 显式 `SameSite=Lax`（或 Strict）+ `Secure` 依据配置/`X-Forwarded-Proto`；新增 `server.cookie_secure`。
  4. 删除 `oauth2/token.go:42` 的 `mutong_session=1`。
- **验收**：`Cookie: mutong_session=<任意 UUID>` → 401；过期/篡改 token → 401；正常登录 → 200。
- **迁移**：上线即强制重新登录（旧 UUID cookie 全部失效），在 CHANGELOG 注明。
- **估时**：0.5 人日。
- **落地情况（2026-11）**：全部实现。UUID 回退分支已删除（`controllers/auth_middleware.go` 注释明确说明原因）；`sessions == nil` 时登录返回 503；`setSessionCookie` 显式 `SameSite=Lax` + `HttpOnly`，`Secure` 由 `cookieSecure()` 依据 `MUTONG_COOKIE_SECURE` / 请求 TLS / `X-Forwarded-Proto` 判定（**最终用环境变量而非 `server.cookie_secure` 配置键**）；`controllers/oauth2/token.go` 已无 `mutong_session` 写入。额外落地：`SessionGuard` 以 30s TTL 复核用户状态/角色/密码指纹，禁用或改密后会话失效。

### S0-3 授权与执行器（P0）

- **问题**：Casbin 中间件未挂载；`seedPolicies` 把角色名当用户名；`SyncUserRole` 未调用；`MUTONG_ADMIN_KEY` 未配置时危险操作直接放行。
- **证据**：`controllers/casbin_middleware.go:11-44`（无 `.Use()`）、`services/auth/casbin.go:53-69`、`controllers/executor_controller.go:48-52`、`cmd/main.go:584-591`。
- **改动**
  1. 修正策略主体：按 `user.UUID` 绑定，登录/建号时调用 `SyncUserRole`；角色策略 `admin/operator/viewer` 分别授权。
  2. 挂载 `CasbinMiddleware`，先 `shadow` 模式（仅记录 deny 不拦截），确认无正常业务被误杀后切 enforce。
  3. 危险路由（`/api/v1/executor/*`、`/api/v1/terminal/*`、`/api/v1/diagnosis/mcp/tool/:name`）叠加 `RequireRole("admin")` 或 operator 白名单。
  4. `MUTONG_ADMIN_KEY` 未配置时对危险操作返回 503；比较改用 `subtle.ConstantTimeCompare`。
  5. `Executor.Execute` 的 `Confidence` 由服务端计算/校验，不采信请求体。
- **验收**：viewer/operator/admin 三角色 × 危险路由矩阵测试，预期 403/200 与表一致。
- **回滚**：`auth.rbac_mode: shadow|enforce|off` 配置开关。
- **估时**：1.5 人日。
- **落地情况（2026-11）**：1–3、5 已实现——策略按 `user.UUID` 绑定，登录时调 `SyncUserRole`，启动时同步所有活跃用户；`CasbinMiddleware` 已在 `setupMiddleware` 挂载，`auth.rbac_mode` 支持 enforce/shadow/off（非法值回落 enforce）；终端 WS / 执行器 / MCP 工具调用即使 GET 也按 `execute` 鉴权。**第 4 项的最终行为与原计划不同**：`MUTONG_ADMIN_KEY` 未配置时不返回 503，而是回退为要求 `admin` 角色（见 CHANGELOG `Changed (defaults)`），配置了密钥则用 `subtle.ConstantTimeCompare` 比较。第 5 项：`controllers/deep_diagnosis.go` 归一化模型输出的 risk 且 `AutoFixable` 恒为 false，`services/executor/executor.go` 明确「LLM 自报的 plan.Risk / plan.Confidence 不能作为唯一依据」。

### S0-4 告警入口（P0）

- **问题**：两个防护中间件均未挂载；IP 判断路径写成 `/api/alert/webhook`；时间戳未校验。
- **证据**：`auth_middleware.go:20-37,196-197`、`webhook_verifier.go:23,100-112`、`alert_controller.go:40-72,244-245`。
- **改动**
  1. 实际挂载 `WebhookSignatureMiddleware`；未配置 `MUTONG_WEBHOOK_SECRET` 时默认拒绝（或仅允许内网 + 显式开关）。
  2. 校验 `X-Alertmanager-Timestamp`，偏差 > 300s 拒绝；nonce/指纹去重防重放。
  3. `AuthMiddleware` 的 IP 白名单改为挂载，路径修正为 `/api/v1/alerts/webhook`；外部源 `/api/v1/alerts/external/:source` 使用独立 token。
  4. secret 比较沿用 `hmac.Equal`。
- **验收**：无签名 401、错签名 401、旧时间戳 401、重复 nonce 401、正确签名 200。
- **灰度**：先 log-only 记录一周，再强制。
- **估时**：0.5 人日。
- **落地情况（2026-11）**：全部实现，且签名方案比原计划更强。`AuthMiddleware` 与 `WebhookSignatureMiddleware` 均已在 `cmd/main.go` 的 `setupMiddleware` 挂载，路径前缀为 `/api/v1/alerts/webhook` 与 `/api/v1/alerts/external/`；时间戳头 `X-Alertmanager-Timestamp` 偏差 > 300s 拒绝；签名头 `X-Alertmanager-Signature`。**签名 payload 已升级为 `HMAC-SHA256(secret, 时间戳头值 + "\n" + 原始 body)`**（原计划只签 body），使抓到的签名无法配新时间戳重放；另有进程内签名重放去重、`http.MaxBytesReader` 限制 body 1MB（超限 413）。未配置 `MUTONG_WEBHOOK_SECRET` 时默认拒绝，`MUTONG_WEBHOOK_ALLOW_UNSIGNED=true` 可显式放行。外部源走 `X-Alert-Token`（常量时间比较）或 `MUTONG_WEBHOOK_TRUSTED_IPS`，两者都未配置时拒绝。**未做 log-only 灰度阶段**，直接强制。

### S0-5 LLM → 执行链路（P0）

- **问题**：LLM 输出的 `risk/auto_fixable/confidence` 直接驱动执行；工具名不校验白名单。
- **证据**：`controllers/deep_diagnosis.go:62-82`、`services/executor/auto_diagnosis.go:225-262`、`services/executor/bridge.go:47-78`、`controllers/diagnosis_controller.go:480,925`。
- **改动**
  1. 执行动作的 `risk/confidence` 由服务端规则计算；LLM 建议只能作为 `proposal`。
  2. 工具调用校验 `tc.Name` 属于本次下发的工具集合；`/diagnosis/mcp/tool/:name` 仅 admin + 只读白名单（或 debug 模式）。
  3. 自愈默认 `autoMode=false`；自动执行要求显式配置 + 人工审批通道。
  4. 进入 prompt 的告警/日志标注「不可信数据」，并做注入模式过滤。
- **验收**：注入 payload（summary/日志中要求输出高置信度）不产生执行计划；自动模式关闭时全部进入待审批。
- **估时**：1 人日。
- **落地情况（2026-11）**：1–3 已实现。模型输出的 risk 被归一化、`AutoFixable` 恒为 false（`controllers/deep_diagnosis.go`）；`Server.ExecuteTool` 对未注册工具名直接返回 `unknown tool`，`POST /diagnosis/mcp/tool/:name` 对执行类工具（`mcp.IsExecTool`）额外要求 admin 角色；`executor.enabled` 与 `autoMode` 默认均为 false；MCP 提案组工具走 `proposalAndRecord`，强制告警指纹 + 无诊断置信度即拒绝 + 同指纹同动作去重，**任何情况下都不自动执行**。**第 4 项部分实现**：外部搜索结果已有 prompt injection 模式过滤与 banned terms（`services/diagnosis/sanitizer.go`），但进入 prompt 的告警/日志尚未统一标注「不可信数据」。

### S0-6 敏感数据与 XSS（P0/P1）

- **Secret 脱敏**：`services/getResource.go` 的 `GetResourceDefine`/`Get` 返回前删除 `Secret.data`/`stringData`；入库侧同样裁剪。验收：返回体不含 `data` 字段。
- **`/device` XSS**：`controllers/oauth2/device.go:171,245` 使用 `html.EscapeString` 或 `html/template`。
- **前端 Markdown XSS**：`view/src/diagnosis/diagnosis.js:10-13,662` 引入 DOMPurify（`marked.parse` 后 `DOMPurify.sanitize`）；同时加 CSP 头。
- **验收**：`code="><script>...` 不执行；LLM 输出 `<img onerror>` 被清洗。
- **估时**：0.5 人日。
- **落地情况（2026-11）**：Secret 脱敏已实现（`services/getResource.go` 的 `redactResourceDefineString` 删除 `data`/`stringData`，资源详情与图谱两条路径都过）；`/device` 已用 `html.EscapeString`（`controllers/oauth2/device.go`）；诊断 Markdown 已 `DOMPurify.sanitize(marked.parse(text))`（`view/src/diagnosis/diagnosis.js`）。**CSP 响应头未添加**，全仓无 `Content-Security-Policy` 设置点，属剩余项。

---

## 6. M1：发布工程

| 编号 | 改动 | 验收 | 估时 |
|---|---|---|---|
| E0-1 | `Dockerfile`/`deploy/Dockerfile` 改 `golang:1.27-alpine`；根镜像加 `USER mutong` | 干净环境 `docker build` 成功 | 0.25d |
| E0-2 | CI：`golangci-lint-action@v9` + 固定 `v2.14.0`，`golangci-lint migrate` 迁移配置；PR 增加 `develop` | lint job 绿 | 0.5d |
| E0-3 | 删除 `.gitignore` 的 `view/bun.lock`，提交锁文件；`package.json` 加 `packageManager`/`engines.node` | 干净检出 `bun install --frozen-lockfile` 成功 | 0.1d |
| E0-4 | 新增 `.dockerignore`：排除 `configs/config.*.yaml`、`kubeconfig*`、`artifacts/`、`.git/`、`view/node_modules/`、`*.log`、`coverage.out` | `docker build` 上下文不含真实配置 | 0.1d |
| E0-5 | `deploy/entrypoint.sh` 写入可写目录或 `chown -R mutong:mutong /app/configs`；根镜像补 entrypoint；无配置时输出可读错误而非 panic | 空配置首启可诊断，不再静默 CrashLoop | 0.25d |
| E0-6 | 新增 `frontend` CI job：`bun install --frozen-lockfile && bun run lint && bun run test && OUT_DIR=/tmp/ui bun run build` | 前端 job 绿 | 0.25d |
| E0-7 | 升级 Go 1.27.x；升级 `x/net`、`grpc`、`go-jose`、`pgx`、`otel`；直接依赖跑一次 `go mod tidy` | `govulncheck` 无可达漏洞 | 0.5d |
| E0-8 | 前端测试：`view/vitest.setup.ts` 注入 jsdom Storage + `setupFiles`；固定 Node 版本（`.nvmrc` 或 `engines`） | `69/69` 通过 | 0.1d |
| E0-9 | compose 弱口令改 `${VAR:?required}`，移除数据库端口发布 | `docker compose config` 无默认口令 | 0.25d |

> E0-8 已验证可行的修复：从 `globalThis.jsdom.window` 取 Storage 注回 `globalThis`（Node ≥25 下 `window.localStorage` 为 `undefined`）。实测修复后 14 文件 / 69 用例全通过。

---

## 7. M2：运行时质量

| 编号 | 问题 | 改动 | 验收 |
|---|---|---|---|
| Q0-1 | 终端会话/goroutine 泄漏（`CloseSession` 零调用、`context.Background()`） | ws 读循环退出时 `CloseSession`；`StartK8sExec` 用可取消 ctx；关闭 stdout writer | 开/关终端 100 次，`runtime.NumGoroutine()` 回落 |
| Q0-2 | 聚合器 ticker 置 nil 导致 panic | goroutine 启动时捕获局部 `ticker := ag.ticker`；`Stop` 只 close done | `go test -race` + 并发 Stop 测试 |
| Q0-3 | 告警投递/审计错误被 `_ =` 吞 | `errors.Join` 收集并记录 + Prometheus 计数 | 断网时 `sendAggregatedAlerts` 返回错误 |
| Q0-4 | 先 200 后 500 | `business_topology_controller.go` 改为先判错再写 | 单测断言状态码 500 |
| Q0-5 | 配置层 `panic`/`os.Exit` | `LoadConfig` 返回 error，`os.Exit` 仅在 `main` | 错误配置单测覆盖 |
| Q0-6 | `/k8s/resources/:name` 读 `Values[7]` 越界 | 按列名/长度取值 | 单测模拟 7 列返回不 panic |
| Q1 | 边写入吞错、recover 无堆栈、watch 不 Stop、`map` 无锁写 | 统一 `safe.Go` + `debug.Stack()`；watch 返回 cancel；registry 启动期构建 | `-race` + 日志含堆栈 |

---

## 8. M3：架构与测试

1. **拆分 `config`**：只保留结构体 + `LoadConfig`；装配迁到 `app/wiring.go`，按 feature 拆 `wireDiagnosis/wireAlerts/wireTerminal`。
2. **拆分 `initializeGin`**（450 行）：路由注册按域拆分，配合 §S0-1 的表驱动。
3. **`pkg/safe`**：`Go`（recover + stack）、`Loop(ctx, interval, fn)`（ticker 生命周期），替换现存裸 goroutine。
4. **测试优先级**：`services/mcp`（工具白名单/nGQL）、`services/terminal`（生命周期）、`services/auth`（会话/授权）、`controllers`（鉴权矩阵）。
5. **清理**：`services/ngql_sanitizer.go` 别名层迁移完成；删除 `controllers/controller.go` 死方法、`view/jslibs` 旧资产、`view/shared` 重复组件。
6. **前端包体**：G6 动态 import 或统一图库；当前 1.45MB chunk 只影响两个复盘页。
7. **文档同步**：`docs/TODO.md` 中「Casbin 中间件已生效」的描述现已与代码一致（中间件确已挂载），无需再修正；本规划完成后更新 `CHANGELOG.md`。

---

## 9. 验证计划

### 9.1 自动化门禁（CI 必跑）

```
go build ./...
go vet ./...
go test -race ./...
govulncheck ./...
golangci-lint run
cd view && bun run lint && bun run test && bun run build
docker build -t mutong:ci .
```

### 9.2 安全验收脚本（M0 出口，手工/半自动）

| 用例 | 预期 |
|---|---|
| 无凭证 `GET /api/v1/users`、`/api/v1/roles`、`/api/v1/k8s/resources/**`、`/metrics` | 401/403 |
| 无凭证 `GET /healthz` | 200 `ok`（唯一免鉴权探针，不泄露组件信息） |
| `Cookie: mutong_session=<admin-uuid>` | 401 |
| viewer 调用 `POST /api/v1/executor/execute` | 403 |
| 未配置 `MUTONG_ADMIN_KEY` 时非 admin 角色调用执行器 | 403（回退为要求 admin 角色，**不是 503**） |
| 无签名 / 错签名 / 过期时间戳 / 重放签名 webhook | 401 |
| webhook body 超过 1MB | 413 |
| `GET /api/v1/k8s/resources/graph/resource-define?resourceId=<Secret>` | 不含 `data` / `stringData` |
| `/device?code="><script>...` | 不执行脚本 |
| 注入告警 summary 要求高置信度执行 | 仅生成待审批，不执行 |
| 非 admin 调用 `POST /api/v1/diagnosis/mcp/tool/<执行类工具>` | 403 |
| 终端访问未列入 `terminal.allowedNamespaces` 的 namespace | 拒绝 |

### 9.3 预发冒烟

告警接入 → 诊断 → 生成计划 → 审批 → 执行 → 审计可查；终端可开/关且连接回收；RBAC 三角色行为符合矩阵。

---

## 10. 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 默认拒绝误伤未知调用方 | 业务中断 | 先 log-only 一周；保留 `strict_auth` 开关 |
| Casbin enforce 锁死管理员 | 无法操作 | 保留 `rbac_mode=off` 应急开关；先 shadow |
| 强制重新登录 | 用户体验 | 发布公告 + 登录页提示 |
| webhook 强制校验 | 告警源中断 | 灰度：先记录不拒绝，确认所有源已配签名 |
| Dockerfile 升级 | 构建缓存失效 | 固定 digest，分两步提交 |
| Go 1.27 升级 | 依赖需重编译 | 单独提交，先跑全量测试 |

---

## 11. 未验证事项

- Docker 构建与 GitHub Actions 实际执行结果（本机无 docker，未触发 Actions）；govulncheck job 与 `release.yml` 本轮新增，首跑结果待观察。
- 语句覆盖率是否达到 ≥30% 出口标准（未复测）。
- 运行时 `autoMode`、`MUTONG_ADMIN_KEY`、`MUTONG_WEBHOOK_SECRET` 的实际配置状态（决定部分漏洞可利用性）。
- 浏览器 SameSite 默认策略对跨站 WebSocket 的具体拦截行为。
- 多副本部署下的 webhook 重放去重效果（重放缓存为进程内存，各副本独立）。
- §9.2 安全验收用例尚未脚本化，也未在真实环境执行。

---

## 12. 任务清单（2026-11 逐项对照代码复核）

> 勾选依据为当前工作区代码，不是计划意图。带「剩余」字样的条目表示主体已落地但仍有未闭合部分。

### M0

- [x] S0-1 鉴权默认拒绝 + 路由迁移（`RequireAuthMiddleware` / `isPublicPath`、`/api/v1/` 迁移 + 308 保留 query、`enableSwagger` 默认 false、`/metrics` 默认鉴权）
  - 剩余：表驱动路由注册未做；`server.strict_auth` 开关未引入
- [x] S0-2 删除 UUID 回退 + Cookie 属性（HMAC 验签强制、`SameSite=Lax` + `HttpOnly` + `Secure`、`sessions == nil` 返回 503、`oauth2/token.go` 不再写 cookie）
  - 额外落地：`SessionGuard` 30s TTL 复核用户状态/角色/密码指纹
- [x] S0-3 Casbin 修正与挂载（UUID 主体 + `SyncUserRole` + enforce/shadow/off）+ adminKey 缺省拒绝
  - 行为差异：缺省 `MUTONG_ADMIN_KEY` 时回退为要求 `admin` 角色（403），**不是原计划的 503**
- [x] S0-4 webhook 签名/时间戳/IP 白名单挂载（签名 payload 含时间戳、±300s 窗口、重放去重、1MB body 上限）
- [x] S0-5 LLM 输出降级为 proposal + 工具白名单（`AutoFixable=false`、`ExecuteTool` 校验工具名、执行类工具 admin-only、`autoMode` 默认 false）
  - 剩余：进入 prompt 的告警/日志未统一标注「不可信数据」
- [x] S0-6 Secret 脱敏 + device/Markdown XSS
  - 剩余：CSP 响应头未添加
- [ ] 安全验收脚本全部通过（§9.2 用例已按实际行为更新，但**尚无脚本化实现，也未在真实环境跑过**）

### M1

- [x] E0-1 Dockerfile Go 1.27 + 非 root（根 `Dockerfile` 与 `deploy/Dockerfile` 均为 `golang:1.27-alpine`、`USER mutong`、`HEALTHCHECK /healthz`）
  - 剩余：基础镜像**未固定 digest**
- [x] E0-2 CI 迁移 golangci-lint v2 + PR develop（`golangci-lint-action@v9` + `v2.14.0`，push/PR 覆盖 master 与 develop）
- [x] E0-3 提交 `view/bun.lock`（`view/package.json` 已有 `packageManager: bun@1.2.0` 与 `engines.node: >=20 <25`）
- [x] E0-4 `.dockerignore`（排除真实 `configs/config.*.yaml`、`kubeconfig*`、`.envrc.local`、`agent.yaml`、`artifacts/`、`.git/`、`view/node_modules/`、`*.log`、`coverage.out`）
- [x] E0-5 容器首启可写/可诊断（`deploy/entrypoint.sh` 检测目录可写性，不可写时回退 `/tmp/mutong-configs` 并从 `.example` 生成；两个镜像均设 ENTRYPOINT）
- [x] E0-6 前端 CI job（`bun install --frozen-lockfile` + lint + test + build）
- [x] E0-7 Go 1.27 + 依赖升级（`go.mod` = `go 1.27.4`，CI Go `1.27.x`）
  - 剩余：`govulncheck` 清零**未在本机复测**；govulncheck job 本轮加入 CI，结果以 CI 产物为准
- [x] E0-8 vitest setup + Node 版本固定（`view/vitest.setup.ts` 注入 jsdom Storage，`.nvmrc` = 24）
- [x] E0-9 compose 口令改 `${VAR:?required}`（`POSTGRES_PASSWORD` / `NEBULA_PASSWORD` / `MUTONG_REDIS_PASSWORD` 均必填）
  - 行为差异：原计划「移除数据库端口发布」，本轮改为**补齐 NebulaGraph metad/storaged 并发布各服务端口**以便本地调试；生产暴露面需部署方自行收敛

### M2

- [x] Q0-1 终端生命周期（`controllers/terminal_controller.go` 在读循环退出时 `defer CloseSession`）
- [x] Q0-2 聚合器 ticker（`services/alert/aggregator.go` goroutine 内捕获局部 `ticker`，注释说明 `Stop()` 可能并发置 nil）
- [x] Q0-3 告警/审计错误（`services/alert/notifier.go`、`services/executor/executor.go` 用 `errors.Join` 收集并返回）
- [x] Q0-4 状态码（`controllers/business_topology_controller.go` 先判错返回 500，再写 200）
- [x] Q0-5 配置 error 化（`LoadConfig(path) (*Config, error)`，`os.Exit` 只在 main）
- [x] Q0-6 资源查询越界（见 CHANGELOG Fixed：`/api/v1/k8s/resources/:name` 读 8 列越界已修）
- [x] Q1 并发与资源泄漏清理（`pkg/safe.Go` 提供 recover + `debug.Stack()`）
  - 剩余：`safe.Go` 目前仅 2 处调用，裸 goroutine 尚未全面替换；`safe.Loop(ctx, interval, fn)` 未实现

### M3

- [x] config 拆分（结构体定义已独立到 `config/config_base.go`，加载与装配留在 `config/config.go`）
  - 剩余：装配**未迁到 `app/wiring.go`**（无 `app/` 目录），`wireDiagnosis/wireAlerts/wireTerminal` 未拆分；`initializeGin` 仍为单函数（`cmd/main.go:673-1107`，约 435 行）
- [x] pkg/safe（`pkg/safe/safe.go` 已建，含 recover + 堆栈）
  - 剩余：`Loop` 未实现，替换覆盖面不足
- [ ] 核心包测试补齐（覆盖率 ≥30%）——`services/auth` 6 个、`services/executor` 7 个、`services/diagnosis` 9 个测试文件已就位，`services/mcp`/`inspection`/`terminal` 各 1 个；`services/trace`/`logsearch`/`audit`/`prompt` 仍为 0。**覆盖率未复测，不声称达标**
- [x] 死代码/重复实现清理（重复 sanitizer 包已合并到 `pkg/ngql`，单测归位；`controller.go` 的 `K8sResourceCollect()`/`K8sResourceRelationship()` 已删；`view/jslibs`、`view/shared` 已不存在）
  - 剩余：`RegisterControllersRoutes()` 空函数仍被调用；`diagnosis_controller.go` 6 处 `nolint:unused` 废弃 chat 方法仍在
- [x] 文档与 CHANGELOG 同步（README 中英、ROADMAP、SECURITY、TODO、evolution、本规划与计划文档已按代码复核更新；新增 `docs/PROJECT_ARCHITECTURE.md`）