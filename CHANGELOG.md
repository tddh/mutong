# Changelog

## [Unreleased]

## [0.1.0] - 2026-11-12

首个标记版本。包含平台核心功能与两轮安全加固。

### Security

- Webhook 签名升级为 HMAC-SHA256(secret, 时间戳 + "\n" + body)：此前只签 body，截获一次合法请求改时间戳即可无限重放；请求体上限 1MB（超限 413）
- PAT/SAT 校验用户/服务账户状态：禁用或删除用户的令牌立即失效；Casbin 对空 `user_uuid` 一律拒绝，堵住 RBAC 绕过
- 会话吊销：校验用户状态/角色/密码指纹（30s 缓存），禁用/改密/改角色后会话失效；只读 scope 的 PAT 拒绝写请求
- 设备流（RFC 8628）加固：授权绑定批准用户与 client_id、单次原子消费、token 端点签发真实持久化 PAT（1 天有效期）；`verification_uri` 取自 `MUTONG_ISSUER_URL` 而非 Origin 头
- PromQL 标签值转义 + K8s 命名字符集校验；任意 expr 查询收紧为 admin
- Web 终端 namespace 白名单（`terminal.allowedNamespaces`，默认拒绝系统 ns）+ 会话审计；不再向客户端回显 API 地址
- 执行器操作审计接线（每个动作落库留痕）；自动执行仅限显式开启 autoMode 且规则侧验证的低风险动作，LLM 自报置信度不再作为唯一依据
- 登录限流在 Redis 已配置但故障时 fail-closed（503），不再静默放行
- OIDC state cookie 设置/清除路径一致，state 比较改常量时间
- 前端：删除仪表盘 MockStats 假数据兜底（故障显示显式错误态）；复盘因果图 tooltip XSS 修复；凭据单轨 HttpOnly cookie，sessionStorage 不再存 token；xterm 本地打包，移除公网 CDN 依赖
- `update_nebula.sh` 不再把密码写入日志，凭据改环境变量必填
- 依赖漏洞修复（govulncheck 清零）：klauspost/compress 1.18.5→1.20.2（GO-2026-6865）、quic-go 0.59.0→0.59.1（GO-2026-5676）、Go 1.27.3→1.27.4（GO-2026-6859，net/http MaxBytesReader 竞态）
- Sanitizer 配置键修复：YAML snake_case 与结构体 camelCase 不匹配导致禁用词/注入模式实际为空；新增零值字段默认回落（ApplyDefaults）
- GitHub Issues 搜索结果与 Tavily AI 摘要补上提示注入过滤（此前仅 Tavily 普通结果过滤）
- 外部搜索审计钩子接线（此前传 nil 丢弃审计事件）；终端会话审计存储接线（此前仅写日志）
- 鉴权改为默认拒绝：`/api/v1/k8s/resources/**`、`/api/v1/users`、`/api/v1/roles` 全部需要登录；旧路径 301 到新路径
- 删除 `mutong_session` 的 UUID 回退分支：cookie 只接受 HMAC 验签通过的会话，伪造 UUID 不再能登录
- 会话 Cookie 显式 `SameSite=Lax` + `HttpOnly`，`Secure` 支持 `X-Forwarded-Proto`/`MUTONG_COOKIE_SECURE`
- Casbin RBAC 真正挂载；启动时同步所有活跃用户角色；新增 `auth.rbac_mode`（`enforce`/`shadow`/`off`）
- 执行器危险操作密钥比较改为常量时间
- 告警 webhook 挂载签名校验 + 时间戳窗口 + 重放拒绝；未配置 secret 时默认拒绝（可用 `MUTONG_WEBHOOK_ALLOW_UNSIGNED=true` 显式放行）
- 外部告警源需要 `X-Alert-Token` 或受信 IP；两者都未配置时拒绝
- Secret 的 `data`/`stringData` 在资源详情与图谱接口返回前脱敏
- `/device` 页面与诊断 Markdown 输出做 HTML 转义/DOMPurify 清洗
- LLM 建议不再标记为可自动修复；MCP 执行工具只生成待审批提案，不再自动执行
- Swagger 默认关闭（`server.enableSwagger`）；`/metrics` 默认需要鉴权（`MUTONG_METRICS_PUBLIC=true` 放行）
- CORS 默认拒绝跨域；`MUTONG_ALLOWED_ORIGINS` 中的 `*` 被忽略
- 默认只信任本机回环代理；跨机反代需通过 `MUTONG_TRUSTED_PROXIES` 显式配置，避免 `X-Forwarded-For` 伪造

### Changed (breaking)

- Webhook 签名算法变更：现有集成方需按「时间戳 + "\n" + body」重新实现签名（头名称不变）
- 设备流 token 端点不再返回伪造的 `refresh_token`，改为签发 1 天有效期的持久化 PAT
- K8s 资源 API 从 `/k8s/resources/**` 迁移到 `/api/v1/k8s/resources/**`（旧路径 301）
- 用户/角色 API 从 `/users`、`/roles` 迁移到 `/api/v1/users`、`/api/v1/roles`（旧路径 301）
- 本地登录要求配置 `MUTONG_SESSION_SECRET` 或 `auth.global_secret`，否则返回 503；升级后需要重新登录
- 告警 webhook 需要配置 `MUTONG_WEBHOOK_SECRET` 并发送签名/时间戳头
- `MUTONG_INIT_ADMIN_PASSWORD` 只在管理员不存在时创建账号，不再每次启动重置密码

### Changed (defaults)

- pyroscope 默认禁用（`--scope-server-addr` 默认空），显式配置才启用且失败降级不 panic；gops 独立开关 `--gops`
- `MUTONG_TRUSTED_PROXIES` 未配置时默认信任本机回环 `127.0.0.1/32,::1/128`
- `MUTONG_SESSION_SECRET` 未配置且 `auth.global_secret` 为空时，自动生成 32 字节密钥并持久化到 `data/session_secret`（0600）；可用 `MUTONG_SESSION_SECRET_FILE` 指定路径。多副本部署必须通过环境变量共享同一密钥
- `MUTONG_ADMIN_KEY` 未配置时，执行器危险操作回退为要求 `admin` 角色（不再返回 503）

### Changed

- Go 工具链升级 1.27.4（CI/Dockerfile/go.mod 同步）；gin 升级 1.12
- `just build` 不再内嵌 tidy/gofmt 改写工作区；`just run` 按宿主平台选择产物（macOS 不再执行 Linux 二进制）
- 前端主题迁移收尾：复盘两页硬编码色值清零，拓扑图重复代码抽共享模块
- `chat/ask` 从无状态改为自动创建/续接用户会话（登录后首次消息即创建，侧栏同步显示）
- 移除前端快捷诊断按钮和引导面板，改为会话侧栏（新建/切换/删除）
- `chat_sessions` 表新增 `user_id:uint`（联合索引 `idx_user_status`）+ `title:varchar(255)` 字段
- 会话 PG 保留期从 30 分钟改为 30 天
- cmdb_data_silo 巡检规则排除 CRD/FlowSchema/PriorityLevelConfiguration/Lease 资源类型
- 巡检报告前端改为表格展示（级别/规则/资源/建议 4 列）

### Fixed

- `initializeGin` 变量遮蔽导致 `diagEngine`/`chatManager` 恒为 nil：停机链路失效、goroutine 泄漏
- 默认配置启动崩溃：`MUTONG_ALLOWED_ORIGINS` 未设置时 `cors.New` 对空 AllowOrigins panic；改为不挂 CORS 中间件（跨域默认拒绝语义不变）
- `diagnosis.Engine.Stop()` close(nil channel) panic：`NewEngine` 未初始化 `stopCh`（停机链路修复后暴露）
- 内存审计存储 `matchesAuditLog` 忽略 fingerprint 过滤（与 PG 存储行为对齐）
- 前端目录解析修复：旧代码只验目录存在，把包着 dist/ 的外壳目录当 web root 致整站 `/view/*` 404；改为以二进制同级 `view/dist` 定位（不依赖 artifacts）、候选必须含 `index.html`、支持 `MUTONG_VIEW_DIR` 覆盖，启动日志打印 CWD 与搜索路径
- 旧路径 308 重定向丢失 query string：`/k8s/...?resourceId=x` 跳转到 `/api/v1/...` 时参数被丢弃，致 `graph/search` 必填参数缺失返回 400；重定向保留 RawQuery（`/k8s/`、`/users`、`/roles`）
- 告警统计口径不一致：收到总数按 webhook payload 计数、抑制总数按单个告警计数，批量 payload 致抑制率虚高（如 14802%）；`RecordReceived` 改按告警数计，与抑制同口径
- Prometheus 未配置时 nil 适配器绕过判空导致 panic
- `autoDiag` 与终端清理协程纳入应用 ctx 与优雅停机（新增 `StopCleanup`）
- 根 docker-compose 缺 Nebula metad/storaged 导致一键启动失败；deploy compose 单 storaged 与 `replica_factor=2` 冲突且缺 `ADD HOSTS`
- `init_postgres.sh` 硬编码 `-U postgres` 与 compose 用户不一致
- `/healthz` 未注册导致容器 HEALTHCHECK 必失败
- 删除 `config` 包 init() 的 banner + `time.Sleep(1s)` 启动副作用
- 重写 `smoke_test.sh` 对齐现行路由/鉴权/签名；删除误导性陈旧测试（明文 UUID cookie 断言 200）
- 前端 terminal/diagnosis 资源清理补全（dispose/ws.close/AbortController）
- `config.diagnosis.yaml` 与 `config.inspection.yaml` 的 inspection 重复键（移除前者，后者为唯一来源）
- 终端 WebSocket 会话关闭时回收 K8s exec 流与 goroutine；所有写操作串行化
- 告警聚合器 `Stop()` 与刷新协程的 ticker 竞态（可导致 nil panic）；done channel 锁外读改局部变量 + `sync.Once`
- 告警投递与执行审计失败不再被静默丢弃
- 业务拓扑接口错误时返回真实 500，而不是 `200 + {"error": ...}`
- 配置加载失败返回 error，不再从库代码 `os.Exit(1)`/`panic`
- `/api/v1/k8s/resources/:name` 读取 8 列导致的索引越界
- 前端 G6 拆分为独立 chunk，首屏包体不再包含 1.4MB 图库
- ClusterRoleBinding→ClusterRole 关联查询 bug（obj.Name → obj.RoleRef.Name），大量 ClusterRole 不再误报为孤岛
- 巡检报告 YAML 规则 Resource 字段补充 namespace 信息
- 外部搜索配置：`enabled` 改为 `true`，YAML key 统一为 camelCase，支持 MUTONG_TAVILY_KEY / MUTONG_GITHUB_TOKEN 环境变量覆盖
- 诊断 System Prompt 增加外部搜索工具引导
- 配置统一为 `configs/` 目录模式，配置文件提供 `.example` 模板
- 硬编码导出：新增 ServerConfig，扩展 KafkaConfig/ExecutorConf，17 处硬编码替换为可配置项
- configs/ 与 config 结构体完整对齐（18 键零缺失）
- MCP 工具调用全链路 Info 日志（InvokableRun + ExecuteTool）
- GitHub Issues 搜索返回 422：Fine-grained PAT 缺少 `is:issue` 限定符，已自动追加
- GitHub 搜索客户端：添加 User-Agent + 错误响应 body 读取；搜索客户端增加结构化日志
- 测试 mock 并发 write（mockNotifier / mockStorage 加 sync.Mutex）
- saveOAuthToken Token 刷新不持久化（map 副本未写回）
- OAuth Token 刷新 HTTP 请求无超时（http.PostForm → 显式构造 + 30s 超时）
- 无 kubeconfig 时优雅降级不再 crash；kubeconfig 敏感信息脱敏
- config.infra.yaml deleteResource→delete_pod key 不匹配 bug
- NavBar sticky 全局粘性定位
- Pod 终止态过滤（list_resources_from_cache + list_k8s_resources）
- api.js 补充 clusters 对象（修复集群管理页面报错）；诊断 api.js 清理遗留端点
- config.inspection.yaml 字段修正（schedule→cronSpec）
- 优雅停机验证：修复 6 个 goroutine 泄漏 + 重构 initializeGin()
- 巡检持久化修复：DB 接入/列名匹配/ID 防碰撞/GetLatestReport DB fallback

### Added

- `GET /healthz` 无鉴权健康检查
- CI govulncheck job；`release.yml` 发布流水线（tag v* → 多架构二进制 + ghcr 镜像 + checksums）；dependabot 覆盖前端
- `docs/PROJECT_ARCHITECTURE.md` 架构文档；README 中英同步并补安全配置章节；SECURITY.md 扩写
- 终端 namespace 白名单配置 `terminal.allowedNamespaces`
- 设备流端到端测试、鉴权回归测试与 controllers/cmd 覆盖率补充
- AI 诊断聊天会话持久化：`chat_sessions` 表新增 `user_id`/`title` 字段，`chat/ask` 自动创建/续接会话
- 会话历史 API：`GET /chat/sessions`（当前用户列表）、`GET /chat/sessions/:id`（详情）、`DELETE /chat/sessions/:id`（软删除）
- 侧栏会话列表（前端）：新建对话 / 切换会话 / 删除，刷新页面不丢失
- LLM 智能标题摘要（Eino chatModel，15 字内中文，异步非阻塞生成）
- PG 会话定时清理（每小时，720h 保留期），Redis 密码认证支持
- Server `sseWriteTimeoutSec` 配置（SSE 流式写超时，默认 300s）
- 外部知识库搜索（Tavily + GitHub Issues）：MCP 工具 search_knowledge_base / search_github_issues，支持 AI 诊断中 LLM 自动调用及 mutongctl 手动触发
- mutongctl search 命令（github / tavily 子命令）
- HTTP 连接池统一工厂 `services/httpclient`（共享 Transport，MaxIdleConns: 100）
- WebSocket 终端保活机制（Ping/Pong + ReadDeadline + WriteDeadline + 会话过期清理）
- 前端 ESLint + Prettier 代码规范配置
- 多集群 kubeconfig 声明式配置
- CI/CD 自动化（GitHub Actions，含 lint/test/build）
- golangci-lint 静态分析配置
- 英文 README
- 自愈执行器操作类型扩展至 13 种（updateConfigMap / updateSecret / updateResourceLimits / updateDeploymentImage / updateAnnotations / updateLabels 等）
- 巡检规则 CRUD API（GORM 模型 + RuleStore + DBBackedRule + 5 个端点）
- ES 日志索引映射：serviceToIndex 路由 + SearchLogsByBusiness + init_es_template 脚本
- 巡检历史前端页面（列表/详情/对比/趋势分析）
- 指标覆盖率扩展：StatefulSet/DaemonSet/Service 全链路指标支持
- Swagger API 文档生成（swaggo 集成，`just swagger`）
- LLM Token 追踪：TokenTrackingChatModel 包装器，记录每次 LLM 调用 token + 日志 + Prometheus
- 配置启动校验（必填字段 validate）
- 拓扑页侧边栏资源类型筛选控件
- 健康检查增加 NebulaGraph 连通性（SHOW SPACES）
- API 请求日志中间件（gin.Logger）
- config.retrospective.yaml 复盘自动触发配置
- CLI mutongctl 工具（20 个命令模块，4 种输出格式）
- NavBar 补全 4 个缺失页面链接（监控/终端/追踪/集群）
- 诊断模块单元测试（Phase 1，test_helpers.go 共享 Mock 基础设施）
- 资源 Profiles 拆分为 configs/profiles/*.yml 独立文件（pod/node/deployment/service/statefulset/daemonset/pvc/ingress），按资源类型独立配置 PromQL 指标
- 巡检规则 YAML 文件加载：configs/rules/inspection/*.yml，5 类内置规则独立文件，加载优先级 YAML > Go 内置 > DB

### Removed

- `config.yaml` 单体配置文件
- RuleGo 遗留文件 + MySQL 脚本
- 前端遗留死文件：view/jslibs、view/shared、根目录遗留测试 HTML、playwright 死依赖
- deploy/values.yaml 上游默认值全量拷贝（保留实际使用的 helm-otelcol-values.yaml）
- `.idea/` 取消 git 追踪；`config/hello.go` 启动副作用