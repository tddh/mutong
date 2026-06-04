# 重明 (Mutong) 待办事项

> 最后更新：2026-06-03

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
- **后端**：`/api/v1/inspection/rules` 已实现 POST/PUT/DELETE/toggle 完整 CRUD
- **现状**：前端巡检页面只展示 6 类内置规则结果
- **缺口**：无规则创建 / 编辑 / 删除 / 启停的 UI

### 5. 外部告警源管理
- **后端**：支持 4 种富化策略（node_affinity / service_graph / direct_business_app / business_labels），配置在 `config.alert.yaml`
- **现状**：API 仅 `GET /api/v1/alerts/sources` 列出已注册源
- **缺口**：
  - 无注册 / 修改 / 删除告警源的 API
  - 无前端告警源配置页面
  - 新增告警源需修改 YAML 配置文件并重启

### 6. 第三方认证（Zitadel OIDC） — 多处断裂
- **后端**：`ZitadelOIDCClient`（认证+PKCE 交换）+ `BearerTokenMiddleware`（路径 2 OIDC token 验证）已完整实现
- **缺口**：
  1. **OIDC 控制器未注册**：`OIDCController` 代码完整但 `main.go` 中从未实例化/注册，`/api/auth/oidc/login` 和 `/api/auth/oidc/callback` 全 404
  2. **RequireAuthMiddleware 未放行**：`/api/auth/oidc/login` 和 `/api/auth/oidc/callback` 不在白名单中（`/api/` 路径下），即使注册也会被 401 拦截
  3. **前端 Zitadel SSO 按钮始终可见**：`login.html` 的 "🔐 Zitadel SSO 登录" 按钮没有条件渲染，`local` 模式下点击直接 404
  4. **无认证模式探测接口**：前端无法判断 `auth.mode` 是 `local` 还是 `hybrid`，应提供 `/api/auth/login-status` 或类似端点
  5. **用户接口不完整**：`UserInterface` 仅有 `GetUserByID` + Zitadel 相关方法，缺少 `CreateUser`、`UpdateUser`、`DeleteUser`
  6. **本地 OAuth2 无登录入口**：fosite OAuth2 Provider 已注册（`/oauth2/authorize`、`/oauth2/token`、`/introspect`、`/revoke`），但无 Web UI 登录页面引导用户获取 access token
- *需修复：`cmd/main.go` 添加 OIDCController 注册 + `auth_middleware.go` 添加白名单 + `login.html` 条件渲染 + 新增 login-status 端点*
- *涉及：`controllers/oidc_controller.go`、`controllers/auth_middleware.go`、`cmd/main.go`、`view/src/login.html`*

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
