# Mutong × Zitadel 认证改造设计文档

> 状态：Proposed  
> 日期：2026-06-01  
> 适用范围：Mutong Web、Mutong CLI、API Token、本地授权体系  
> 目标方向：GitLab / Harbor 型 —— 外部身份认证 + 本地业务授权

## 1. 背景

Mutong 当前认证体系以本地 `fosite` 为核心，承载了以下三类能力：

1. **身份认证能力**
   - Web OAuth2 / OIDC 登录
   - CLI Device Flow 登录
   - 本地密码登录
   - 会话与 refresh token

2. **产品级访问令牌能力**
   - PAT（`mtp_`）
   - SAT（`mts_`）
   - token 生命周期管理、吊销、最后使用时间追踪

3. **产品级授权能力**
   - Casbin RBAC
   - admin / operator / viewer 等本地角色
   - API 路径与动作级权限控制

当前问题不在于“能不能登录”，而在于职责边界混杂：

- `fosite` 同时承担了协议能力与产品认证入口
- 本地密码登录、前端登录态、CLI 登录、PAT/SAT、Casbin 混在一条链上
- 如果简单做“fosite → Zitadel 替换”，极易破坏 Mutong 已经成型的产品权限与 token 模型

因此本次改造不采用“全部迁入 Zitadel”的思路，而采用成熟平台常见模式：

> **Zitadel 管登录与身份，Mutong 管授权与产品令牌。**

---

## 2. 目标

本次改造目标如下：

1. 用 Zitadel 接管 **身份认证**
   - Web 登录 / SSO
   - OIDC / OAuth2 Provider
   - CLI Device Flow 登录
   - 会话 / MFA（如启用）

2. 保留 Mutong 本地的 **产品授权与 API Token 能力**
   - Casbin RBAC
   - PAT / SAT
   - 本地角色与资源权限
   - 产品级审计

3. 迁移过程中保证以下能力不回退
   - Web 登录与用户态初始化
   - CLI 登录与 token 刷新
   - PAT / SAT 继续可用
   - Casbin 继续作为最终授权判断点
   - `/api/auth/me` 等产品接口继续存在

---

## 3. 非目标

以下内容不在本次首期范围内：

1. 不将 Casbin 替换为 Zitadel 原生授权体系
2. 不在首期迁移中重做 PAT/SAT 语义
3. 不在首期引入复杂多租户权限系统
4. 不做“大爆炸式”切换，避免一次性删除 fosite、本地登录、CLI 登录链路

---

## 4. 目标架构

### 4.1 架构原则

Mutong 改造成如下职责边界：

#### Zitadel 负责
- 用户登录
- OIDC / OAuth2 Provider
- Web SSO
- CLI Device Flow
- 用户身份真实性与会话

#### Mutong 负责
- Casbin RBAC
- PAT / SAT 生命周期管理
- 本地角色 / 组织 / 资源权限模型
- Zitadel 身份到 Mutong 用户的映射
- 产品审计日志

### 4.2 一句话定义

> Zitadel 证明“你是谁”，Mutong 决定“你能做什么、你怎么调用我的 API”。

### 4.3 目标拓扑

```text
[Browser / mutongctl / API Client]
          |
          v
      [Zitadel]
   - Login / Session / MFA
   - OIDC / OAuth2
   - Device Flow
          |
          v
      [Mutong]
   - OIDC callback / user mapping
   - BearerTokenMiddleware
   - Casbin RBAC
   - PAT / SAT validation
   - Product APIs
```

---

## 5. 核心对象模型

### 5.1 Mutong User

`User` 不再承担“密码认证主表”职责，而改为 **业务侧用户镜像 + 授权承载体**。

建议保留或扩展字段：

- `id`
- `uuid`
- `zitadel_user_id`
- `username`
- `email`
- `display_name`
- `role`
- `status`
- `last_login_at`

#### 含义说明

- `zitadel_user_id`：外部身份主键（对应 OIDC `sub`）
- `uuid`：Mutong 内部稳定标识
- `role`：Mutong 产品角色，不由 Zitadel 直接裁决
- `status`：即使 Zitadel 用户存在，也可本地禁用访问 Mutong

### 5.2 角色模型

本地角色继续保留，例如：

- `admin`
- `operator`
- `viewer`

未来也可扩展：

- `auditor`
- `sre`
- `readonly-biz`
- `executor-approver`

角色来源由 Mutong 决定，可支持：

- 首次登录赋默认角色
- 管理后台手工调整
- 按 Zitadel groups / claims 做映射

### 5.3 PAT / SAT

PAT / SAT 保留为 Mutong 本地产品能力：

- PAT：用户级 API Token
- SAT：服务账号级 API Token

保留现有要素：

- `mtp_` / `mts_` 前缀
- hash 存库
- `expires_at`
- `revoked_at`
- `last_used_at`
- `scopes`

### 5.4 Service Account

建议逐步把服务账号从“token 附属字段”强化为独立实体：

- `service_account.id`
- `service_account.name`
- `service_account.description`
- `service_account.owner_user_id`
- `service_account.status`

SAT 绑定服务账号而不是普通用户。

---

## 6. 认证与授权流程

### 6.1 Web 登录流程

#### 流程说明

1. 前端点击登录
2. 进入 Mutong 登录入口，例如 `/api/auth/oidc/login`
3. Mutong 302 到 Zitadel authorize endpoint
4. Zitadel 完成密码、MFA、session、consent
5. 回调到 `/api/auth/oidc/callback`
6. Mutong 校验 `state` 与 `code`
7. Mutong 交换 token，解析 ID Token / UserInfo
8. 用 `sub` 建立或更新本地用户映射
9. 建立 Mutong 本地 session
10. 前端调用 `/api/auth/me` 获取本地用户上下文

#### 设计选择

推荐使用 **Mutong 本地 session cookie** 承载产品登录态，而不是让前端完全裸持有 Zitadel access token 作为主态。

理由：

- `/api/auth/me` 可继续稳定存在
- 前端侵入更小
- 更容易与 Casbin、本地用户上下文、审计统一

### 6.2 CLI 登录流程

`mutongctl auth login` 改走 Zitadel Device Flow：

1. CLI 发起 device authorization 请求
2. 获取 `device_code` / `user_code` / `verification_uri`
3. 用户在浏览器完成登录与授权
4. CLI 轮询 token endpoint
5. 保存 access token / refresh token 到本地缓存
6. CLI 调用 Mutong API 时携带 access token
7. Mutong 校验外部 token，并映射到本地用户上下文

CLI 用户体验保持尽量不变：

- `auth login`
- `auth status`
- `auth logout`
- 自动刷新
- 本地 token 缓存

### 6.3 BearerTokenMiddleware 改造

改造后建议分三路处理：

#### 路由 1：PAT / SAT
- 识别 `mtp_` / `mts_`
- 继续本地 DB hash 校验
- 注入本地用户或服务账号上下文

#### 路由 2：Zitadel access token
- 校验 JWT 或调用 introspection
- 验证 issuer / audience / expiry
- 提取 `sub`
- 查本地 `zitadel_user_id -> Mutong user`
- 注入本地 `user_id` / `uuid` / `role`

#### 路由 3：Mutong 本地 session
- 从 session 找本地用户
- 注入本地上下文

### 6.4 Casbin 接入原则

Casbin 不感知 Zitadel。Casbin 只认 Mutong 本地 subject。

因此授权链必须是：

1. BearerTokenMiddleware / SessionMiddleware
2. 本地用户映射
3. 注入 `user_id`、`role`
4. CasbinMiddleware
5. 业务 Handler

### 6.5 `/api/auth/me`

该接口保留，作为前端统一“当前用户信息”入口。

建议返回：

- `id`
- `uuid`
- `username`
- `email`
- `display_name`
- `role`
- `auth_provider`
- `token_type`

### 6.6 登出

#### Web
- 本地登出：清理 Mutong session
- 可选 OIDC 登出：跳转 Zitadel end-session endpoint

#### CLI
- 删除本地 token 缓存
- 可选调用 revoke endpoint

---

## 7. 迁移步骤

迁移遵循四条原则：

1. 不做大爆炸替换
2. 先接入，再切流，再下线
3. PAT/SAT 独立于 OIDC 迁移节奏
4. 先建立本地用户映射层

### Phase 0：准备阶段

目标：只准备，不影响现有用户路径。

工作内容：

- 增加 Zitadel 配置
- 扩展本地用户映射字段
- 增加 `auth.mode = local | hybrid | zitadel`
- 规划 auth 审计字段

### Phase 1：Web OIDC 登录接入

目标：先打通 Web SSO，新旧链路并存。

工作内容：

- 新增 `/api/auth/oidc/login`
- 新增 `/api/auth/oidc/callback`
- 建立本地用户映射
- 建立 Mutong 本地 session
- 前端增加 SSO 登录入口

验收：

- 新用户可通过 Zitadel 登录
- 老登录链路仍可用
- `/api/auth/me` 正常
- Casbin 正常

### Phase 2：BearerTokenMiddleware 收敛

目标：统一支持 Zitadel token、PAT/SAT、本地 session。

工作内容：

- 重构 middleware 为多分支验证器
- 增加 Zitadel token 校验
- 保留 `mtp_` / `mts_` 分支
- 统一上下文字段

验收：

- Web SSO 后所有 API 正常
- PAT/SAT 不受影响
- Casbin 正常工作

### Phase 3：CLI 切换到 Zitadel Device Flow

目标：让 `mutongctl auth login` 接入 Zitadel。

工作内容：

- 替换 device authorization endpoint
- 对齐 token 交换与刷新逻辑
- 尽量保持现有 token 缓存格式兼容

验收：

- login/status/logout/refresh 全部正常
- 现有 CLI 调用行为稳定

### Phase 4：下线本地主认证职责

目标：在 Web 与 CLI 稳定后，逐步移除本地主认证链路。

可下线内容：

- `/api/auth/login`（如果不保留 fallback）
- 本地登录页
- fosite 的主认证职责

仍保留：

- Casbin
- PAT/SAT
- `/api/auth/me`
- 本地用户映射
- 审计能力

### Phase 5：后续增强

- Zitadel groups -> Mutong role 映射
- 服务账号实体化
- PAT/SAT scope 增强
- SSO 单点登出
- 多组织 / 多租户权限模型

---

## 8. 风险与控制

### 风险 1：用户映射错乱

表现：同一 Zitadel 用户登录多次，生成多个本地用户。

控制：

- `zitadel_user_id` 唯一约束
- 首次登录建档流程做幂等

### 风险 2：角色缺失

表现：登录成功但 role 为空，Casbin 拒绝所有请求。

控制：

- 给默认最小权限角色
- `/api/auth/me` 明确返回 role

### 风险 3：CLI 中断

表现：CLI 登录切换后，旧缓存或脚本失效。

控制：

- CLI 与 Web 分阶段切
- 兼容旧缓存格式或提供迁移提示

### 风险 4：PAT/SAT 被误伤

表现：改造 BearerTokenMiddleware 时破坏 `mtp_` / `mts_` 分支。

控制：

- PAT/SAT 分支逻辑独立保留
- 不与 OIDC token 共用处理逻辑

### 风险 5：回滚困难

表现：新登录链路出问题时无法快速回退。

控制：

- feature flag
- 保留 hybrid 模式
- 支持快速切回 local 模式

---

## 9. 备选方案

### 方案 A：全部迁入 Zitadel

不推荐。原因：

- 会强行打散 Mutong 的本地授权模型
- PAT/SAT 语义难以完整保留
- Casbin 与产品角色边界会被破坏

### 方案 B：双栈长期并存

不推荐作为长期目标。原因：

- 复杂度长期过高
- 会形成长期双认证债务

### 当前推荐方案

> GitLab / Harbor 型：Zitadel 管身份，Mutong 管授权与产品令牌。

---

## 10. 决策结论

### ADR

- **Title**: 将 Mutong 认证体系重构为 Zitadel 身份认证 + 本地授权模型
- **Status**: Proposed
- **Decision**:
  - 采用 Zitadel 作为身份认证提供方
  - 保留 Mutong 本地 Casbin RBAC
  - 保留 Mutong 本地 PAT / SAT 产品令牌体系
  - 通过本地用户映射层桥接外部身份与本地授权
- **Consequences**:
  - Web 与 CLI 登录体验标准化
  - Mutong 继续保持完整产品权限控制能力
  - 迁移过程可灰度、可回滚
  - 首期需要额外实现用户映射与 middleware 收敛
