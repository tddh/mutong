# 重明修复规划（2026-10）

> 依据：2026-10-21 全面检查（构建/测试/竞态/静态分析/漏洞扫描/三路代码审计）
> 状态：**M0–M3 已实施**（2026-10-21）。安全验收脚本与发布门禁见 §9；上线前仍需在真实环境执行一次预发冒烟。
> 范围：`controllers/`、`services/`、`cmd/`、`config/`、`view/`、CI 与部署文件

---

## 1. 检查基线（实测）

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
  2. 把 `/k8s/resources/**`、`/users`、`/roles` 迁到 `/api/v1/` 下；旧路径保留 301（仅 GET）。
  3. `/metrics` 仅内网或加 Bearer；`/swagger` 增加 `server.enable_swagger` 配置，默认 false。
  4. 路由注册改为表驱动，注册时声明 `public/auth/admin` 级别。
- **验收**：无凭证逐个 curl 新路由清单，全部 401/403；`auth_middleware_test.go` 增加表驱动用例。
- **回滚**：保留旧路由 301 与白名单开关（`server.strict_auth: true`）。
- **估时**：1 人日（路由迁移 + 测试）。

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

### S0-6 敏感数据与 XSS（P0/P1）

- **Secret 脱敏**：`services/getResource.go` 的 `GetResourceDefine`/`Get` 返回前删除 `Secret.data`/`stringData`；入库侧同样裁剪。验收：返回体不含 `data` 字段。
- **`/device` XSS**：`controllers/oauth2/device.go:171,245` 使用 `html.EscapeString` 或 `html/template`。
- **前端 Markdown XSS**：`view/src/diagnosis/diagnosis.js:10-13,662` 引入 DOMPurify（`marked.parse` 后 `DOMPurify.sanitize`）；同时加 CSP 头。
- **验收**：`code="><script>...` 不执行；LLM 输出 `<img onerror>` 被清洗。
- **估时**：0.5 人日。

---

## 6. M1：发布工程

| 编号 | 改动 | 验收 | 估时 |
|---|---|---|---|
| E0-1 | `Dockerfile`/`deploy/Dockerfile` 改 `golang:1.26-alpine` 并固定 digest；根镜像加 `USER mutong` | 干净环境 `docker build` 成功 | 0.25d |
| E0-2 | CI：`golangci-lint-action@v9` + 固定 `v2.14.0`，`golangci-lint migrate` 迁移配置；PR 增加 `develop` | lint job 绿 | 0.5d |
| E0-3 | 删除 `.gitignore` 的 `view/bun.lock`，提交锁文件；`package.json` 加 `packageManager`/`engines.node` | 干净检出 `bun install --frozen-lockfile` 成功 | 0.1d |
| E0-4 | 新增 `.dockerignore`：排除 `configs/config.*.yaml`、`kubeconfig*`、`artifacts/`、`.git/`、`view/node_modules/`、`*.log`、`coverage.out` | `docker build` 上下文不含真实配置 | 0.1d |
| E0-5 | `deploy/entrypoint.sh` 写入可写目录或 `chown -R mutong:mutong /app/configs`；根镜像补 entrypoint；无配置时输出可读错误而非 panic | 空配置首启可诊断，不再静默 CrashLoop | 0.25d |
| E0-6 | 新增 `frontend` CI job：`bun install --frozen-lockfile && bun run lint && bun run test && OUT_DIR=/tmp/ui bun run build` | 前端 job 绿 | 0.25d |
| E0-7 | 升级 Go 1.26.10；升级 `x/net`、`grpc`、`go-jose`、`pgx`、`otel`；41 个直接依赖跑一次 `go mod tidy` | `govulncheck` 无可达漏洞 | 0.5d |
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
7. **文档同步**：修正 `docs/TODO.md` 中「Casbin 中间件已生效」的错误描述；本规划完成后更新 `CHANGELOG.md`。

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
| 无凭证 `GET /users`、`/roles`、`/k8s/resources/**`、`/metrics` | 401/403 |
| `Cookie: mutong_session=<admin-uuid>` | 401 |
| viewer 调用 `POST /api/v1/executor/execute` | 403 |
| 未配置 `MUTONG_ADMIN_KEY` 时调用执行器 | 503 |
| 无签名 / 错签名 / 过期时间戳 webhook | 401 |
| `GET /k8s/resources/graph/resource-define?resourceId=<Secret>` | 不含 `data` |
| `/device?code="><script>...` | 不执行脚本 |
| 注入告警 summary 要求高置信度执行 | 仅生成待审批，不执行 |

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
| Go 1.26.10 升级 | 依赖需重编译 | 单独提交，先跑全量测试 |

---

## 11. 未验证事项

- Docker 构建与 GitHub Actions 实际执行结果（本机无 docker，未触发 Actions）。
- 运行时 `autoMode`、`MUTONG_ADMIN_KEY`、`MUTONG_WEBHOOK_SECRET` 的实际配置状态（决定部分漏洞可利用性）。
- 浏览器 SameSite 默认策略对跨站 WebSocket 的具体拦截行为。
- `/k8s/resources/:name` 越界为代码层确定缺陷，但未在真实 Nebula 实例上复现。

---

## 12. 任务清单

### M0

- [ ] S0-1 鉴权默认拒绝 + 路由迁移 + 表驱动
- [ ] S0-2 删除 UUID 回退 + Cookie 属性
- [ ] S0-3 Casbin 修正与挂载（shadow → enforce）+ adminKey 缺省拒绝
- [ ] S0-4 webhook 签名/时间戳/IP 白名单挂载
- [ ] S0-5 LLM 输出降级为 proposal + 工具白名单
- [ ] S0-6 Secret 脱敏 + device/Markdown XSS
- [ ] 安全验收脚本全部通过

### M1

- [ ] E0-1 Dockerfile Go 1.26 + digest + 非 root
- [ ] E0-2 CI 迁移 golangci-lint v2 + PR develop
- [ ] E0-3 提交 `view/bun.lock`
- [ ] E0-4 `.dockerignore`
- [ ] E0-5 容器首启可写/可诊断
- [ ] E0-6 前端 CI job
- [ ] E0-7 Go 1.26.10 + 依赖升级 + govulncheck 清零
- [ ] E0-8 vitest setup + Node 版本固定
- [ ] E0-9 compose 口令与端口

### M2

- [ ] Q0-1 终端生命周期
- [ ] Q0-2 聚合器 ticker
- [ ] Q0-3 告警/审计错误
- [ ] Q0-4 状态码
- [ ] Q0-5 配置 error 化
- [ ] Q0-6 资源查询越界
- [ ] Q1 并发与资源泄漏清理

### M3

- [ ] config 拆分 / initializeGin 拆分
- [ ] pkg/safe
- [ ] 核心包测试补齐（覆盖率 ≥30%）
- [ ] 死代码/重复实现清理
- [ ] 文档与 CHANGELOG 同步