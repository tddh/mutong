# 牧童（mutong）项目全面评估报告

| 项目 | 说明 |
| --- | --- |
| 评估日期 | 2026-11-12 |
| 评估对象 | `gitee.com/tddh/mutong`，分支 `develop`，HEAD `913d6e7`（含未提交的工具链升级改动） |
| 评估方式 | 5 个并行深度审查（架构 / 安全 / 测试工程 / 前端 / 仓库治理）+ 实际运行构建、vet、全量测试、覆盖率、前端测试 + 对最高危结论逐条人工复核 |
| 证据标准 | 所有发现均附 `file:line` 或命令输出证据；最高危项已二次人工确认 |
| 编号规则 | 问题按类别编号：`FUNC` 功能缺陷、`SEC` 安全、`DOC` 文档、`TEST` 测试、`ENG` 工程发布、`CODE` 代码质量、`REPO` 仓库卫生；严重度分 严重 / 高 / 中 / 低 四级 |

---

## 整改状态（2026-11-12 当日完成）

本报告全部 69 项发现已于当日处置完毕：**61 项完全修复，8 项部分修复/明确遗留**（见下）。修复后实测：`go build/vet/test` 全绿，前端 lint 0 error / 69 测试全过 / 构建成功，govulncheck 影响代码漏洞 **3→0**。覆盖率：controllers 7.3%→**58.8%**、oauth2 0%→**73.5%**、cmd 3.0%→**40.0%**、executor 16.4%→**40.6%**、terminal **73.6%**。

**修复过程中新发现并已修复的问题**：默认配置 CORS 启动 panic、`Engine.Stop()` close(nil channel)、sanitizer 配置键 snake_case/camelCase 不匹配致注入模式加载为空、GitHub 搜索结果未过注入过滤、外部搜索审计钩子传 nil、终端审计存储未接线、inspection 配置重复键、deploy compose 单 storaged 与 replica_factor=2 冲突、init_postgres.sh 用户不匹配、3 个依赖漏洞（compress/quic-go/Go stdlib）。

**明确遗留（含原因）**：

| 编号 | 状态 | 说明 |
| --- | --- | --- |
| CODE-02 | 部分 | main 装配 bug 已修、停机已接线；1095 行巨型 main 的结构性重构未做（低覆盖率下大重构风险高，建议随功能迭代渐进） |
| CODE-06 | 部分 | 前端 115 行重复已抽共享模块；后端 30 个 processX 与超长函数（FormatReport 等）未拆分（同上） |
| CODE-01 | 部分 | auth_controller 越层已回收；controller.go:40 采集协程启动位置未动 |
| CODE-10 | 遗留 | 配置双轨加载未合并（涉及面广，需独立方案确认） |
| REPO-03 | 部分 | gin/compress/quic-go/Go 已升级，golang/mock 随 tidy 清除；nebula-go/websocket pseudo-version 为有意钉住上游修复（保留）；个人 fork go-openai 系 eino-ext 间接依赖（无法单方面替换，接受风险） |
| REPO-07 | 遗留 | .qoder 自动放行清单收敛属用户权限偏好，留待用户自行决定 |
| TEST-06 | 部分 | 真 e2e 仍依赖活服务（smoke_test.sh 已重写对齐现行鉴权）；进程内 mock 集成测试保留 |
| CODE-11 | 部分 | lint warning 57→32（余为 no-console 等低危项） |

敏感文件处置：`artifacts/kubeconfig`、大二进制、`tmp_0001.md`、`{{output_dir}}` 已移至仓库外备份 `~/mutong-cleanup-backup-2026-11-12/`（可恢复，未删除）。

---

## 目录

1. [评估概览](#一评估概览)
2. [问题分类总览](#二问题分类总览)
3. [功能缺陷（FUNC）](#三功能缺陷func)
4. [安全问题（SEC）](#四安全问题sec)
5. [文档问题（DOC）](#五文档问题doc)
6. [测试问题（TEST）](#六测试问题test)
7. [工程与发布问题（ENG）](#七工程与发布问题eng)
8. [代码质量与架构问题（CODE）](#八代码质量与架构问题code)
9. [仓库卫生与依赖问题（REPO）](#九仓库卫生与依赖问题repo)
10. [亮点](#十亮点)
11. [修复路线图](#十一修复路线图)

---

## 一、评估概览

Go 1.27 运维 / AIOps 平台：告警、诊断、巡检、K8s / NebulaGraph 拓扑、OAuth2 / OIDC / RBAC、终端执行、MCP / LLM（eino）。后端 325 个 Go 文件约 6 万行（66 个测试文件）；前端 Vue 3.4 ESM + Vite 5 多页应用（17 入口，无 SFC），位于 `view/`。

### 实测基线

| 实测项 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./...` / `gofmt -l` | 零告警 / 零输出 |
| `go test ./... -count=1` | 34 包全过、0 失败（sqlite 内存 + httptest，无外部依赖） |
| 覆盖率 | 有测试包 34/61（56%），平均 40.8%；**controllers 仅 7.3%、cmd 3.0%、mcp 0.6%**；pkg/ngql 85.3% |
| 前端 `bun run test` | 14 文件 69 用例全过；lint 0 error / 57 warning |
| `go mod verify` / `tidy -diff` | 全部验证通过、无漂移、无 replace / fork 替换 |

### 总评分：6.5 / 10

| 维度 | 评分 | 一句话结论 |
| --- | :---: | --- |
| 架构与代码质量 | 6.5 | 分层方向正确、无循环依赖，但 main 装配有两处可复现真 bug |
| 安全 | 7.0 | 上次 P0 修复扎实且验证彻底，残留 5 个高危 |
| 测试与工程化 | 6.5 | CI / lint / 镜像规范，发布链路未闭环、核心包覆盖低 |
| 前端 | 6.5 | 设计系统与契约测试高于平均，重构只完成一半 |
| 仓库治理与文档 | 6.0 | 凭据隔离干净，文档层严重失修 |

**总体判断**：安全意识和工程骨架明显高于同规模平均水平的个人 / 小团队项目，最近的安全加固（`913d6e7`）质量很高。主要短板不是"写不出好代码"，而是**收尾不闭环**：修了漏洞但冒烟脚本和测试没跟上、做了设计系统但一半页面没迁移、写了 CHANGELOG 但没有 tag 和发布流水线、README 与实际行为脱节。

---

## 二、问题分类总览

共 **69 项**发现：严重 3、高 13、中 34、低 19。

| 类别 | 数量 | 严重 | 高 | 中 | 低 | 核心结论 |
| --- | :---: | :---: | :---: | :---: | :---: | --- |
| [功能缺陷 FUNC](#三功能缺陷func) | 11 | 3 | 4 | 3 | 1 | 停机失效、nil panic、假数据仪表盘；部署链路多处"照做会失败" |
| [安全问题 SEC](#四安全问题sec) | 17 | 0 | 5 | 10 | 2 | 无 P0 残留；webhook 重放、PAT 绕过、PromQL 注入、XSS 需优先补 |
| [文档问题 DOC](#五文档问题doc) | 8 | 0 | 1 | 6 | 1 | 安全变量零文档、重复小节、数字漂移、计划文档与现实脱节 |
| [测试问题 TEST](#六测试问题test) | 6 | 0 | 2 | 2 | 2 | 陈旧测试与修复矛盾、冒烟脚本失效、核心包覆盖低、假测试 |
| [工程与发布 ENG](#七工程与发布问题eng) | 7 | 0 | 1 | 4 | 2 | 无 tag 无发布流水线、无版本化迁移、构建非幂等 |
| [代码质量 CODE](#八代码质量与架构问题code) | 11 | 0 | 0 | 6 | 5 | 越层、巨型 main、错误处理不一致、前端重构半途、重复代码 |
| [仓库卫生 REPO](#九仓库卫生与依赖问题repo) | 9 | 0 | 0 | 3 | 6 | 工作区明文凭据、大二进制散落、依赖风险项、工具残留 |

---

## 三、功能缺陷（FUNC）

### FUNC-01 变量遮蔽导致停机链路失效

- **严重度**：严重（已人工复核）
- **位置**：`cmd/main.go:802`、`:857`（外层声明在 `:622-623`）
- **问题**：`initializeGin` 外层声明 `diagEngine` / `chatManager`，但 if 块内用 `:=` 新建同名变量，函数返回的外层指针恒为 nil。
- **影响**：`cmd/main.go:206-211` 的 Stop 永不执行；StartStatsSync 协程泄漏。
- **建议**：块内改 `=` 赋值，或去掉外层声明、统一返回块内变量。

### FUNC-02 Prometheus 未配置时 panic

- **严重度**：严重
- **位置**：`cmd/main.go:880`、`:895` + `services/prometheus/query_service.go:755`、`:434`
- **问题**：nil 服务被 `AsMetricsQuerier` 包进非 nil 接口，调用方判空被绕过。
- **影响**：未配置 Prometheus 时 `QueryMetric` 解引用 `s.baseURL` 即崩溃。
- **建议**：适配器对 nil 接收者返回 nil 接口，或装配前判空。

### FUNC-03 仪表盘假数据掩盖故障

- **严重度**：严重（已人工复核，产品级信任缺陷）
- **位置**：`view/src/utils/stats.js:3-19`、`view/src/main.js:22-28`
- **问题**：接口失败返回 `MockStats`（1200 资源、24 节点、健康"良好"），UI 照写"数据更新于 XX"。
- **影响**：后端挂了，运维大屏显示一切正常。
- **建议**：删除 MockStats 兜底，改为显式错误态。

### FUNC-04 终端页依赖公网 CDN

- **严重度**：高
- **位置**：`view/src/terminal/terminal.js:1`、`terminal/index.html:7`
- **问题**：从 jsdelivr 拉 xterm 5.5.0，未进 package.json，构建产物保留该 URL。
- **影响**：内网 / 离线部署终端功能直接失效。
- **建议**：xterm 本地化进 package.json，随 Vite 打包。

### FUNC-05 根 docker-compose 无法启动图库

- **严重度**：高
- **位置**：根 `docker-compose.yml`（101 行）vs `deploy/docker-compose.yml`（157 行）
- **问题**：根 compose 只有 Nebula graphd，缺 metad / storaged；且未暴露 9669 / 5432 / 9092。
- **影响**：README 的"一键启动"图库起不来；宿主机 `just init-nebula` / `init_postgres.sh` 连不上。
- **建议**：补齐三服务与端口映射，或让 README 指向 deploy/ 版本。

### FUNC-06 `just run` 在 macOS 必失败

- **严重度**：高
- **位置**：`Justfile:6`（`output := "artifacts/mutong-linux-amd64"`）
- **问题**：README / CONTRIBUTING 均推荐 `just build-mac && just run`，但 run 执行的是 Linux ELF。
- **建议**：run 按平台选择产物，或 build-mac 输出到独立路径。

### FUNC-07 容器健康检查必失败

- **严重度**：高
- **位置**：`Dockerfile:30`
- **问题**：HEALTHCHECK 指向 `/healthz`，但该路由未注册。
- **建议**：注册 /healthz 或改 HEALTHCHECK 指向现有端点。

### FUNC-08 至 FUNC-11（中 / 低）

| 编号 | 严重度 | 问题 | 证据 | 建议 |
| --- | :---: | --- | --- | --- |
| FUNC-08 | 中 | pyroscope 默认地址非空 → 默认启用且 Start 失败 panic；gops 耦合在 pyroscopeServer 内 | `cmd/main.go:274-290` | 默认禁用，显式配置才启用；gops 独立开关 |
| FUNC-09 | 中 | `autoDiag.Start(context.Background())` 脱离应用 ctx，Stop 无人调用；terminal StartCleanup 启动后无对应 Stop | `cmd/main.go:979`、`:799`；`executor/auto_diagnosis.go:147` | 接入应用 ctx 与停机链路 |
| FUNC-10 | 中 | 前端资源清理缺失：terminal 无 `term.dispose()` / `ws.close()` / resize 解绑；diagnosis 无 onUnmounted 中止 AbortController | `view/src/terminal/terminal.js`、`diagnosis.js` | 补 onUnmounted 清理 |
| FUNC-11 | 低 | init() 打 banner 还 `time.Sleep(1s)`，import 副作用拖慢启动、污染日志 | `config/hello.go:17-20` | 删除或改为显式调用 |

---

## 四、安全问题（SEC）

> `913d6e7` 修复验证：原 P0 已真实修复——UUID 会话伪造回退已删、鉴权默认拒绝、Casbin 已挂载且无 enforcer 时拒绝、webhook 加签、Secret 脱敏、MCP 执行工具仅提案 + admin 门禁。**未发现残留 P0。** 以下为新发现。

### SEC-01 Webhook 无限重放

- **严重度**：高（已人工复核）
- **位置**：`controllers/webhook_verifier.go:173-177`（HMAC 只签 body）、`:161-164`（重放缓存 300s 后清除）
- **攻击场景**：截获一次合法请求，改 Timestamp 头即可永久重放注入告警。
- **建议**：时间戳纳入签名计算。

### SEC-02 PAT 生命周期缺陷 → RBAC 绕过

- **严重度**：高（已人工复核）
- **位置**：`controllers/auth_middleware.go:98-124`、`controllers/casbin_middleware.go:42-48`
- **攻击场景**：token 路径不查用户 status，禁用 / 删除用户的 PAT 仍有效；用户行缺失时 `user_uuid` 为空，Casbin 对空 uuid 直接放行，可绕过全部授权（含终端）。
- **建议**：校验用户 status；uuid 为空时拒绝。

### SEC-03 PromQL 注入

- **严重度**：高
- **位置**：`services/prometheus/query_service.go:444`、`:176`；`controllers/monitoring_controller.go:75`
- **攻击场景**：namespace / name 未转义拼入标签匹配，普通 read 用户可读取全量监控数据；timeseries 端点还直接接受任意 expr。
- **建议**：转义引号 / 反斜杠或白名单校验；expr 端点收紧权限。

### SEC-04 存储型 XSS（复盘因果图 tooltip）

- **严重度**：高（已人工复核）
- **位置**：`view/src/retrospective/retrospective.js:798-801`（同类：`:633`、`retrospective-history.js:728`）
- **攻击场景**：G6 tooltip 用 `innerHTML` 直拼 LLM 生成的 `model.label`，未转义。同仓 `diagnosis.js:14` 已用 DOMPurify，标准存在但没覆盖到。
- **建议**：改 `textContent` 或统一走 DOMPurify。

### SEC-05 公网 webhook 无 body 上限

- **严重度**：高
- **位置**：`controllers/webhook_verifier.go:107`
- **攻击场景**：未认证即可用超大 body 耗尽内存（`io.ReadAll` 无 `MaxBytesReader`）。
- **建议**：加 `http.MaxBytesReader`。

### SEC-06 至 SEC-17（中 / 低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| SEC-06 | 中 | 终端用 root kubeclient 可 exec 任意 ns（含 kube-system），无操作审计；Exec URL 回显泄露 API 地址 | `controllers/terminal_controller.go:236-240`、`terminal/session.go:282` |
| SEC-07 | 中 | 操作审计 opAuditStore 恒 nil 未接线；登录 / 令牌 / 终端无审计 | `services/executor/executor.go:127` |
| SEC-08 | 中 | 设备流：token 未持久化无法校验、换 token 无客户端认证、任意登录用户可批准、Origin 头注入 verification_uri | `services/auth/device.go:94-106`、`:142-155`、`:188` |
| SEC-09 | 中 | autoMode 下 LLM 自报置信度可驱动重启 / 滚动重启自动执行，告警内容提示注入为残留风险面 | `executor/bridge.go:29-48`、`executor.go:157-171` |
| SEC-10 | 中 | LoginLimiter 仅 Redis 可用时生效，错误吞掉 fail-open | `services/auth/login_limiter.go:21`、`auth_controller.go:79` |
| SEC-11 | 中 | getSession 属主校验在 UserID=0 时跳过 | `controllers/diagnosis_controller.go:1288` |
| SEC-12 | 中 | OIDC state cookie 设置 / 清除路径不一致；state 比较非常量时间 | `controllers/oidc_controller.go:47`、`:113`、`:55` |
| SEC-13 | 中 | 会话角色内嵌 24h 无吊销；PAT scopes 存而不用 | `services/auth/session.go:39-48`、`auth_middleware.go:121` |
| SEC-14 | 中 | 前端凭据双轨：token 存 sessionStorage（XSS 可读）+ HttpOnly cookie；api.js 无 CSRF token，靠 SameSite=Lax 兜底 | `view/src/login.js:62`、`controllers/auth_controller.go:42` |
| SEC-15 | 中 | update_nebula.sh 把 nebula 密码明文写入日志，默认 root/nebula | `scripts/update_nebula.sh:116` |
| SEC-16 | 低 | Label 顶点 NGQL 裸拼（K8s 标签字符集缓解）；mergebizapp 仍用已弃用的 strconv.Quote | `services/k8sresource_relationships.go:73`、`cmd/mergebizapp/main.go:75` |
| SEC-17 | 低 | 大量 `err.Error()` 直返客户端，泄露内部细节 | `terminal_controller.go:88`、`monitoring_controller.go:89` 等 |

---

## 五、文档问题（DOC）

### DOC-01 安全环境变量零文档，照 README 部署会 503

- **严重度**：高
- **位置**：两版 README vs CHANGELOG 新增项
- **问题**：`MUTONG_SESSION_SECRET` / `MUTONG_ADMIN_KEY` / `MUTONG_WEBHOOK_SECRET` / `MUTONG_TRUSTED_PROXIES` / `enableSwagger` / `rbac_mode` 等关键变量 README 0 命中。
- **建议**：补"安全配置"章节，给出最小可用部署示例。

### DOC-02 至 DOC-08（中 / 低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| DOC-02 | 中 | README 有 7 个重复"安全"小节，含病句标题"🔒 安全与安全 / Security" | `README.md:953-1004` |
| DOC-03 | 中 | 中英 README 严重不同步：英文缺"配置说明 / 项目结构 / 开发指南"整章 | 1029 行 vs 345 行 |
| DOC-04 | 中 | 版本自相矛盾：go.mod 为 1.27.0，README 前置条件仍写 Go 1.25+（中英皆是） | `README.md`、`README.en.md` |
| DOC-05 | 中 | 数字漂移：自愈操作实际 13 种 vs ROADMAP 11 / evolution 12；MCP 实际 29 vs 多处文档 20；CLI 实际 20 模块 vs ROADMAP 19；controllers 34 文件 vs README 33 | `services/executor/executor.go` case 统计等 |
| DOC-06 | 中 | ROADMAP 过时：webhook 签名已实现仍列待办；声称 CI 有 govulncheck 实际没有；称 v0.1.0"当前"但 git tag 为 0 | `ROADMAP.md` vs 代码 / CI |
| DOC-07 | 中 | 计划文档半成品：remediation-plan 称"M0-M3 已实施"但 28 个 checkbox 全未勾；business-topology-plan 43 项全未勾且写 Go 1.25；external-search-plan 仍标"提案"而 Tavily 已上线 | `docs/remediation-plan.md` 等 |
| DOC-08 | 低 | 断链：`.agents/AGENTS.md` 要求维护的 `docs/PROJECT_ARCHITECTURE.md` 不存在；`.envrc` 指向不存在的 terraform/；SECURITY.md 仅 21 行、清单全未勾、无 .envrc 所称内容 | 各文件 |

---

## 六、测试问题（TEST）

### TEST-01 误导性陈旧测试与安全修复矛盾

- **严重度**：高
- **位置**：`controllers/auth_middleware_test.go` 中 `TestBearerTokenMiddlewareLegacyLocalLogin`
- **问题**：断言明文 UUID cookie 返回 200，与 CHANGELOG"删除 UUID 回退"及 middleware 注释（`auth_middleware.go:170-172`）矛盾——它只挂 BearerTokenMiddleware 未挂 RequireAuthMiddleware，也不断言身份。
- **建议**：删除或改为断言"仅挂该中间件不构成认证"。

### TEST-02 冒烟脚本已失效

- **严重度**：高
- **位置**：`scripts/smoke_test.sh`
- **问题**：仍用旧路径 `/k8s/resources/*`（现 301，curl 无 -L）、无登录态、webhook 无签名——安全加固后必失败，未同步。
- **建议**：按现行路由与鉴权重写，纳入 CI 或发布前检查。

### TEST-03 至 TEST-06（中 / 低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| TEST-03 | 中 | 核心包覆盖低：controllers 7.3%、cmd 3.0%、mcp 0.6%、executor 本体 16.4%（测试集中在 bridge） | `go test -cover` 实测 |
| TEST-04 | 中 | 前端 4 个假测试：`topology/diagnosis/inspection/retrospective.smoke.test.ts` 各 10 行，mount 自造 div，从不 import 真实页面 | `view/src/**/*.smoke.test.ts` |
| TEST-05 | 低 | playwright 是死依赖：仅在根 package.json，全仓无用例 / 配置 / 调用 | 根 `package.json:4` |
| TEST-06 | 低 | 集成测试为进程内 mock 非真实依赖；真 e2e 只有已失效的 smoke_test.sh；2 处条件 t.Skip | alert / diagnosis 的 integration_test.go；`services/auth/mutong_session_test.go:102`、`:110` |

---

## 七、工程与发布问题（ENG）

### ENG-01 发布链路未闭环

- **严重度**：高
- **位置**：git tag / `.github/workflows/` / `CHANGELOG.md`
- **问题**：git tag 为 0、CHANGELOG 只有 `[Unreleased]`、无 release workflow、无镜像推送、无 goreleaser。
- **建议**：打第一个 tag，补 release workflow（构建 + 镜像推送 + 产物校验）。

### ENG-02 至 ENG-07（中 / 低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| ENG-02 | 中 | 无 govulncheck / trivy / CodeQL（gosec 随 lint）；dependabot 只覆盖 gomod + actions，漏前端 | `.github/workflows/ci.yml`、dependabot 配置 |
| ENG-03 | 中 | 工具链升级挂着未提交：CI 仍按 Go 1.26 跑、与本地 1.27 不一致。diff 本身合理低风险（1.26→1.27、sonic 1.15.4、tidy 产物），应整体提交；`1.27.x` 浮动版本建议与 go.mod 对齐锁定 | `git diff` ci.yml / Dockerfile / deploy/Dockerfile / go.mod / go.sum / README |
| ENG-04 | 中 | 数据库迁移全靠 gorm AutoMigrate，无版本化、无回滚 | `config/config.go:570`、`:591`；alert / audit / executor 的 pg_store.go |
| ENG-05 | 中 | Justfile `build` 内嵌 `go mod tidy` + `gofmt -w .`，构建改写工作区、非幂等 | `Justfile` |
| ENG-06 | 低 | update_nebula.sh 无 `set -e`（密码明文日志问题另见 SEC-15） | `scripts/update_nebula.sh` |
| ENG-07 | 低 | deploy 配置冗余：values.yaml 是 733 行上游 otel-collector 默认值全量拷贝，与实际用的 42 行 helm-otelcol-values.yaml 并存，另有 .bak；根与 deploy/ 两份 compose | `deploy/` |

---

## 八、代码质量与架构问题（CODE）

### CODE-01 至 CODE-06（中）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| CODE-01 | 中 | 越层：controller 直接 gorm 查 / 建 User，绕过 service 层；controller 启动采集协程 | `controllers/auth_controller.go:91`、`:271`、`:279`；`controllers/controller.go:40` |
| CODE-02 | 中 | 巨型 main 装配：1095 行，initializeServices 返回 8 值、initializeGin 收 12 参，参数列表即耦合清单 | `cmd/main.go:296`、`:619` |
| CODE-03 | 中 | 错误处理不一致：434 处 `fmt.Errorf` 仅 235 处用 `%w`；`_ = json.Unmarshal` 吞错；生产路径用 `fmt.Printf` | `services/alert/pg_storage.go:274-280`、`diagnosis/llm_http.go:738`、`executor/pg_store.go:115`、`:143` |
| CODE-04 | 中 | 前端主题迁移半途：硬编码色值 retrospective.js 224 处、retrospective-history.js 139 处、diagnosis.css 115 处、topology.css 73 处，违反自定 theme 规则；`'#18900ff'` 非法 7 位色；G6 tooltip 写死 rgba 不随暗色 | `view/src/styles/theme.js:2`、`retrospective.js:616` |
| CODE-05 | 中 | 前端错误被吞：api.js 大量 `.catch(() => null/[])`，页面分不清"空"与"故障"；toast 仅 1/17 页使用 | `view/src/utils/api.js` |
| CODE-06 | 中 | 重复代码：前端两页拓扑图 115 行完全相同（约 24%），DataTable 仅 1/6 页采用；后端 30 个雷同 processXRelationship（1480 行）；超长函数 FormatReport 298 行 / ListTools 202 行 / chatContext 227 行 | `retrospective.js:522-810` vs `retrospective-history.js:617-745`；`k8sresource_relationships.go`；`retrospective_service.go:354`、`mcp/server.go:142`、`diagnosis_controller.go:560` |

### CODE-07 至 CODE-11（低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| CODE-07 | 低 | 抽象漏底 / 未落地：Logger 接口泄漏 zap.Field、GraphDB 绑死 Nebula 类型（注释称可换 Neo4j）；pkg/safe 仅 1 处引用而 main 手写 7 个 recover | `interfaces/dependencies.go:17-33`、`pkg/safe/safe.go` |
| CODE-08 | 低 | 死代码 / 无人使用的导出：InitCollect 无调用者；`var _ = url.Parse` 保活 import；前端 Toolbar、isNavActive 导出后无人用（NavBar 自己重写一份） | `controllers/controller.go:35`、`services/auth/oauth2_storage.go:313`、`primitives.js:124`、`nav.config.js:52` |
| CODE-09 | 低 | 硬编码相对路径依赖 CWD（casbin 模型 / prompts）；包级全局 enforcer / mode | `services/auth/casbin.go:20-24`、`:32`；`cmd/main.go:807` |
| CODE-10 | 低 | 配置双轨加载：全局 viper 单文件 vs yaml.Unmarshal 目录加载并存；config.go 50K + config_base.go 26K 有 god object 倾向 | `config/config.go:141` vs `:156-180` |
| CODE-11 | 低 | 前端 lint 57 warning（no-console、unused）；旧变量别名仍被 3 个文件引用 | `inspection-history.css`、`retrospective-history.css`、`login.html` |

---

## 九、仓库卫生与依赖问题（REPO）

> 干净的部分：476MB artifacts、`{{output_dir}}`、tmp_0001.md、各 AI 工具目录（.omo / .sisyphus / .workbuddy / .qoder / .zvec-grep / .obsidian）**均未入库**，.gitignore 逐条覆盖；artifacts 从未进 git 历史；.git 仅 48MB；追踪文件全量扫描 0 硬编码凭据。

### REPO-01 至 REPO-03（中）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| REPO-01 | 中 | 工作区敏感文件：`artifacts/kubeconfig` 含真实 client-key-data / CA / server 地址明文；`.workbuddy/memory/` 存内部审计细节（含未修漏洞）。均未入库，但明文躺在磁盘 | `artifacts/kubeconfig`（8K） |
| REPO-02 | 中 | 大二进制散落：artifacts/ 476MB（4 个二进制 157+153+151+13MB）；根目录 mutong-linux-amd64 152MB、mutongctl 12MB、mergebizapp 9.7MB | `du` 实测 |
| REPO-03 | 中 | 依赖风险项：nebula-go 与 gorilla/websocket 钉 pseudo-version；gin 1.10 落后 1.11.x；golang/mock 已归档（应迁 go.uber.org/mock）；eino-ext 间接引入个人 fork `meguminnnnnnnnn/go-openai v0.1.2`；fosite 拖入 pop / mssql / mysql 等无关 indirect；三套 SQLite 驱动 + 两套 yaml 库冗余 | `go.mod` |

### REPO-04 至 REPO-09（低）

| 编号 | 严重度 | 问题 | 证据 |
| --- | :---: | --- | --- |
| REPO-04 | 低 | tmp_0001.md 是个人职业规划文档，与项目无关，含私人上下文 | 根 `tmp_0001.md` |
| REPO-05 | 低 | 已入库的 IDE / AI 工具配置：`.idea/`、`.opencode/AGENTS.md`、`.opencode/skills/`、`.agents/AGENTS.md` | `git ls-files` |
| REPO-06 | 低 | `{{output_dir}}/` 工具模板 bug 残留：内文件与 `.opencode/skills/mutongctl/SKILL.md` 完全相同，某工具把模板变量当字面路径写盘 | 目录 diff |
| REPO-07 | 低 | `.qoder/settings.local.json` 56 条自动放行命令中 15 条含凭据链（跨仓库 .envrc + admin 登录），建议收敛 | 该文件（未入库） |
| REPO-08 | 低 | .DS_Store 只靠用户全局 gitignore 忽略，仓库 .gitignore 应补一条 | `.gitignore` |
| REPO-09 | 低 | 前端遗留死文件仍在 git：jslibs（vue 3.4.21 + 1.5MB G6 UMD）、shared/、根 8 个遗留 html、view/npm（0 字节）、scripts/main.js（仅 console.log）；remediation-plan 已计划删除未执行 | `docs/remediation-plan.md:181` |

---

## 十、亮点

### 架构

- 无循环依赖：services 不 import controllers、models 不 import services；跨域经 interfaces（73 个文件引用），方向清晰。
- 后台循环规范：统一 stopCh + ticker + select；AutoDiagnosis.Stop 用 CAS + WaitGroup；无 time.Tick 泄漏。
- NGQL sanitizer 集中化（`pkg/ngql/sanitizer.go`），旧入口留兼容别名；测试就近放置。
- 真实 TODO / FIXME / HACK 注释为 0；models 为纯 gorm 结构体，无业务逻辑混入。

### 安全

- 默认拒绝鉴权 + CORS 默认禁跨域、拒通配符（`cmd/main.go:453-471`、`:539-544`）。
- argon2id + 常量时间比较；PAT 仅存 SHA-256；fosite 强制 PKCE 禁 plain。
- HMAC 会话密钥四级回退 + 0600 持久化；Cookie HttpOnly / SameSite=Lax。
- webhook 常量时间比较 + IP 白名单；执行器动作白名单 + 冷却 + 验证回滚。
- NGQL sanitizer 覆盖主链路（85.3% 覆盖）；MCP 参数严格字符集校验、执行工具仅提案 + admin 门禁。
- 前端 DOMPurify + safeRedirect 白名单；K8s Secret 脱敏；compose 密码 `${VAR:?}` 强制注入无弱默认。
- 示例与实配分离到位（9 个 .example 入库，真实配置全忽略）；git 历史抽查未见秘密泄漏。

### 工程

- CI 五 job（lint / test -race + cover / build / 前端 --frozen-lockfile / docker），最小权限 + concurrency 取消重复跑。
- 双 Dockerfile 多阶段、非 root 用户、alpine:3.21 固定、CGO_ENABLED=0；entrypoint.sh 幂等且处理只读卷回退。
- `.golangci.yml` v2 七 linter，gosec 豁免逐条注明理由；skywalking 编译期注入规范；/metrics 默认鉴权。
- init_postgres.sh / schema.ngql 幂等（IF NOT EXISTS × 57）。
- 高质量真断言测试：webhook_verifier（默认拒绝 / 签名 / 过期 / 重放）、ngql sanitizer（表驱动 + 注入样例 + 边界）、session 密钥（0600 / 复用 / env 优先级）。

### 前端

- 三层设计 token 扎实：原始层（AntD 10 阶）→ 语义层（light/dark）→ 组件层全 `:where()` 零特异度；reset 降进 `@layer` 修的是真实层叠 bug。
- 测试钉契约：theme.test.ts 断言暗色每个变量名在浅色存在，防静默失效；nav / layout 测试锁住 CSS 与交互契约。
- 首帧防闪：head-prepend 引导 + theming 类关过渡 + 双 rAF 释放；MutationObserver 收敛三种主题来源。
- NavBar 状态驱动下拉、180ms 容差、Esc / 外点关闭、全解绑；轮询均 clearInterval；NAV_HIDDEN 带 reason + 代码位置。
- 构建可复现：bun.lock 入库、内容 hash、G6 独立分块；dist 未提交进 git。

### 治理

- .gitignore 覆盖到位，476MB artifacts 与全部工具目录从未入库，.git 仅 48MB。
- `go mod verify` 全过、无 replace / fork 替换；LICENSE（MIT）存在；docs/images 全部被引用；README 的 29 个 MCP 工具表与代码逐条对应。
- 依赖整体较新：x/crypto 0.57、x/net 0.60、k8s 0.34.1、gorm 1.31.1、otel 1.45，go-jose 已含修复版本。

---

## 十一、修复路线图

### 本周（正确性 + 可利用漏洞）

- [ ] FUNC-01 / FUNC-02：修 main.go 两处 `:=` 遮蔽与 Prometheus nil 适配器（各一行级改动，收益大）。
- [ ] FUNC-03：删除 MockStats 兜底，改显式错误态。
- [ ] SEC-01 / SEC-05：webhook 签名纳入时间戳 + MaxBytesReader。
- [ ] SEC-02：PAT 校验用户 status；Casbin 空 uuid 拒绝。
- [ ] SEC-03：PromQL 标签转义；SEC-04：tooltip 改 textContent / DOMPurify。
- [ ] ENG-03：提交挂着的 Go 1.27 工具链升级；FUNC-07：注册 /healthz 或改 HEALTHCHECK。

### 两周内（闭环）

- [ ] TEST-01 / TEST-02：同步陈旧测试与冒烟脚本。
- [ ] DOC-01 / DOC-02 / DOC-04：补 README 安全变量章节、删重复小节、对齐版本。
- [ ] FUNC-04 / FUNC-05 / FUNC-06：xterm 本地化、修根 compose 与 `just run`。
- [ ] ENG-01 / ENG-02：打第一个 tag + release workflow + govulncheck。
- [ ] DOC-05 / DOC-06 / DOC-07：文档数字与 checkbox 状态对齐现实。

### 一个月内（还债）

- [ ] TEST-03 / TEST-04：controllers / cmd 覆盖率补到 40%+；假 smoke 测试删除或改真挂载。
- [ ] ENG-04：AutoMigrate 换版本化迁移。
- [ ] SEC-06 / SEC-07：终端按 namespace 限权 + 操作审计接线。
- [ ] CODE-04 / CODE-06：retrospective 两页迁移到 token / 组件层并去重。
- [ ] CODE-01 / CODE-02 / CODE-03：越层回收、装配收敛、错误处理统一（可随功能迭代渐进）。
- [ ] REPO-01 / REPO-02 / REPO-04 / REPO-05 / REPO-09：清理 kubeconfig、大二进制、私人文档、入库工具配置、前端遗留死文件。

---

## 总结

骨架和安全底子好于多数同规模项目（尤其最近的 P0 修复质量高）。当前最危险的不是黑客，而是"自己骗自己"——停机失效的装配 bug、假数据仪表盘、必失败的冒烟脚本和照做会挂的 README。

**先把"本周"清单修掉，项目即可从 6.5 稳步上到 7.5+。**