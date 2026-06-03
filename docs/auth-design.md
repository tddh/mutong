# 重明 (Mutong) 用户认证体系设计文档

> **版本**: v2.2  
> **状态**: 已实施（Zitadel + 本地混合架构）  
> **最后更新**: 2026-06-03  
> **依赖**: Go 1.25, Gin v1.10, GORM v1.31, PostgreSQL, Redis, Vue 3, Vite 5, Zitadel (OIDC)

---

## 目录

1. [架构概览](#1-架构概览)
2. [OAuth 2.0 授权服务器](#2-oauth-20-授权服务器)
3. [数据模型](#3-数据模型)
4. [API 设计](#4-api-设计)
5. [CLI 认证 (mutongctl)](#5-cli-认证-mutongctl)
6. [前端登录页面](#6-前端登录页面)
7. [中间件链](#7-中间件链)
8. [安全分层防护](#8-安全分层防护)
10. [实施计划](#10-实施计划)
11. [附录：技术选型依据](#11-附录技术选型依据)

---

## 1. 架构概览

### 1.1 混合认证架构（Zitadel + 本地）

```
┌──────────────────────────────────────────────────────────────┐
│              Mutong Auth Architecture (Hybrid)                │
│                                                               │
│  认证三通道 (统一入口: BearerTokenMiddleware)                  │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ Path 1: PAT/SAT (mtp_/mts_ 前缀)                       │ │
│  │   → SHA-256 hash 查 PostgreSQL → 本地用户映射            │ │
│  │   → 场景: CI/CD、Agent、非交互式脚本                     │ │
│  ├─────────────────────────────────────────────────────────┤ │
│  │ Path 2: OIDC Access Token (Zitadel 签发 JWT)           │ │
│  │   → 调用 Zitadel UserInfo 端点实时验证                   │ │
│  │   → zitadel_user_id 映射到本地 User                     │ │
│  │   → 场景: 浏览器 SSO、API Bearer Token                  │ │
│  ├─────────────────────────────────────────────────────────┤ │
│  │ Path 3: 本地 Session Cookie (mutong_session)            │ │
│  │   → HMAC-SHA256 签名验证 → 回退 UUID 明文（兼容旧版）    │ │
│  │   → 场景: 浏览器登录后的常规请求                          │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                               │
│  用户登录 (Web UI)                                            │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ Vue 3 SPA → /api/auth/oidc/login → Zitadel 登录页       │ │
│  │          → Zitadel 处理 SSO/MFA/密码                      │ │
│  │          → /api/auth/oidc/callback → 本地 Session 签发   │ │
│  │          → 本地模式备选: /api/auth/login (Argon2id)      │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                               │
│  CLI 登录 (mutongctl)                                         │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ mutongctl auth login → Authorization Code Grant + PKCE   │ │
│  │                      → 浏览器回调 → Token 保存本地         │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                               │
│  职责划分                                                     │
│  ┌───────────────────────┬─────────────────────────────────┐ │
│  │  Zitadel (IDP)        │  Mutong 本地                     │ │
│  ├───────────────────────┼─────────────────────────────────┤ │
│  │  ✅ 用户身份认证      │  ✅ Casbin RBAC (admin/operator  │ │
│  │  ✅ OIDC / OAuth2     │      /viewer)                    │ │
│  │  ✅ SSO / MFA         │  ✅ PAT/SAT API Token            │ │
│  │  ✅ OAuth2 授权码认证  │  ✅ 本地 Session (HMAC 签名)     │ │
│  │  ✅ Token 签发        │  ✅ 用户-角色本地映射             │ │
│  │                        │  ✅ 本地 OAuth2 Provider (fosite) │ │
│  │                        │  ✅ 登录限流                 │ │
│  │                        │  ✅ Argon2id 密码 (local 模式)   │ │
│  └───────────────────────┴─────────────────────────────────┘ │
│                                                               │
│  后端核心                                                     │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │ coreos/go-oidc/v3  → Zitadel OIDC 客户端                │ │
│  │ ory/fosite         → 本地 OAuth 2.0 Provider            │ │
│  │ casbin v3          → RBAC 权限                           │ │
│  │ GORM + PostgreSQL  → 用户/Token/Session 存储             │ │
│  │ Redis              → 限流 / Session 缓存                  │ │
│  └─────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────┘
```

### 1.2 Token 类型

| Token | 前缀 | 生命周期 | 使用者 | 存储/验证 |
|-------|------|----------|--------|----------|
| Zitadel Access Token | `eyJ...` (JWT) | 由 Zitadel 配置 | 浏览器 / API | Zitadel UserInfo 端点实时验证 |
| Zitadel Refresh Token | — | 由 Zitadel 配置 | 浏览器 | Zitadel 管理 |
| 本地 Session | `mutong_session` Cookie | 可配置 | 浏览器 | HMAC-SHA256 签名，base64 编码 |
| PAT | `mtp_` | 30-365 天 | 人工 CLI | SHA-256 hash → DB |
| SAT | `mts_` | 30-365 天 | Agent/CI | SHA-256 hash → DB |

### 1.3 核心依赖

```go
// go.mod 认证相关依赖
require (
    github.com/coreos/go-oidc/v3 v3.x           // Zitadel OIDC 客户端
    github.com/ory/fosite v0.49.0               // 本地 OAuth 2.0 Provider
    golang.org/x/oauth2 v0.34.0                 // OAuth2 客户端 (Device Flow)
    github.com/casbin/casbin/v3 v3.10.0         // RBAC 权限
    github.com/casbin/gorm-adapter/v3 v3.32.0   // Casbin GORM 适配器
    golang.org/x/crypto v0.50.0                 // Argon2id / bcrypt
    github.com/google/uuid v1.6.0               // UUID 生成
)
```

### 1.4 已完成的重构

| 原始文件 | 重构后 | 状态 |
|----------|--------|------|
| `models/user_models.go` | → `models/user.go`（新增 `zitadel_user_id`, `auth_provider`） | ✅ |
| `models/role_models.go` | → `models/role.go`（扩展权限字段） | ✅ |
| `services/user_service.go` | → 扩展 `GetUserByZitadelID`, `CreateUserFromZitadel` | ✅ |
| `services/role_service.go` | → 保留并扩展 | ✅ |
| `interfaces/user_interface.go` | → 保留接口，扩展方法 | ✅ |
| `interfaces/role_interface.go` | → 保留接口，扩展方法 | ✅ |

### 1.5 认证配置文件

`configs/config.auth.yaml.example`:

```yaml
auth:
  # 认证模式: local (仅本地) / hybrid (Zitadel + 本地)
  mode: local

  # OAuth 2.0 发行者 URL
  issuer: "https://mutong.example.com"

  # Token 生命周期
  access_token_lifespan: 15m
  refresh_token_lifespan: 168h
  auth_code_lifespan: 10m
  device_code_lifespan: 10m

  # 全局密钥 (32 bytes base64)
  global_secret: ""

  # RSA 私钥路径
  rsa_private_key_path: ""

  # Zitadel OIDC 配置（hybrid 模式时必填）
  zitadel:
    issuer: "https://zitadel.example.com"
    client_id: "mutong-web"
    client_secret: ""
    redirect_uri: "https://mutong.example.com/api/auth/oidc/callback"
    audience: "mutong-api"
    device_client: "mutongctl"
    scopes:
      - openid
      - profile
      - email
      - offline_access

  login_ratelimit:
    enabled: true
    ip_max_per_minute: 10
    redis_prefix: "ratelimit:login:"

  password:
    algorithm: argon2id
    argon2:
      memory: 65536
      iterations: 3
      parallelism: 4
```

### 1.6 认证相关目录结构

```
mutong/
├── models/ouath2/                       # OAuth 2.0 GORM 数据模型
│   ├── client.go                       #   OAuth2Client
│   ├── authorization_code.go           #   AuthorizationCode
│   ├── grant.go                        #   Grant (user↔client)
│   └── device_code.go                  #   DeviceCode (RFC 8628)
│
├── services/auth/                      # 认证服务层
│   ├── zitadel_oidc.go                #   Zitadel OIDC 客户端 (go-oidc)
│   ├── session.go                     #   本地 Session 管理 (HMAC 签名)
│   ├── token.go                        #   PAT/SAT 生成/验证
│   ├── password.go                     #   Argon2id 密码哈希
│   ├── login_limiter.go               #   登录限流/锁定
│   ├── casbin.go                       #   Casbin RBAC 集成
│   ├── oauth2_provider.go             #   本地 fosite OAuth2 Provider
│   ├── oauth2_storage.go              #   GORM 存储实现(fosite)
│   └── service.go                      #   服务层门面
│
├── controllers/                        # HTTP handlers
│   ├── auth_middleware.go             #   BearerTokenMiddleware + RequireAuth
│   ├── auth_controller.go             #   本地登录/PAT CRUD
│   ├── oidc_controller.go             #   Zitadel OIDC 登录/回调
│   ├── oauth2/                         #   OAuth 2.0 端点 (fosite)
│   │   ├── authorize.go
│   │   ├── token.go
│   │   ├── device.go
│   │   ├── introspect.go
│   │   └── discovery.go
│
├── cmd/mutongctl/auth/                 # CLI 认证命令
│   ├── login.go                        #   auth login (Device Flow)
│   └── token_store.go                  #   ~/.mutong/tokens.json
│
└── view/src/                           # 前端页面
    ├── login.html                      #   登录页 (含 CAPTCHA)
    ├── login.js                        #   登录逻辑
    ├── consent.html                    #   OAuth 授权确认页
    ├── consent.js                      #   授权确认逻辑
    ├── stores/auth.js                  #   Pinia Auth Store
    └── utils/http.js                   #   Axios + 401 自动刷新
```

---

## 2. 认证流程

### 2.1 Zitadel OIDC 登录流程（hybrid 模式）

Muton 委托用户身份认证给 Zitadel，通过标准 OIDC Authorization Code + PKCE 流程实现 SSO：

```
1. 用户访问 /view/login.html
2. 前端重定向到 /api/auth/oidc/login
3. 服务端构建 Zitadel Auth URL (PKCE code_challenge=S256, state=random)
   重定向到 Zitadel 登录页
4. Zitadel 处理用户认证（用户名/密码/MFA/社交登录等）
5. Zitadel 302 回调 /api/auth/oidc/callback?code=xxx&state=yyy
6. 服务端验证 state、用 code_verifier 交换 token
7. 解析 id_token claims → ZitadelUserInfo (sub, preferred_username, email, name)
8. 按 zitadel_user_id 查找本地 User → 不存在则自动创建（角色默认 viewer）
9. SessionManager.Issue() 签发本地 session
10. 设置 mutong_session cookie，重定向到首页
```

**ZitadelOIDCClient** (`services/auth/zitadel_oidc.go`):
- 基于 `coreos/go-oidc/v3` 实现 OIDC Provider 发现
- 实现 `AccessTokenValidator` 接口：`ValidateAccessToken()` 调用 Zitadel UserInfo 端点验证 Bearer token
- 用户-本地映射：`User.zitadel_user_id` 字段桥接 Zitadel 身份

### 2.2 本地 OAuth2 Provider (fosite)

本地保留基于 ory/fosite 的 OAuth2 Provider，支持以下 Grant Type：

| Grant Type | RFC | 用途 |
|------------|-----|------|
| Authorization Code + PKCE | 7636 | 浏览器登录（本地模式） |
| Client Credentials | 6749 | 服务间调用 |
| Refresh Token | 6749 | Token 续期 |
| Device Authorization Grant | 8628 | CLI 登录 |

### 2.3 OIDC/Token 验证端点

| 方法 | 路径 | 用途 |
|------|------|------|
| GET | `/api/auth/oidc/login` | 发起 Zitadel 登录（302 → Zitadel） |
| GET | `/api/auth/oidc/callback` | Zitadel 回调（code 交换 + session 签发） |
| GET/POST | `/oauth2/authorize` | 本地 OAuth2 授权（本地模式） |
| POST | `/oauth2/token` | 交换/刷新 Token |
| POST | `/oauth2/device/auth` | 设备授权（RFC 8628） |
| POST | `/oauth2/introspect` | Token 内省 |
| POST | `/oauth2/revoke` | 撤销 Token |
| GET | `/.well-known/openid-configuration` | OIDC Discovery 元数据 |

---

## 3. 数据模型

### 3.1 数据库迁移

在 `config/config.go` 的 `SetupDB()` AutoMigrate 中新增：

```go
db.AutoMigrate(
    // ... 已有表 ...
    &oauth2model.OAuth2Client{},
    &oauth2model.AuthorizationCode{},
    &oauth2model.Grant{},
    &oauth2model.DeviceCode{},
    &User{},
    &APIToken{},
    &ServiceAccount{},
)
```

### 3.2 OAuth2Client

```go
// models/oauth2/client.go
type OAuth2Client struct {
    ID                 uint      `gorm:"primaryKey"`
    ClientID           string    `gorm:"uniqueIndex;size:255;not null"`
    ClientSecret       []byte    `gorm:"size:60"`                    // bcrypt hash
    Name               string    `gorm:"size:255;not null"`
    RedirectURIs       string    `gorm:"type:text;not null"`         // JSON array
    GrantTypes         string    `gorm:"type:text;not null"`         // JSON array
    ResponseTypes      string    `gorm:"type:text;not null"`         // JSON array
    Scopes             string    `gorm:"type:text;not null"`         // JSON array
    Public             bool      `gorm:"not null;default:false"`
    CreatedAt          time.Time
    UpdatedAt          time.Time
    DeletedAt          gorm.DeletedAt `gorm:"index"`
}

// 预置客户端
func SeedClients(db *gorm.DB) {
    clients := []OAuth2Client{
        {
            ClientID:      "mutong-web",
            ClientSecret:  bcryptHash(""),  // Public client
            Name:          "Mutong Web UI",
            RedirectURIs:  `["/view/callback"]`,
            GrantTypes:    `["authorization_code","refresh_token"]`,
            ResponseTypes: `["code"]`,
            Scopes:        `["openid","profile"]`,
            Public:        true,
        },
        {
            ClientID:      "mutongctl",
            ClientSecret:  bcryptHash(""),  // Public client (Device Flow)
            Name:          "Mutong CLI",
            RedirectURIs:  `[]`,
            GrantTypes:    `["urn:ietf:params:oauth:grant-type:device_code","refresh_token"]`,
            ResponseTypes: `[]`,
            Scopes:        `["openid","profile","read","write"]`,
            Public:        true,
        },
    }
    for _, c := range clients {
        db.Where("client_id = ?", c.ClientID).FirstOrCreate(&c)
    }
}
```

### 3.3 AuthorizationCode

```go
// models/oauth2/authorization_code.go
type AuthorizationCode struct {
    ID                  uint      `gorm:"primaryKey"`
    Code                string    `gorm:"uniqueIndex;size:255;not null"`
    CodeChallenge       string    `gorm:"size:128"`          // PKCE S256
    CodeChallengeMethod string    `gorm:"size:10;default:S256"`
    ClientID            string    `gorm:"index;size:255"`
    UserID             uint       `gorm:"index"`
    Scope               string    `gorm:"size:1024"`
    RedirectURI         string    `gorm:"size:2048"`
    Request             string    `gorm:"type:text"`         // 序列化 fosite.Requester
    Session             string    `gorm:"type:text"`         // 序列化 session
    ExpiresAt           time.Time `gorm:"index;not null"`
    CreatedAt           time.Time
}
```

### 3.4 Grant (user↔client 授权关系)

```go
// models/oauth2/grant.go
type Grant struct {
    ID            uint      `gorm:"primaryKey"`
    UserID        uint      `gorm:"index:idx_user_client,unique;not null"`
    ClientID      string    `gorm:"index:idx_user_client,unique;size:255;not null"`
    Scope         string    `gorm:"size:1024"`
    Counter       int64     `gorm:"not null;default:0"`     // refresh token 轮换计数
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

### 3.5 DeviceCode

```go
// models/oauth2/device_code.go
type DeviceCode struct {
    ID            uint      `gorm:"primaryKey"`
    DeviceCode    string    `gorm:"uniqueIndex;size:255;not null"`  // 轮询用
    UserCode      string    `gorm:"uniqueIndex;size:32;not null"`   // 用户输入用 (8 字符)
    ClientID      string    `gorm:"index;size:255"`
    Scope         string    `gorm:"size:1024"`
    Status        string    `gorm:"size:20;not null;default:pending"` // pending/authorized/denied/expired
    UserID        *uint     `gorm:"index"`
    Request       string    `gorm:"type:text"`                       // 序列化 fosite.Requester
    Session       string    `gorm:"type:text"`                       // 序列化 session
    ExpiresAt     time.Time `gorm:"index;not null"`
    CreatedAt     time.Time
}
```

### 3.6 User

```go
// models/user.go
type User struct {
    ID           uint           `gorm:"primaryKey"`
    UUID         string         `gorm:"uniqueIndex;size:36;not null"` // 对外 ID
    Username     string         `gorm:"uniqueIndex;size:64;not null"`
    PasswordHash string         `gorm:"size:256"`                     // Argon2id (local 模式需要)
    Email        string         `gorm:"size:255"`
    ZitadelUserID *string       `gorm:"uniqueIndex;size:255"`        // Zitadel 用户 ID（sub），桥接字段
    AuthProvider  string        `gorm:"size:16;not null;default:local"` // "local" | "zitadel"
    Role         string         `gorm:"size:32;not null;default:viewer"` // admin/operator/viewer
    Status       string         `gorm:"size:16;not null;default:active"`
    LastLoginAt  *time.Time
    CreatedAt    time.Time
    UpdatedAt    time.Time
    DeletedAt    gorm.DeletedAt `gorm:"index"`
}
```
- `ZitadelUserID`：当用户通过 Zitadel SSO 首次登录时，从 id_token 的 `sub` claim 提取并写入此字段；后续 BearerTokenMiddleware Path 2 通过此字段完成 Zitadel → 本地用户映射。
- `AuthProvider`：标识用户来源，`"local"` 表示本地密码登录，`"zitadel"` 表示通过 Zitadel SSO 创建。

### 3.7 APIToken (PAT + SAT)

```go
// models/api_token.go
type APIToken struct {
    ID              uint           `gorm:"primaryKey"`
    UserID          *uint          `gorm:"index"`  // PAT 关联用户
    ServiceAccountID *uint         `gorm:"index"`  // SAT 关联服务账号
    Name            string         `gorm:"size:255;not null"`
    TokenPrefix     string         `gorm:"size:12;not null"`      // "mtp_" or "mts_" + 前 8 字符
    TokenHash       string         `gorm:"uniqueIndex;size:64;not null"` // SHA-256 hex
    TokenType       string         `gorm:"size:4;not null"`       // "pat" / "sat"
    Scopes          string         `gorm:"type:text;not null"`    // JSON array
    ExpiresAt       *time.Time     `gorm:"index"`
    LastUsedAt      *time.Time
    RevokedAt       *time.Time     `gorm:"index"`
    RotatedFromID   *uint
    CreatedAt       time.Time
}

// Token 生成: 仅返回一次原文
func GenerateAPIToken(tokenType string) (raw string, prefix string, hash string) {
    b := make([]byte, 32)
    rand.Read(b)
    hex := hex.EncodeToString(b)
    raw = tokenType + "_" + hex           // "mtp_a1b2c3d4..."
    prefix = raw[:12]                      // "mtp_a1b2c3d4"
    h := sha256.Sum256([]byte(raw))
    hash = hex.EncodeToString(h[:])
    return
}
```

### 3.8 ServiceAccount (机器身份)

```go
// models/service_account.go
type ServiceAccount struct {
    ID          uint           `gorm:"primaryKey"`
    Name        string         `gorm:"size:128;not null"`
    Description string         `gorm:"size:512"`
    Status      string         `gorm:"size:16;not null;default:active"`
    CreatedBy   *uint          `gorm:"index"`
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DeletedAt   gorm.DeletedAt `gorm:"index"`
}
```

---

## 4. API 设计

### 4.1 OAuth 2.0 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/oauth2/authorize` | 发起授权（302 → 登录页） |
| POST | `/oauth2/authorize` | 用户确认授权 |
| POST | `/oauth2/token` | Token 交换/刷新 |
| POST | `/oauth2/device/auth` | 设备授权 |
| POST | `/oauth2/introspect` | Token 内省 |
| POST | `/oauth2/revoke` | Token 撤销 |
| GET | `/.well-known/openid-configuration` | OIDC Discovery |

### 4.2 认证 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/login` | 登录（用户名+密码 → 设置 Cookie Session） |
| GET | `/api/auth/consent` | OAuth 授权确认页面数据 |
| POST | `/api/auth/consent` | 用户确认/拒绝授权 |
| GET | `/api/auth/me` | 获取当前用户信息 |
| POST | `/api/auth/logout` | 登出（清除 Session + Cookie） |

### 4.3 Token 管理 API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/auth/tokens` | 创建 PAT |
| GET | `/api/auth/tokens` | 列出我的 Token（仅元数据） |
| DELETE | `/api/auth/tokens/:id` | 撤销 Token |
| POST | `/api/auth/service-accounts` | 创建 Service Account (admin) |
| GET | `/api/auth/service-accounts` | 列出 Service Account (admin) |
| POST | `/api/auth/service-accounts/:id/tokens` | 为 SA 创建 SAT (admin) |
| DELETE | `/api/auth/service-accounts/:id/tokens/:tid` | 撤销 SAT (admin) |

### 4.4 PAT/SAT 创建 API

```
POST /api/auth/tokens
Authorization: Bearer <oauth_access_token>
Body:
{
    "name": "My CI Token",
    "type": "pat",           // "pat" | "sat"
    "scopes": ["read:alerts", "read:resources"],
    "expires_in_days": 90
}
Response 201:
{
    "id": 42,
    "name": "My CI Token",
    "token": "mtp_a1b2c3d4e5f6...",    // ← 仅此一次！
    "prefix": "mtp_a1b2c3d4",
    "type": "pat",
    "scopes": ["read:alerts", "read:resources"],
    "expires_at": "2026-08-29T00:00:00Z"
}
```

---

## 5. CLI 认证 (mutongctl)

### 6.1 mutongctl auth login

使用 OAuth 2.0 Authorization Code Grant + PKCE（RFC 7636）：

1. CLI 启动本地 HTTP 服务器监听随机端口（`127.0.0.1:<port>/callback`）
2. 生成 PKCE `code_verifier`（32 字节随机数，base64url 编码）和 `code_challenge`（SHA256(verifier)）
3. 构建授权 URL 并在浏览器打开：`/oauth2/authorize?client_id=mutongctl&redirect_uri=...&code_challenge=...&code_challenge_method=S256`
4. 用户在浏览器中登录并授权
5. 浏览器重定向到本地 callback，CLI 从 URL 提取 `code` 和 `state`
6. CLI 用 `code` + `code_verifier` 向 `/oauth2/token` 交换 access token
7. Token 保存到 `~/.mutong/tokens.json`


### 6.2 Token 认证链（优先级）

```
1. --token 标志 (mutongctl -t mtp_xxx ...)
2. MUTONG_TOKEN 环境变量
3. ~/.mutong/tokens.json (OAuth 2.0 refresh token → 自动刷新)
4. ~/.mutong/config.yaml (手动配置的 PAT)
```

### 6.3 Token 自动刷新

```go
// cmd/mutongctl/internal/client/token_refresher.go
func (c *Client) ensureValidToken() error {
    if c.token == "" || c.tokenExpired() {
        token, err := c.refreshFromFile() // 从 tokens.json 用 refresh_token 换新
        if err != nil {
            return fmt.Errorf("token expired, run 'mutongctl auth login': %w", err)
        }
        c.token = token
    }
    return nil
}
```

---

## 6. 前端登录页面

### 6.1 页面布局

```
┌─────────────────────────────────────────────┐
│                                             │
│          🏔️  牧童 · 重明                     │
│        Kubernetes AIOps 智能运维平台          │
│                                             │
│  ┌─────────────────────────────────────┐    │
│  │  用户名:  [________________]         │    │
│  │  密码:    [________________]         │    │
│  │                                     │    │
│  │  [        登  录        ]            │    │
│  └─────────────────────────────────────┘    │
│                                             │
│  命令行登录: mutongctl auth login            │
│  — 或 — 使用 API Token 访问                 │
│                                             │
└─────────────────────────────────────────────┘
```

### 6.2 文件

| 文件 | 作用 |
|------|------|
| `view/src/login.html` | 登录页 (独立 HTML 入口) |
| `view/src/login.js` | 登录逻辑 (Vue 3) |
| `view/src/consent.html` | OAuth 授权确认页 |
| `view/src/consent.js` | 授权确认逻辑 |

### 6.3 Vite 构建配置

```js
// vite.config.js rollupOptions.input 新增
login: resolve(__dirname, 'src/login.html'),
consent: resolve(__dirname, 'src/consent.html'),
```

### 6.4 样式设计

复用 `common.css` 变量 (`--primary-color: #1890ff`, `--bg-color: #f0f2f5` 等)。

登录卡片：400px 宽，居中，白色背景，8px 圆角，轻阴影。

### 6.5 Auth Store (Pinia)

```js
// view/src/stores/auth.js
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useAuthStore = defineStore('auth', () => {
    const status = ref('unknown')  // unknown | loggedOut | loggedIn
    const accessToken = ref(null)
    const user = ref(null)

    const isAuthenticated = computed(() => status.value === 'loggedIn')

    async function init() {
        try {
            const res = await fetch('/oauth2/token', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: 'grant_type=refresh_token&client_id=mutong-web',
                credentials: 'include',
            })
            if (res.ok) {
                const data = await res.json()
                accessToken.value = data.access_token
                await fetchMe()
            } else {
                status.value = 'loggedOut'
            }
        } catch {
            status.value = 'loggedOut'
        }
    }

    async function fetchMe() {
        const res = await fetch('/api/auth/me', {
            headers: { Authorization: `Bearer ${accessToken.value}` },
        })
        user.value = await res.json()
        status.value = 'loggedIn'
    }

    async function logout() {
        await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' })
        accessToken.value = null
        user.value = null
        status.value = 'loggedOut'
    }

    function setAuth(token, userData) {
        accessToken.value = token
        user.value = userData
        status.value = 'loggedIn'
    }

    return { status, accessToken, user, isAuthenticated, init, fetchMe, logout, setAuth }
})
```

### 6.6 HTTP 拦截器

```js
// view/src/utils/http.js
import { useAuthStore } from '../stores/auth.js'

let isRefreshing = false
let refreshQueue = []

export async function request(url, options = {}) {
    const auth = useAuthStore()
    const headers = {
        'Content-Type': 'application/json',
        ...(auth.accessToken && { Authorization: `Bearer ${auth.accessToken}` }),
        ...options.headers,
    }

    let res = await fetch(url, { ...options, headers })
    if (res.status === 401 && !isRefreshing) {
        isRefreshing = true
        try {
            const refreshRes = await fetch('/oauth2/token', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: 'grant_type=refresh_token&client_id=mutong-web',
                credentials: 'include',
            })
            if (refreshRes.ok) {
                const data = await refreshRes.json()
                auth.accessToken = data.access_token
                // 重试原始请求
                headers.Authorization = `Bearer ${data.access_token}`
                res = await fetch(url, { ...options, headers })
            } else {
                auth.logout()
                window.location.href = '/view/login.html'
            }
        } finally {
            isRefreshing = false
        }
    }
    if (!res.ok) throw new Error(`API Error: ${res.status}`)
    return res.json()
}
```

---

## 7. 中间件链

### 7.1 中间件注册顺序

```go
// cmd/main.go initializeGin()
engine.Use(gin.Logger())                        // 1. 请求日志
engine.Use(TraceIDMiddleware())                 // 2. TraceID
engine.Use(cors.New(...))                       // 3. CORS
engine.Use(RateLimitMiddleware(...))            // 4. 全局 IP 限流
engine.Use(BearerTokenMiddleware(              // 5. ★ Token 认证（三条路径）
    db, sessionManager, oidcClient))
// 路由
oauth2Group := engine.Group("/oauth2")          // OAuth 2.0 端点 (公开)
apiGroup := engine.Group("/api/v1")             // 业务 API
apiGroup.Use(RequireAuthMiddleware())           // 6. ★ 强制认证（白名单放行）
// Phase 5: apiGroup.Use(CasbinMiddleware(...))   // 7. RBAC 授权
```

### 7.2 BearerTokenMiddleware（统一 Token 验证 — 三条路径）

```go
// controllers/auth_middleware.go
func BearerTokenMiddleware(db *gorm.DB, sessions *auth.SessionManager, validator auth.AccessTokenValidator) gin.HandlerFunc {
    return func(ctx *gin.Context) {
        authHeader := ctx.GetHeader("Authorization")
        if strings.HasPrefix(authHeader, "Bearer ") {
            token := strings.TrimPrefix(authHeader, "Bearer ")

            // Path 1: 本地 PAT/SAT (mtp_/mts_ 前缀)
            if strings.HasPrefix(token, "mtp_") || strings.HasPrefix(token, "mts_") {
                h := sha256.Sum256([]byte(token))
                hash := hex.EncodeToString(h[:])
                // 查 PostgreSQL: token_hash 匹配 + 未撤销 + 未过期
                var t models.APIToken
                db.Where("token_hash = ? AND revoked_at IS NULL", hash).
                    Where("expires_at IS NULL OR expires_at > ?", time.Now()).
                    First(&t)
                // set ctx: user_id, user_uuid, token_type, scopes
                ctx.Next()
                return
            }

            // Path 2: Zitadel OIDC Access Token（委托验证）
            if validator != nil {
                claims, err := validator.ValidateAccessToken(ctx, token)
                if err == nil {
                    // 通过 zitadel_user_id 查找本地 User
                    var user models.User
                    db.Where("zitadel_user_id = ?", claims.Subject).First(&user)
                    ctx.Set("user_id", user.ID)
                    ctx.Set("user_uuid", user.UUID)
                    ctx.Set("token_type", "oidc")
                    ctx.Next()
                    return
                }
            }

            // 无有效 Bearer token → 401
            ctx.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
            return
        }

        // Path 3: 本地 Session Cookie (mutong_session)
        if sessions != nil {
            if cookie, err := ctx.Cookie("mutong_session"); err == nil && cookie != "" {
                claims, err := sessions.Verify(cookie) // HMAC-SHA256 验证
                if err == nil {
                    ctx.Set("user_id", claims.UserID)
                    ctx.Set("user_uuid", claims.UUID)
                    ctx.Set("token_type", "session")
                } else {
                    // 向后兼容：回退到明文 UUID 查找（旧版 local login）
                    var user models.User
                    db.Where("uuid = ?", cookie).First(&user)
                    ctx.Set("user_id", user.ID)
                    ctx.Set("user_uuid", user.UUID)
                    ctx.Set("token_type", "session")
                }
            }
        }
        ctx.Next() // Session 路径不会 abort，采用被动认证
    }
}
```

**三条路径总结**：

| 路径 | 触发条件 | 验证方式 | 用户解析 |
|------|---------|---------|---------|
| Path 1: PAT/SAT | `mtp_` 或 `mts_` 前缀 | PostgreSQL SHA-256 hash 比对 | `user_id` 外键 |
| Path 2: OIDC | 任何非 mtp_/mts_ 的 Bearer token | 调用 Zitadel UserInfo 端点 | `zitadel_user_id` → 本地 User |
| Path 3: Session | `mutong_session` Cookie | HMAC-SHA256 签名（回退 UUID 明文） | `User.ID` / `User.UUID` |

### 7.3 RequireAuthMiddleware

```go
func RequireAuthMiddleware() gin.HandlerFunc {
    return func(ctx *gin.Context) {
        if _, exists := ctx.Get("user_id"); !exists {
            ctx.AbortWithStatusJSON(401, gin.H{"error": "authentication required"})
            return
        }
        ctx.Next()
    }
}
```

---

## 8. 安全分层防护

```
┌─────────────────────────────────────────────────────────────┐
│ 第 1 层 — 传输安全                                           │
│ HTTPS (TLS 1.3)                                             │
├─────────────────────────────────────────────────────────────┤
│ 第 2 层 — IP 级速率限制                                      │
│ Redis 滑动窗口: 每 IP 10 req/min (全局)                      │
│ 登录端点单独: 每 IP 5 req/min                                │
├─────────────────────────────────────────────────────────────┤
│ 第 3 层 — 账号级保护                                         │
│ 5 次失败 → 15 分钟锁定 (Redis TTL 900s)                      │
│ 10 次失败 → 1 小时锁定                                       │
├─────────────────────────────────────────────────────────────┤
│ 第 4 层 — OAuth 2.0 协议安全                                 │
│ PKCE S256 强制 (防授权码拦截)                                 │
│ Refresh Token Rotation (防重放)                              │
│ Device Code 10min 过期 (防暴力猜测)                           │
│ Token Introspection (实时验证)                                │
│ Client Secret bcrypt 存储 (防数据库泄露)                      │
├─────────────────────────────────────────────────────────────┤
│ 第 5 层 — Token 安全                                         │
│ Access Token: 15min JWT (RS256)                              │
│ Refresh Token: HttpOnly + Secure + SameSite=Strict Cookie    │
│ PAT/SAT: SHA-256 hash 存储 (防数据库泄露还原)                 │
│ jti 黑名单: Redis (即时撤销)                                  │
├─────────────────────────────────────────────────────────────┤
│ 第 6 层 — 密码安全                                           │
│ Argon2id (m=64MB, t=3, p=4)                                  │
│ bcrypt 备选 (cost=12)                                        │
│ 禁止弱密码 (长度 < 8, 纯数字, 常见密码黑名单)                   │
├─────────────────────────────────────────────────────────────┤
│ 第 7 层 — 审计                                               │
│ 登录成功/失败日志 (zap)                                       │
│ Token 创建/撤销日志                                           │
│ 异常行为告警 (异地登录, 频繁失败)                               │
└─────────────────────────────────────────────────────────────┘
```

---

## 9. 实施状态

> **当前状态**: 混合架构已实施。Zitadel OIDC 集成、本地 PAT/SAT、Casbin RBAC、Session 管理等核心功能已完成。

| 阶段 | 内容 | 状态 |
|------|------|------|
| Phase 1 | 本地 fosite OAuth2 Provider + JWT | ✅ 完成 |
| Phase 2 | 登录限流 | ✅ 完成 |
| Phase 3 | CLI 登录 (Device Flow) | ✅ 完成 |
| Phase 4 | 前端登录页 + Auth Store | ✅ 完成 |
| Phase 5 | PAT/SAT + Casbin RBAC | ✅ 完成 |
| Phase 6 | Zitadel OIDC 混合集成 | ✅ 完成 |
| — | 本地 Session 管理 (HMAC) | ✅ 完成 |
| — | BearerTokenMiddleware 三路径 | ✅ 完成 |

---

## 10. 附录：技术选型依据

### 10.1 OAuth 2.0 库对比

| 维度 | ory/fosite | openshift/osin | go-authgate |
|------|------------|----------------|-------------|
| ⭐ Stars | 2,560 | 1,937 | 45 |
| Device Grant | ✅ 2025.02 合并 | ❌ | ✅ |
| PKCE | ✅ 内置 | ✅ 内置 | ✅ |
| JWT | ✅ 内置策略 | ❌ 需自实现 | ✅ |
| 生产验证 | Ory Hydra | OpenShift | 无 |
| **结论** | ✅ 选用 | ❌ 功能不足 | ❌ 太新 |

### 10.2 参考项目

| 项目 | 参考价值 |
|------|----------|
| [Gitea OAuth2 Provider](https://github.com/go-gitea/gitea) | 数据模型 (XORM)，JWT Token 设计 |
| [Ory Hydra](https://github.com/ory/hydra) | fosite 生产级 usage，Device Grant 实现 |
| [GitHub CLI (gh)](https://github.com/cli/cli) | Device Flow 客户端，Token 缓存 |
| [go-authgate](https://github.com/go-authgate/authgate) | Gin+GORM+fosite 同构参考 |
