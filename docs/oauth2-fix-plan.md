# 重明 (Mutong) OAuth2/OIDC 认证架构方案

> 最后更新：2026-06-04  
> 状态：方案评审（第三遍校验完成）  
> 涉及模块：认证中间件、OIDC 控制器、前端登录页、配置体系、fosite OAuth2 流程

---

## 一、现状分析（逐文件校验版）

### 1.1 当前认证架构

重明实现了**双栈认证**，共四条路径：

```
┌──────────────────────────────────────────────────────────┐
│                    BearerTokenMiddleware                   │
│         (所有请求前置，尝试解析身份到 ctx.user_id)          │
├──────────────────────────────────────────────────────────┤
│ Path 1: PAT/SAT (mtp_/mts_) → SHA-256 hash → DB 查用户   │
│ Path 2: OIDC Access Token → Zitadel UserInfo 实时验证     │
│ Path 3: 本地 fosite OAuth2 Token → introspect 验证       │
│ Path 4: Session Cookie (mutong_session) → HMAC-SHA256 验证 │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│              RequireAuthMiddleware (拦截 /api/ 路径)       │
│ 白名单: login, captcha, captcha/verify, login-status,    │
│         alerts/webhook, alerts/external/                   │
│ 其他 /api/* 路径 → 无 user_id → 401                       │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│              fosite OAuth2 栈 (非 /api/ 路径)             │
│ 路由: /oauth2/authorize, /oauth2/token, /oauth2/introspect│
│       /oauth2/revoke, /oauth2/device/*, /.well-known/...  │
│ 保护: BearerTokenMiddleware (弱保护)，不经过 RequireAuth  │
│ AuthorizeHandler: 未登录 → 重定向 /view/login.html         │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│              前端 /view/ 静态文件保护                      │
│ 白名单: login.html, consent.html, .css, .js, .png...     │
│ 未登录 → 重定向 /view/login.html                          │
└──────────────────────────────────────────────────────────┘
```

### 1.2 链路完整性检验

| 链路 | 当前状态 | 备注 |
|------|---------|------|
| PAT 认证（CLI `--token`） | ✅ 完整 | mtp_/mts_ → DB 校验 → 工作正常 |
| fosite Authorization Code + PKCE（CLI `auth login`） | ✅ 完整 | /oauth2/authorize → login.html → /oauth2/token → token 保存 |
| 本地密码登录（Web /api/auth/login） | ✅ 完整 | password → session 签发 → cookie → 正常进入仪表盘 |
| OIDC SSO 登录（Web） | ❌ 断裂 | 原因见 1.3 |

### 1.3 已确认的断裂点（逐文件校验）

| # | 断裂点 | 根因 | 验证方法 |
|---|--------|------|---------|
| **A** | `OIDCController` 未在 `main.go` 注册 | `git grep OIDCController cmd/main.go` 返回空 | `/api/auth/oidc/login` → 404 |
| **B** | `/api/auth/oidc/login` 和 `/api/auth/oidc/callback` 不在 `RequireAuthMiddleware` 白名单 | `auth_middleware.go` 第 189-196 行确认未列出 | 即使注册 → 401 |
| **C** | `/api/auth/login-status` **白名单已有但 handler 未实现** | `auth_middleware.go` 第 193 行有白名单，但 `auth_controller.go` 无对应 handler | 端点 404（不走 fosite 路径） |
| **D** | `login.html` SSO 按钮无条件渲染 | `login.html` 第 44 行无 `v-if` 判断 | `local` 模式点击 404 |

### 1.4 已排除的误判

| 先前误判 | 实际情况 | 排除依据 |
|---------|---------|---------|
| CLI 在 local 模式无法登录 | fosite OAuth2 链路完整，CLI `auth login` 通过 `/oauth2/authorize` → `login.html` → 密码登录 → code → token | `authorize.go` 第 37 行确认重定向 login.html；`login.js` `safeRedirect` 支持 `/oauth2/` 路径回跳 |
| `/oauth2/*` 路径需要额外白名单 | `/oauth2/` 不以 `/api/` 开头，`RequireAuthMiddleware` 对非 `/api/` 路径直接放行（第 200-202 行） | 实际走 BearerTokenMiddleware 弱保护，fosite 自行做授权决策 |
| fosite 的 device flow 在 local 模式断裂 | `/device` 路径在 ViewAuthGuard 白名单中，fosite device 流程完整 | `device.go` 确认已实现 |

---

## 二、目标

| 目标 | 优先级 | 说明 |
|------|--------|------|
| T1: hybrid 模式 SSO 登录完整打通 | **P0** | OIDC login → Zitadel → callback → session → 仪表盘 |
| T2: local 模式隐藏 SSO 按钮 | **P0** | 纯密码登录，不出现点击 404 |
| T3: 实现 login-status 端点 | **P0** | 白名单已有，补 handler，前端据此做条件渲染 |
| T4: OIDC 配置 key 通用化（zitadel → oidc） | **P1** | 向后兼容，支持任意 OIDC provider |

---

## 三、方案设计

### 3.1 修复断裂点 A — 注册 OIDCController

**文件**：`cmd/main.go`  
**位置**：第 511-512 行 `authCtrl.RegisterRoutes(engine)` 之后

```go
// Register auth endpoints (login, me, logout)
authCtrl := controllers.NewAuthController(cfg.DB, sessions, loginLimit)
authCtrl.RegisterRoutes(engine)

// Register OIDC endpoints (if OIDC client is available)
if oidcClient != nil {
    oidcCtrl := controllers.NewOIDCController(userSvc, oidcClient, sessions)
    oidcCtrl.RegisterRoutes(engine)
}

// Seed OAuth2 clients and initial admin
oauth2model.SeedClients(cfg.DB)
```

### 3.2 修复断裂点 B — 白名单补充

**文件**：`controllers/auth_middleware.go`  
**位置**：第 189-196 行

```go
func RequireAuthMiddleware() gin.HandlerFunc {
    publicPaths := []string{
        "/api/auth/login",
        "/api/auth/captcha",
        "/api/auth/captcha/verify",
        "/api/auth/login-status",
        "/api/auth/oidc/login",        // ← 新增: OIDC 登录发起
        "/api/auth/oidc/callback",     // ← 新增: OIDC 回调接收
        "/api/v1/alerts/webhook",
        "/api/v1/alerts/external/",
    }
    // ...
}
```

### 3.3 修复断裂点 C — 实现 login-status 端点

**文件**：`controllers/auth_controller.go`

```go
// RegisterRoutes 中新增
auth.GET("/login-status", ac.LoginStatus)
```

```go
// GET /api/auth/login-status
// 返回当前认证模式，供前端自适应渲染登录页面
func (ac *AuthController) LoginStatus(c *gin.Context) {
    mode := os.Getenv("MUTONG_AUTH_MODE")
    if mode == "" {
        mode = "local" // 默认 local
    }

    resp := gin.H{
        "mode": mode,
    }

    if mode == "hybrid" && ac.DB != nil {
        // 检查是否有合法的 OIDC 配置
        // 这里通过环境变量或配置判断 OIDC 是否可用
        oidcLabel := os.Getenv("MUTONG_OIDC_LABEL")
        if oidcLabel == "" {
            oidcLabel = "SSO"
        }
        resp["provider_label"] = oidcLabel
    }

    // 白名单放行，无需认证
    c.JSON(http.StatusOK, resp)
}
```

**注意**：`LoginStatus` handler 不应有认证要求，已在白名单中。

### 3.4 修复断裂点 D — 前端条件渲染

**文件**：`view/src/login.html`（第 44 行区域）

```html
<!-- 仅在 hybrid 模式下显示 SSO 按钮 -->
<button v-if="authMode === 'hybrid'" class="sso-btn" @click="loginWithZitadel">
    🔐 {{ ssoLabel }} 登录
</button>

<!-- local 模式显示密码登录 -->
<div v-if="authMode !== 'hybrid'" class="hybrid-divider">
    <span>使用密码登录</span>
</div>

<!-- hybrid 模式同时保留密码登录（divider + 表单） -->
<div v-if="authMode === 'hybrid'" class="hybrid-divider">
    <span>或使用密码登录</span>
</div>

<form @submit.prevent="handleLogin">
    <!-- ... -->
</form>
```

**文件**：`view/src/login.js`

```js
createApp({
    setup() {
        const username = ref('');
        const password = ref('');
        const error = ref('');
        const loading = ref(false);
        const authMode = ref('loading');    // 'local' | 'hybrid' | 'loading'
        const ssoLabel = ref('SSO');

        // 获取认证模式
        onMounted(async () => {
            try {
                const res = await fetch('/api/auth/login-status')
                const data = await res.json()
                authMode.value = data.mode || 'local'
                ssoLabel.value = data.provider_label || 'SSO'
            } catch {
                // API 不可达时，保守使用 local 模式
                authMode.value = 'local'
            }
        })

        function loginWithZitadel() {
            window.location.href = buildSSOLoginURL();
        }

        function buildSSOLoginURL() {
            return '/api/auth/oidc/login';
        }

        // ... 其余 handleLogin / safeRedirect 逻辑不变

        return { username, password, error, loading, authMode, ssoLabel, loginWithZitadel, handleLogin };
    }
}).mount('#app');
```

### 3.5 配置 key 通用化（P1 — 可选）

**变更范围**（不影响 P0 修复）：

| 文件 | 变更 |
|------|------|
| `config/config_base.go` | `ZitadelConfig` → `OIDCConfig`，字段不变，仅重命名 struct |
| `config/config_base.go:498` | `yaml:"zitadel"` → `yaml:"oidc"` |
| `config/config.go:100-101` | `c.Auth.Zitadel` → `c.Auth.OIDC` |
| `cmd/main.go:125-126` | `cfg.Auth.Zitadel` → `cfg.Auth.OIDC` |
| `configs/config.auth.yaml.example` | `zitadel:` → `oidc:` |

**向后兼容**：保留旧 struct 名作为 alias，启动时检测旧 key 并 warn：

```go
// config/config_base.go
type OIDCConfig ZitadelConfig  // 别名，零成本

func (c *AuthConfig) GetOIDCConfig() OIDCConfig {
    if c.OIDC.Issuer != "" {
        return c.OIDC
    }
    // 兼容旧 key
    if c.Zitadel.Issuer != "" {
        return OIDCConfig(c.Zitadel)  // 旧配置映射
    }
    return OIDCConfig{}
}
```

考虑到向后兼容的实现成本（~50 行），建议 P0 修复时不修改 key 名，在 v0.2.0 再做迁移。

---

## 四、变更清单

### 4.1 P0 变更（修复断裂）

| 文件 | 变更内容 | 行数 |
|------|---------|------|
| `cmd/main.go:512` | `if oidcClient != nil { ... }` 注册 OIDCController | ~4 |
| `controllers/auth_middleware.go:189` | 白名单新增 2 个路径 | ~2 |
| `controllers/auth_controller.go:32-39` | RegisterRoutes 新增 `/login-status`，新增 `LoginStatus` handler | ~20 |
| `view/src/login.html:44` | SSO 按钮改 `v-if="authMode === 'hybrid'"` + divider 条件渲染 | ~6 |
| `view/src/login.js:17-61` | 新增 authMode/ssoLabel ref + onMounted 探测 | ~15 |

**合计**：约 47 行新增/修改

### 4.2 P1 变更（后续优化）

| 文件 | 变更 | 优先级 |
|------|------|--------|
| `config/*` | `zitadel` → `oidc` 配置 key 重命名 + 兼容 | P1 |
| `services/auth/zitadel_oidc.go` | `ZitadelOIDCClient` → `OIDCClient` 重命名 | P2 |
| `models/user.go` | `ZitadelUserID` → `OIDCUserID` | P2（需 DB migration） |
| `docs/auth-design.md` | 更新文档 | P2 |

---

## 五、端到端链路验证

### 5.1 hybrid 模式完整流程

```
用户访问 /view/login.html
    │
    ├─ login.js onMounted → GET /api/auth/login-status
    │   └─ 返回 { mode: "hybrid", provider_label: "Zitadel" }
    │   └─ authMode = "hybrid"，显示 SSO 按钮
    │
    ├─ 用户点击 "🔐 Zitadel SSO 登录"
    │   └─ window.location = "/api/auth/oidc/login"
    │   └─ (不在 /api/ 白名单检查中？✅ 在白名单中)
    │   └─ OIDCController.BuildAuthURL() → 重定向到 Zitadel
    │
    ├─ Zitadel 认证成功
    │   └─ 回调 /api/auth/oidc/callback?code=xxx&state=xxx
    │   └─ (不在 /api/ 白名单检查中？✅ 在白名单中)
    │   └─ OIDCController.Exchange() → 换取 OIDC token + UserInfo
    │   └─ GetUserByZitadelID(Subject) → 不存在则 CreateUserFromZitadel
    │   └─ Sessions.Issue(...) → mutong_session cookie
    │   └─ 重定向 /view/index.html → 进入仪表盘
    │
    └─ 访问 /api/v1/alerts (需要认证)
        └─ BearerTokenMiddleware Path 4: mutong_session → HMAC 验证 → user_id 设置
        └─ RequireAuthMiddleware → has user_id → Next → 正常返回
```

### 5.2 local 模式完整流程

```
用户访问 /view/login.html
    │
    ├─ login.js onMounted → GET /api/auth/login-status
    │   └─ 返回 { mode: "local" }
    │   └─ authMode = "local"，SSO 按钮隐藏
    │
    ├─ 用户输入用户名密码 → POST /api/auth/login
    │   └─ password 验证 → session 签发 → cookie
    │   └─ 重定向 /view/index.html
    │
    └─ CLI: mutongctl auth login
        └─ /oauth2/authorize → login.html (已在 1.4 确认完整)
```

### 5.3 关键路径检查矩阵

| 路径 | RequireAuthMiddleware | BearerTokenMiddleware | 预期行为 |
|------|----------------------|----------------------|---------|
| `/api/auth/login` | ✅ 白名单放行 | — | 匿名密码登录 |
| `/api/auth/login-status` | ✅ 白名单放行 | — | 匿名获取模式 |
| `/api/auth/oidc/login` | ✅ 白名单放行（新增） | — | OIDC 登录发起 |
| `/api/auth/oidc/callback` | ✅ 白名单放行（新增） | — | OIDC 回调接收 |
| `/api/auth/me` | ❌ 需要 user_id | Path 1/4 设置 user_id | 返回用户信息 |
| `/oauth2/authorize` | ✅ 非 /api/ 路径放行 | Path 4 从 cookie 设置 user_id | fosite 授权 |
| `/oauth2/token` | ✅ 非 /api/ 路径放行 | — | fosite token 交换 |
| `/oauth2/device/auth` | ✅ 非 /api/ 路径放行 | — | 设备授权 |
| `/view/login.html` | ✅ 非 /api/ 路径放行 | — | 登录页（无需认证） |
| `/view/index.html` | ✅ 非 /api/ 路径放行 | 前端 inline guard 检查 session | 无 session → 重定向 login |

---

## 六、验证计划

| 场景 | 测试步骤 | 预期结果 |
|------|---------|---------|
| hybrid 模式 SSO | 1. `auth.mode=hybrid` 重启<br>2. 访问 login.html<br>3. 显示 SSO 按钮<br>4. 点击 SSO | 跳转到 Zitadel → 回调 → 仪表盘 |
| local 模式 | 1. `auth.mode=local` 重启<br>2. 访问 login.html | SSO 按钮不显示，只有密码表单 |
| login-status 端点 | `curl /api/auth/login-status` | local 返回 `{"mode":"local"}`，hybrid 返回 `{"mode":"hybrid","provider_label":"SSO"}` |
| OIDC 回调白名单 | 直接访问 `/api/auth/oidc/callback` | 不被 401 拦截（进入 OIDC controller 逻辑） |
| CLI local 模式 | `mutongctl auth login` | fosite 流程完整：login.html → 密码 → code → token |
| CLI hybrid 模式 | `mutongctl auth login` | fosite 流程完整（local/hybrid 共享此链路） |
| PAT 认证 | `mutongctl --token mtp_xxx alert list` | 正常返回，不受任何变更影响 |

---

## 七、风险与回退

| 风险 | 影响 | 缓解 |
|------|------|------|
| OIDC controller 注册后回调参数缺失 | Zitadel 回调 400 | 确认 Zitadel redirect URI 与 `config.auth.yaml` 一致 |
| 白名单路径写错 | SSO 登录被 401 | 修改后直接 `curl` 验证不 401 |
| login-status 端点返回异常 | 前端 fallback 为 local | try-catch 保底 |
| 旧配置兼容延迟 | 暂无（v0.2 再做） | P0 修复不涉及 key 重命名 |

**回退方案**：本次变更不涉及数据库 schema 修改、不影响 fosite/OIDC 中间件逻辑本身，仅新增路由注册 + 白名单 + handler + 前端条件渲染。任何问题可直接回滚 git。

---

## 八、不在本方案范围内

以下功能已有完整代码，**无需修改**：

- `controllers/oidc_controller.go` — OIDC login + callback，代码完整无需改动
- `services/auth/zitadel_oidc.go` — 标准 OIDC provider discovery，无 Zitadel 私有协议
- `BearerTokenMiddleware` Path 2 — OIDC token 验证已实现
- `BearerTokenMiddleware` Path 3 — fosite introspect 已实现
- `fosite oauth2_provider.go` — Authorization Code + PKCE + Refresh Token + Introspection + Revocation + OIDC 全部已注册
- `controllers/oauth2/authorize.go` — 未登录重定向 login.html 已实现
- `controllers/oauth2/device.go` — 设备授权流程已实现
- `cmd/mutongctl/auth/login.go` — CLI OAuth2 PKCE 流程已实现
- `configs/config.auth.yaml.example` — 旧 key 在 v0.2 前维持兼容
