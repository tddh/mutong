# Mutong AIOps 智能搜索扩展方案

> **文档版本**: v1.1  
> **状态**: 已落地（Tavily + GitHub Issues 已上线；审计落库、状态还原、搜索词指纹提取尚未闭合，见 §0）  
> **最后更新**: 2026-11-12（对照代码核实，原提案日期 2026-06-09）  
> **目标读者**: 架构师、后端开发、运维团队

---

## 0. 落地状态速览

本节于 2026-11-12 对照代码核实，是本文其余章节的**现状校正入口**：下文 §4–§8 保留原始设计意图，凡与实现不一致处以本节为准。

| 能力 | 状态 | 实现位置 / 说明 |
|------|------|----------------|
| PII 脱敏 + 映射账本 | ✅ 已实现 | `services/diagnosis/sanitizer.go`：`Redact()` 生成 `[REDACTED_<RULE>_<n>]` 占位符并写入内存映射；默认规则 ip_v4 / email / jwt / secret（aws_key、ali_key 默认关闭）。账本为**进程级共享**（`main.go` 只建一个 `"main-app"` 实例），非会话级 |
| 高 PII 阻断 + 禁用词 | ✅ 已实现 | `ShouldBlock()`：按「含占位符的词 / 总词数」比例与 `highPIIBlockThreshold`（默认 0.5）比较；命中 `bannedTerms` 直接阻断 |
| Prompt Injection 入站过滤 | ⚠️ 部分 | `FilterResults()` 仅在 Tavily 路径调用；`handleSearchGitHubIssues` **未过滤**返回结果 |
| 搜索词指纹提取（§5.2） | ⚠️ 简化实现 | `ExtractSearchFingerprint()` 只按 `maxQueryLength`（默认 200）截断，未实现正则提取组件/版本/错误码；另有 `Genericize()` 把占位符转成 `<IP_V4>` 这类泛化词后再出站 |
| 状态还原 De-redaction（§5.4） | ❌ 未接线 | `Restore()` 已实现但**全仓无调用方**；诊断报告中的占位符当前不会被还原 |
| Tavily Web 搜索 | ✅ 已上线 | `services/search/tavily_client.go`，`search_knowledge_base` 工具；支持 `topic=general/news/security`，返回 AI 摘要 + 最多 `maxResults`（默认 3）条，每条正文截断 400 字符 |
| GitHub Issues 搜索 | ✅ 已上线 | `services/search/github_client.go`，`search_github_issues` 工具；`state` 支持 `open/closed/all`（默认 open），无 token 时受 60 次/小时限制 |
| Bing 备选引擎 | ❌ 未实现 | 代码中无 Bing 客户端与配置项 |
| 审计落库（§5.5） | ❌ 未接线 | `mcp.AuditLogFunc` 钩子已实现并在阻断/成功路径调用，但 `cmd/main.go` 传入 `nil`；无 `external_search_audit` 表，元数据仅存在于工具返回文本与服务端日志 |
| 配置开关与策略 | ✅ 已实现 | `config/sanitizer_config.go`（`ExternalSearchConfig` / `SanitizerConfig`）+ `configs/config.diagnosis.yaml` 的 `external_search` 与 `sanitizer` 段；`MUTONG_TAVILY_KEY` / `MUTONG_GITHUB_TOKEN` 覆盖 YAML |
| Prompt 策略（§8） | ⚠️ 位置不同 | 未新增 `configs/prompts/search_strategy.md`；外部搜索使用指引内联在 `controllers/diagnosis_controller.go` 的 system prompt 中 |
| 工具注册与可见性 | ✅ 已实现 | `external_search.enabled=false` 或对应客户端未就绪时，工具不注册（`WithExternalSearch`），对 LLM 不可见 |
| 独立 `search.Client` 接口 | ❌ 未创建 | 两个客户端各自独立，未抽象公共接口；配置结构体落在 `config/sanitizer_config.go` 而非 `config_base.go` |

**已知配置缺陷（需代码/配置侧修复，本文不改代码）**：`config.SanitizerConfig` 的 yaml 标签是 camelCase（`highPIIBlockThreshold`、`maxQueryLength`、`bannedTerms`、`promptInjectionPatterns`），而 `configs/config.diagnosis.yaml` 与 `.example` 写的是 snake_case（`high_pii_block_threshold` 等）。yaml.v3 按标签精确匹配，这些键**不会绑定**：`bannedTerms` 与注入过滤模式实际为空；又因 `rules` 能绑定（非空），`cmd/main.go` 不会回落到 `DefaultSanitizerConfig()`，阈值与长度改由 `NewSanitizer()` 兜底为 0.5 / 200。修复方向：把 YAML 键改为 camelCase，或给结构体补 snake_case 标签。

---

## 目录

0. [落地状态速览](#0-落地状态速览)
1. [背景与动机](#1-背景与动机)
2. [社区前沿实践调研](#2-社区前沿实践调研)
3. [目标与范围](#3-目标与范围)
4. [架构设计](#4-架构设计)
5. [安全层设计 (核心)](#5-安全层设计核心)
6. [MCP 工具扩展](#6-mcp-工具扩展)
7. [配置升级](#7-配置升级)
8. [Prompt 策略升级](#8-prompt-策略升级)
9. [实施路线图](#9-实施路线图)
10. [风险评估与缓解](#10-风险评估与缓解)
11. [验收标准](#11-验收标准)
12. [参考资料](#12-参考资料)

---

## 1. 背景与动机

### 1.1 当前架构局限性

Mutong 当前的 AI 诊断引擎采用单 Agent + 20 个 MCP 工具架构（**撰写时基线**；截至 2026-11-12 已扩展为 29 个工具 = 22 诊断 + 7 执行类，其中含本方案新增的 2 个外部搜索工具），所有工具均面向**集群内部资源**（拓扑查询、日志检索、指标查询等）。这种设计存在以下局限：

| 局限 | 影响 | 示例 |
|------|------|------|
| **知识截止** | LLM 训练数据截止前的最新 CVE/Bug 无法识别 | K8s v1.32 新发布的 CNI 漏洞 |
| **社区隔离** | 无法获取 GitHub Issues 中的已知 Bug | Istio Proxy 特定版本的 segfault |
| **文档时效** | 无法确认 API/CRD 的最新变更 | K8s 废弃旧版 Ingress API |
| **根因盲区** | 第三方组件（Ceph/Kafka/MySQL）的新问题无外部参考 | Ceph OSD 崩溃 exit code 132 |

### 1.2 核心需求

引入**受控的外部知识检索能力**，使 Agent 能够在内部排查无果时，安全地搜索社区、官方文档和开源代码仓库，获取最新技术情报。

### 1.3 设计原则

1. **安全第一**: 任何离开集群的数据必须经过脱敏
2. **按需触发**: 默认关闭，仅在 Agent 判定必要时触发
3. **可观测**: 所有外部调用可审计、可追溯
4. **渐进式**: 分阶段实施，不破坏现有功能

---

## 2. 社区前沿实践调研

### 2.1 隐私保护模式

| 方案 | 维护者 | 核心理念 | 适用场景 |
|------|--------|---------|---------|
| **llm-search-mediator** | SecAI-Hub (开源) | PII 剥离 + decoy 查询 + 审计日志链 + prompt injection 过滤 | 自建 SearXNG 桥接，最高隐私等级 |
| **Microsoft Presidio** | Microsoft | 40+ PII 类型识别与替换，支持自定义 recognizer | 企业级通用脱敏 |
| **Kong/Gravitee AI Gateway** | Kong/Gravitee | 在 API Gateway 层集中管控 LLM 流量，全局 PII 策略 | 多 AI 应用统一治理 |
| **AIOpsShield** | RSA Conference 2025 | 针对 AIOps 的遥测数据操纵攻击防御，通过 taint analysis 识别并屏蔽遥测中的不可信输入 | 防御 telemetry injection |
| **Snowflake AI_REDACT** | Snowflake | 在查询离开可信边界前用 AI_REDACT 函数脱敏 | 数据仓库 Agent 场景 |

### 2.2 搜索引擎对比 (Agent-First)

| 搜索引擎            | 返回形式      | 中文覆盖  | 国内可访问  | 免费额度   | AIOps 适配度              |
| --------------- | --------- | ----- | ------ | ------ | ---------------------- |
| **Tavily** ⭐ 推荐 | 网页正文文本    | ✅ 好   | ✅ 通常可用 | 1K 次/月 | 专为 Agent 设计，返回正文无需二次爬取 |
| **Exa**         | 语义匹配结果+摘要 | ⚠️ 偏弱 | ✅ 可用   | 1K 次/月 | 语义检索最强，但贵              |
| **Brave**       | 链接+片段     | ✅ 不错  | ❌ 被墙   | 2K 次/月 | 独立索引，隐私最强，但国内不可用       |
| **Bing API**    | 链接+片段     | ✅ 好   | ✅ 可用   | 1K 次/月 | 传统搜索引擎，需配合爬取           |

**结论**: 首选 Tavily（返回正文，适合 Agent 直接消费），备选 Bing API（更可控但需二次解析）。

### 2.3 关键安全洞察

根据 RSA 2025 论文 **《When AIOps Become "AI Oops"》**，LLM 驱动的 AIOps 面临**遥测数据操纵攻击**（AIOpsDoom）：攻击者可通过构造恶意请求，将 adversarial payload 注入日志/指标，误导 Agent 作出错误判断。

**对我们方案的启示**:
- 外部搜索结果可能包含 prompt injection 攻击，需在返回给 Agent 前过滤
- 遥测数据（日志/指标）在发给 Agent 前应识别并屏蔽不可信输入

---

## 3. 目标与范围

### 3.1 In Scope

| 序号 | 功能 | 说明 |
|------|------|------|
| 1 | 安全脱敏网关 (`Sanitizer`) | 拦截告警/日志中的 PII，替换为占位符 |
| 2 | Web 搜索 MCP Tool (`search_knowledge_base`) | 接入 Tavily（已上线）；Bing 备选未实现 |
| 3 | GitHub Issues MCP Tool (`search_github_issues`) | 搜索已知 Bug 和修复方案 |
| 4 | Prompt Injection 过滤 | 过滤搜索结果中的恶意指令 |
| 5 | 审计日志 | 记录所有外部 API 调用的元数据 |
| 6 | 配置开关与策略管理 | 通过 YAML 控制搜索行为和安全策略 |

### 3.2 Out of Scope (后续迭代)

| 序号 | 功能 | 原因 |
|------|------|------|
| - | SearXNG 自建部署 | 当前阶段 Tavily API 足够，后续如需隐私增强可切换 |
| - | Agent 多模式协作 | 当前单 Agent 架构满足需求，不急重构 |
| - | 本地 RAG 知识库扩展 | 已有 pgvector + NebulaGraph 混合检索，暂不扩展 |

---

## 4. 架构设计

### 4.1 整体架构

```
                    ┌─────────────────────────────────┐
                    │        告警/日志 输入            │
                    └──────────────┬──────────────────┘
                                   │
                    ┌──────────────▼──────────────────┐
                    │      Context Collector           │
                    │  (现有：并行采集拓扑/指标/日志)     │
                    └──────────────┬──────────────────┘
                                   │ diagnosis_context
                    ┌──────────────▼──────────────────┐
                    │  Eino Diagnosis Agent           │
                    │  (ReAct 循环, max 10 steps)      │
                    │  • 自主决定何时调用外部工具        │
                    │  • 受 Prompt 策略约束             │
                    └──┬───────────────────────────┬───┘
                       │                           │
              ┌────────▼──────┐          ┌────────▼───────────┐
              │ 内部 MCP Tools│          │ 外部 MCP Tools (新)│
              │ (20 个原有工具)│          │                    │
              │ • query_...   │          │ • search_knowledge_│
              │ • get_pod_... │          │   base (Tavily)   │
              │ • ...         │          │ • search_github_   │
              └────────┬──────┘          │   issues (GitHub)  │
                       │                 └──┬──────────────┬─┘
                       │                    │              │
                       │           ┌───────▼──┐   ┌───────▼─────┐
                       │           │ 🔒 脱敏  │   │ 🔒 脱敏     │
                       │           │ & 提取   │   │ & 提取      │
                       │           └───────┬──┘   └───────┬────┘
                       │                   │              │
                       │              ┌────▼────┐   ┌────▼────┐
                       │              │ Tavily  │   │ GitHub  │
                       │              │ API     │   │ API     │
                       │              └────┬────┘   └────┬────┘
                       │                   │              │
                       │           ┌───────▼──────────────▼──┐
                        │           │ 🔒 Prompt Injection 过滤 │
                        │           └───────┬─────────────────┘
                        │                   │ filtered_results (含占位符)
                        │           ┌───────▼─────────────────┐
                        │           │ 🔑 存储：脱敏映射账本     │
                        │           │ (仅内存，不外发)         │
                        │           └─────────────────────────┘
                        └───────────────────┘
                                    │ 含占位符的 Agent 报告
                                    │  + [映射账本] (内存)
                        ┌───────────▼──────────────────────┐
                        │ 🔑 展示层/执行层 (De-redaction)  │
                        │ • 还原报告 IP 供用户查看           │
                        │ • 根据 ResourceName 执行自动化修复 │
                        └──────────────────────────────────┘
                    ┌──────────────▼──────────────────┐
                    │  诊断报告 → PostgreSQL 持久化     │
                    └─────────────────────────────────┘
```

### 4.2 数据流

> 第 5 步为简化实现（截断 + 泛化），第 10 步（De-redact）与第 11 步（审计落库）**尚未接线**，详见 §0 / §5.2 / §5.4 / §5.5。

```
1. 告警触发 ─→ [Context Collector] 并行采集拓扑/指标/日志 (现有逻辑不变)
2. Agent 开始内部工具排查 (query_topology → get_pod_logs → get_resource_metrics)
3. [IF] 置信度不足 ─→ Agent 自主决定调用外部搜索工具
4. 🔒 [Sanitizer::PII Redaction] 脱敏搜索词中的敏感信息
5. 🔒 [Sanitizer::QueryExtractor] 提取搜索关键词 (仅技术信息，丢弃内部架构细节)
6. 🔒 [Sanitizer::High-PII Check] 如果 PII 占比过高则阻止搜索并返回安全提示
7. [Tavily/Bing] 返回社区搜索结果
8. 🔒 [Sanitizer::ResultFilter] 过滤搜索结果中的 prompt injection 模式
9. Agent 结合内外部信息生成最终诊断 (结果含脱敏占位符)
10. 🔑 [Sanitizer::De-redact] 在展示/执行层，通过内存账本将占位符还原
11. [Audit Log] 记录外部调用的元数据到 PostgreSQL
```

### 4.3 代码结构

> **落地差异**：`services/search/client.go`（公共接口）未创建；配置结构体实际落在 `config/sanitizer_config.go`（不是 `config_base.go`）；`sanitizer` 与 `external_search` 是 `configs/config.diagnosis.yaml` 的两个**平级顶层段**，不是嵌套的 `security` 子段。

```
mutong/
├── services/
│   ├── diagnosis/
│   │   ├── sanitizer.go           ← 新增：安全脱敏网关
│   │   ├── sanitizer_test.go      ← 新增：脱敏单元测试
│   │   ├── context_collector.go   ← 现有：不变
│   │   └── eino_agent.go          ← 现有：不变
│   ├── mcp/
│   │   ├── server.go              ← 修改：registerTools() 中注册新工具
│   │   └── search_tools.go        ← 新增：搜索工具 handler
│   └── search/
│       ├── client.go              ← 新增：搜索客户端接口定义
│       ├── tavily_client.go       ← 新增：Tavily API 封装
│       └── github_client.go       ← 新增：GitHub API 封装
├── config/
│   └── config_base.go             ← 修改：新增 ExternalSearch, TavilyConfig, GitHubConfig 结构体
├── configs/
│   └── config.diagnosis.yaml.example ← 修改：新增 external_search 配置段
└── docs/
    └── external-search-plan.md    ← 本文档
```

---

## 5. 安全层设计 (核心)

安全层是本方案的核心，参考社区最佳实践 (Presidio / llm-search-mediator / AIOpsShield)，设计四层防线。

### 5.1 Layer 1: PII 脱敏 (Outbound Sanitization)

**目标**: 确保任何离开集群的数据不包含敏感信息。

**有状态映射机制 (Stateful Mapping - "The Ledger")**:
为了在展示层还原信息，**脱敏不是简单的替换，而是建立映射关系** (类似 Pseudonymization 假名化)。
系统需在内存中维护一个会话级的映射表：
*   `[REDACTED_IP_V4_1]` <-> `10.168.0.55`
*   `[REDACTED_JWT_2]` <-> `eyJhb...`

> **实现差异**：`cmd/main.go` 只创建一个 `NewSanitizer("main-app", …)` 实例并交给 `SetExternalSearch`，因此映射表实际是**进程级共享**（跨用户、跨诊断会话累积），而非原方案设想的「会话级、会话结束即销毁」。序号按规则名各自递增，进程重启后清空。

**脱敏规则 (Go 正则匹配)** — 下表已按 `services/diagnosis/sanitizer.go` / `config/sanitizer_config.go` 的实现校正（占位符统一为 `[REDACTED_<规则名大写>_<序号>]`，序号按规则各自递增）:

| PII 类型 | 匹配模式（规则名） | 替换结果 | 默认状态 |
|----------|---------|---------|--------|
| 内部 IP 地址 | `10.x.x.x` / `172.16-31.x.x` / `192.168.x.x`（`ip_v4`） | `[REDACTED_IP_V4_1]`（写入映射表） | 启用 |
| 邮箱地址 | `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`（`email`） | `[REDACTED_EMAIL_1]` | 启用 |
| JWT/Bearer Token | `eyJ[A-Za-z0-9_-]{20,}`（`jwt`） | `[REDACTED_JWT_1]` | 启用 |
| 密码字段 | `(?i)(password\|passwd\|secret)\s*[:=]\s*\S+`（`secret`） | `[REDACTED_SECRET_1]`（**整段替换**，不保留原方案的 `$1:` 前缀） | 启用 |
| AWS Access Key | `(AKIA\|ABIA\|ACCA\|ASIA)[A-Za-z0-9]{16,}`（`aws_key`） | `[REDACTED_AWS_KEY_1]` | 关闭（按需开启） |
| 阿里云 Access Key | `(LTAI)[A-Za-z0-9]{12,}`（`ali_key`） | `[REDACTED_ALI_KEY_1]` | 关闭（按需开启） |
| Kubernetes UID | 原方案设想 `[0-9a-f]{8}-…-[0-9a-f]{12}` | — | **未实现**（无对应规则） |

**保留的字段 (不脱敏)**:
- Pod 名 (如 `payment-service-5d8f9c-xk2p9`) — 有助于问题定位且不含 PII
- Namespace 名 (如 `kube-system`, `production`) — 技术上下文
- 容器名/镜像名 (如 `nginx:1.25`, `redis:7`) — 技术信息
- 错误码/堆栈帧 (如 `SIGSEGV`, `panic: runtime error`) — 核心诊断信息

**高 PII 阻断阈值**: 如果一段文本中超过 50% 的内容被脱敏，则**完全阻止发送**，因为此时搜索价值极低且风险极高。

### 5.2 Layer 2: 搜索词提取器 (Query Extractor)

> **实现现状（2026-11-12）**：`ExtractSearchFingerprint()` 目前只做长度截断（`maxQueryLength`，默认 200，超出追加 `...`），下文的正则提取策略**尚未实现**；出站前另经 `Genericize()` 把 `[REDACTED_IP_V4_1]` 之类占位符转成 `<IP_V4>` 泛化词，避免把会话编号带给搜索引擎。

脱敏后的日志可能仍有数千字符，搜索引擎无法处理。需要提取**"技术搜索指纹"**。

**提取规则**:

```go
// 保留: 组件 + 版本 + 错误码/关键帧
// 丢弃: 时间戳、Trace ID、随机 UUID、业务 Payload

// Good 搜索词:
"kubernetes 1.30 cgroup v2 OOM cgroupfs driver bug"
"istio envoy proxy SIGSEGV segfault github issue"
"ceph osd crush map update failed exit code 132"

// Bad 搜索词 (会被拒绝):
"payment-service pod crashed at 2025-06-09T12:30:45Z namespace production container payment-api"
// → 太具体，可能包含内部架构信息
```

**实现策略**:
1. 使用正则提取错误模式 (`panic:.*`, `error:.*`, `failed to.*`, `SIG.*`)
2. 提取组件名和版本号 (`v\d+\.\d+\.\d+`, `kubernetes \d+\.\d+`, `istio.*`)
3. 组合成不超过 128 字符的搜索词
4. 如果无法提取，返回原脱敏文本的前 80 字符

### 5.3 Layer 3: Prompt Injection 过滤 (Inbound Filtering)

**目标**: 防止外部搜索结果中的恶意指令影响 Agent 决策。

参考 `llm-search-mediator` 的实现，检测以下模式：

| 模式类型 | 示例 | 处置 |
|---------|------|------|
| 指令覆盖 | "ignore previous instructions" | 删除该结果 |
| 角色劫持 | "you are now a different assistant" | 删除该结果 |
| 代码注入 | `<script>`, `javascript:` | 删除该结果 |
| 敏感重定向 | "visit this URL for more info" | 删除该结果 |

**实现**: 通过正则模式匹配搜索结果内容，匹配到的结果直接丢弃，不参与 Agent 上下文。

### 5.4 Layer 3.5: 状态还原机制 (De-redaction) 🔑 核心

> **实现现状（2026-11-12）**：`Sanitizer.Restore()` 已实现（按内存映射表逐个替换回原值），但**全仓无调用方**——展示层与 API 响应层尚未接入。另需注意：当前脱敏只作用于**出站搜索词**（`services/mcp/search_tools.go`），诊断结果本身未被脱敏，因此执行器定位资源不受影响，报告里也不会出现占位符。

**目标**: 确保用户能看懂报告，系统能执行修复。

Agent 返回的诊断报告中包含的是占位符 (如 `[REDACTED_IP_1]`)。**在系统展示给用户或执行修复 (Executor) 之前，必须利用内存中的"映射账本"进行还原。**

*   **输入 (Agent Report)**: "根因分析：节点 `[REDACTED_IP_1]` 存在内核 Bug。"
*   **查账 (Lookup)**: 检索 Sanitizer 会话映射 -> `[REDACTED_IP_1]` = `10.168.0.55`
*   **输出 (User/Executor View)**: "根因分析：节点 `10.168.0.55` 存在内核 Bug。"

**注意**:
1.  **展示层还原**: 前端或 API 响应层调用 `Restore()` 函数，将报告中的占位符替换回真实 IP。
2.  **Executor 逻辑**: 修复系统不需要 IP，它直接利用 `DiagnosisResult` 结构体中**未脱敏的** `ResourceName/UID` 来定位目标节点。
3.  **生命周期**: 映射表（账本）仅在诊断会话期间存活 (内存)，会话结束后销毁或归档（仅存哈希用于审计）。

---

### 5.5 Layer 4: 审计日志 (Audit Trail)

> **实现现状（2026-11-12）**：审计**钩子**已实现——`mcp.AuditLogFunc` 在阻断与成功两条路径上被调用，字段与下文 JSON 基本对应（toolName / engine / query（脱敏后）/ redactions / returned / blocked / reason）。但 `cmd/main.go` 调用 `SetExternalSearch(..., nil)` 传入空钩子，且不存在 `external_search_audit` 表与保留期清理，因此**元数据未落库**。hash-chained 设计亦未实现。

参考 `llm-search-mediator` 的 hash-chained 审计日志设计：

```json
{
  "timestamp": "2026-06-09T12:30:45Z",
  "alert_fingerprint": "abc123...",
  "sanitized_query": "kubernetes 1.30 CNI plugin [REDACTED_IP] bug",
  "redactions_count": 3,
  "tool_name": "search_knowledge_base",
  "engine": "tavily",
  "results_returned": 3,
  "blocked": false,
  "blocking_reason": null
}
```

**不记录的内容**:
- 原始未脱敏的查询 (只记录脱敏后版本)
- 搜索结果具体内容 (只记录数量)
- Agent 的最终诊断结论 (已在现有审计中记录)

审计日志写入 PostgreSQL 专用表 `external_search_audit`，保留 90 天。

---

## 6. MCP 工具扩展

> **实现现状（2026-11-12）**：两个工具均已上线，但不是下文设想的独立 Eino Tool 结构体，而是 `services/mcp/search_tools.go` 中注册到 `Server.tools` 的 handler（`WithExternalSearch` 注入客户端与 sanitizer），再由 `ConvertToEinoTools` 统一转成 Eino 工具。实际方法名是 `Redact()`（非 `SanitizeQuery()`），`ShouldBlock(text, count)` 需要传入脱敏计数。下文 Go 代码保留为设计示意。

### 6.1 Tool 1: `search_knowledge_base`

| 属性 | 值 |
|------|---|
| 名称 | `search_knowledge_base` |
| 描述 | 搜索外部技术社区获取实时信息（CVE、新 Bug、官方公告）。结果经过 PII 脱敏和 Prompt Injection 过滤。 |
| 参数 | `query` (required, string): 技术搜索词 |
| 参数 | `topic` (optional, string): 搜索范围，**实现为 `general` / `news` / `security`**（非法值回落 `general`），默认 `general`。原方案设想的 `kubernetes` 未实现。 |
| 返回 | 格式化文本：脱敏后的查询回显 + Tavily AI 摘要 + 最多 `maxResults`（默认 3）条结果，每条含标题、来源 URL、正文（截断 400 字符） |

**Eino Tool 实现**:

```go
// services/mcp/search_tools.go

type KnowledgeBaseSearchTool struct {
    searchClient search.Client
    sanitizer    *diagnosis.Sanitizer
    logger       interfaces.Logger
}

func (t *KnowledgeBaseSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return &schema.ToolInfo{
        Name: "search_knowledge_base",
        Desc: "搜索外部技术社区获取实时信息（CVE、新 Bug、官方公告）。仅在内部知识库无法解决时使用。",
        ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
            "query": {
                Type:     schema.String,
                Desc:     "技术搜索词，应聚焦错误模式和组件版本",
                Required: true,
            },
            "topic": {
                Type: schema.String,
                Desc: "搜索范围：general, security, kubernetes",
                Enum: []string{"general", "security", "kubernetes"},
            },
        }),
    }, nil
}

func (t *KnowledgeBaseSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
    var args struct {
        Query string `json:"query"`
        Topic string `json:"topic"`
    }
    if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
        return "", fmt.Errorf("parse arguments: %w", err)
    }

    // 1. 脱敏搜索词
    sanitizedQuery, redactionCount := t.sanitizer.SanitizeQuery(args.Query)
    if t.sanitizer.ShouldBlock(sanitizedQuery) {
        t.logger.Warn("Search query blocked due to high PII ratio",
            zap.Int("redactions", redactionCount))
        return "[搜索结果被阻止: 查询包含过多敏感信息]", nil
    }

    // 2. 提取搜索指纹 (精简搜索词)
    searchFingerprint := t.sanitizer.ExtractSearchFingerprint(sanitizedQuery)

    // 3. 调用搜索引擎
    results, err := t.searchClient.Search(ctx, searchFingerprint, 3)
    if err != nil {
        return fmt.Sprintf("[搜索失败: %v]", err), nil
    }

    // 4. 过滤 Prompt Injection
    filteredResults := t.sanitizer.FilterResults(results)

    // 5. 格式化返回
    return formatSearchResults(filteredResults, searchFingerprint), nil
}
```

### 6.2 Tool 2: `search_github_issues`

| 属性 | 值 |
|------|---|
| 名称 | `search_github_issues` |
| 描述 | 在 GitHub 上搜索 Issues，查找已知 Bug 和修复方案。**实现强制附加 `is:issue`（Fine-grained PAT 必需），因此不含 Pull Requests**；按 `updated` 倒序取前 N 条。 |
| 参数 | `query` (required, string): 错误信息或堆栈关键字 |
| 参数 | `repo` (optional, string): 限制搜索仓库，如 `kubernetes/kubernetes` |
| 参数 | `state` (optional, string): Issue 状态，**实现支持 `open` / `closed` / `all`**，默认 `open` |
| 返回 | 最多 `maxResults`（默认 3）个 Issue 的编号、标题、状态、创建时间、标签和 URL；**当前未对返回结果做 Prompt Injection 过滤** |

**Eino Tool 实现**:

```go
type GitHubIssuesSearchTool struct {
    githubClient *search.GitHubClient
    sanitizer    *diagnosis.Sanitizer
    logger       interfaces.Logger
}

func (t *GitHubIssuesSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
    return &schema.ToolInfo{
        Name: "search_github_issues",
        Desc: "在 GitHub 上搜索 Issues 和 Pull Requests，查找已知 Bug 和修复方案。",
        ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
            "query": {
                Type:     schema.String,
                Desc:     "错误信息或堆栈关键字",
                Required: true,
            },
            "repo": {
                Type: schema.String,
                Desc: "限制搜索仓库，如 kubernetes/kubernetes",
            },
            "state": {
                Type:     schema.String,
                Desc:     "Issue 状态：open 或 closed",
                Enum:     []string{"open", "closed"},
            },
        }),
    }, nil
}

func (t *GitHubIssuesSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
    var args struct {
        Query string `json:"query"`
        Repo  string `json:"repo"`
        State string `json:"state"`
    }
    if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
        return "", fmt.Errorf("parse arguments: %w", err)
    }

    if args.State == "" {
        args.State = "open"
    }

    // 脱敏后搜索
    sanitizedQuery, _ := t.sanitizer.SanitizeQuery(args.Query)
    searchFingerprint := t.sanitizer.ExtractSearchFingerprint(sanitizedQuery)

    // 调用 GitHub API
    issues, err := t.githubClient.SearchIssues(ctx, searchFingerprint, args.Repo, args.State)
    if err != nil {
        return fmt.Sprintf("[GitHub 搜索失败: %v]", err), nil
    }

    return formatGitHubResults(issues), nil
}
```

---

## 7. 配置升级

### 7.1 `config.diagnosis.yaml` 实际配置段（已落地）

下面是 `configs/config.diagnosis.yaml` / `.example` 中**真实存在**的两个平级顶层段，键名以 `config/sanitizer_config.go` 的 yaml 标签为准：

```yaml
# 外部知识库搜索（ExternalSearchConfig）
external_search:
  enabled: true                  # 代码默认 false；随包 YAML 为 true，但无 API Key 时 Tavily 工具不注册
  auditEnabled: true             # 字段存在，但审计钩子未接线（见 §5.5），当前不产生落库记录
  tavily:
    apiKey: ""                   # 由 MUTONG_TAVILY_KEY 覆盖，勿写明文
    endpoint: "https://api.tavily.com/search"
    timeoutSeconds: 15
    maxResults: 3
  github:
    token: ""                    # 由 MUTONG_GITHUB_TOKEN 覆盖；无 token 时 60 次/小时
    endpoint: "https://api.github.com"
    timeoutSeconds: 10
    maxResults: 3

# 安全脱敏（SanitizerConfig，独立顶层段，不是 external_search.security）
sanitizer:
  enabled: true
  highPIIBlockThreshold: 0.5     # 注意：随包 YAML 目前误写为 high_pii_block_threshold
  maxQueryLength: 200            # 注意：随包 YAML 目前误写为 max_query_length
  bannedTerms:                   # 注意：随包 YAML 目前误写为 banned_terms
    - "internal.corp.net"
    - "kubeconfig"
  promptInjectionPatterns:       # 注意：随包 YAML 目前误写为 prompt_injection_patterns
    - "ignore previous"
    - "you are now"
    - "system prompt"
    - "<script>"
    - "javascript:"
  rules:                         # 单单词键名，能正常绑定
    - name: "ip_v4"
      pattern: '\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3})\b'
      enabled: true
    - name: "email"
      pattern: '[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}'
      enabled: true
    - name: "jwt"
      pattern: 'eyJ[A-Za-z0-9_-]{20,}'
      enabled: true
    - name: "secret"
      pattern: '(?i)(password|passwd|secret)\s*[:=]\s*\S+'
      enabled: true
    # 云厂商 AK 规则默认关闭，按需开启：aws_key / ali_key
```

**与原方案的差异**：

- 键名统一为 camelCase（原方案写 snake_case）；`sanitizer` 提升为独立顶层段，原 `external_search.security` 子段不存在。
- 无 `bing` 段、无 `audit_retention_days`、无 `rate_limit_per_minute`、无 `strict_sanitization` 开关。脱敏事实上不可关闭：`WithExternalSearch` 要求 sanitizer 非 nil 才注册工具，而 `sanitizer.enabled` 字段虽被读入配置却**未被 `NewSanitizer` / handler 使用**，置 false 不会跳过脱敏。
- YAML **不支持 `${VAR}` 插值**：密钥通过 `MUTONG_TAVILY_KEY` / `MUTONG_GITHUB_TOKEN` 在 `config.SetDefaultConfig()` 中覆盖，写 `${...}` 会被当成字面量。
- `sanitizer.rules` 为空时 `cmd/main.go` 回落 `diagnosis.DefaultSanitizerConfig()`；非空则完全以 YAML 为准（这也是 §0 所述键名缺陷不会自动兜底的原因）。

### 7.2 环境变量

```bash
# 搜索 API Keys（通过环境变量注入，勿写入配置文件）
export MUTONG_TAVILY_KEY="tvly-xxxxxxxx"          # Tavily API Key；未设置则 search_knowledge_base 不注册
export MUTONG_GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxx" # GitHub PAT；未设置仍可搜索，限 60 次/小时
# MUTONG_BING_KEY 未实现（无 Bing 引擎）
```

---

## 8. Prompt 策略升级

> **实现现状（2026-11-12）**：未新增 `configs/prompts/search_strategy.md`（该目录现有 8 个文件：`diagnosis_{default,pod,node,deployment,service,ingress,pvc}.md` 与 `postmortem.md`）。外部搜索的使用指引内联在 `controllers/diagnosis_controller.go` 的 system prompt 中（"外部搜索：search_knowledge_base（Tavily 全网搜索…）和 search_github_issues（GitHub Issues 搜索…）。遇到陌生错误、不熟悉的组件、需要参考外部资料时主动使用"），工具自身的 Description 也承担了「何时用/何时不用」的约束。下文策略文本保留为设计参考。

在 `configs/prompts/` 目录中新增 `search_strategy.md` 文件，注入 Agent 的 System Prompt：

```markdown
## External Search Strategy

### Overview
You have access to two external search tools:
1. `search_knowledge_base` - Searches the technical web community for real-time information (CVEs, new bugs, official documentation).
2. `search_github_issues` - Searches GitHub Issues and PRs for known bugs and fix patterns.

### When to Use
Use these tools ONLY WHEN internal investigation is insufficient:
- Internal logs, metrics, and topology do not reveal the root cause
- The error pattern is unfamiliar (not in your training data)
- A specific component version is involved (e.g., Kubernetes v1.32, Ceph v18.2.0)
- A CVE ID or security advisory is mentioned

### When NOT to Use
- For generic errors like "connection refused" without context (internal logs should suffice)
- If the root cause is already clear from internal tools
- If the query would contain sensitive internal information (IPs, tokens, user data)

### Search Query Guidelines
Craft specific and focused queries:
- ❌ Bad: "pod is crashing"
- ✅ Good: "kubernetes 1.30 cgroup v2 OOMKilled cgroupfs driver bug"
- ❌ Bad: "istio error"
- ✅ Good: "istio-proxy envoy segfault SIGSEGV github issue"

### Result Verification
- Cross-reference multiple sources when possible
- Prioritize official documentation and GitHub Issues over community blogs
- If search results are conflicting, state the conflict in your diagnosis
- Always cite the source URL when referencing external information

### Privacy Compliance
- The search tools automatically handle data redaction. DO NOT try to mask data yourself.
- NEVER include internal IP addresses, domain names, or credentials in your search queries.
```

---

## 9. 实施路线图

> 下表「状态」列为 2026-11-12 对照代码核实的结果：✅ 已落地 / ⚠️ 部分或简化 / ❌ 未实现。原排期（第 N 周）保留为历史计划，不代表实际工时。

### Phase 1: 安全底座 (第 1-2 周) 🔴 必须 — ⚠️ 大部分完成

| 序号 | 任务 | 产出 | 状态 |
|------|------|------|------|
| 1.1 | 编写 `diagnosis/sanitizer.go` | PII 脱敏函数 + 单元测试 | ✅ `sanitizer.go` + `sanitizer_test.go`（197 行）；覆盖率未测量 |
| 1.2 | 编写 Query Extractor | 搜索词精简逻辑 | ⚠️ 仅长度截断 + `Genericize()` 泛化，正则指纹提取未实现 |
| 1.3 | 编写 Prompt Injection 过滤器 | inbound 过滤逻辑 | ⚠️ `FilterResults()` 已实现，但只用于 Tavily 路径；且随包 YAML 键名不匹配导致模式列表为空（§0） |
| 1.4 | 新增配置结构体 `SearchConfig` | 配置加载逻辑 | ✅ 实际命名为 `ExternalSearchConfig` / `SanitizerConfig`（`config/sanitizer_config.go`） |
| 1.5 | 运行历史告警回归测试 | 确认脱敏后无敏感信息残留 | ❌ 未见回归测试记录 |

**验收标准**:
- 脱敏单元测试覆盖率 > 95%（未测量）
- 历史 100 条告警日志经脱敏后无 IP/Token/密码残留（未执行）
- 高 PII 阻断功能正常工作（✅ `ShouldBlock()` 已实现并有单测）

### Phase 2: Web 搜索集成 (第 3-4 周) 🟡 高优 — ⚠️ 功能上线，审计未闭合

| 序号 | 任务 | 产出 | 状态 |
|------|------|------|------|
| 2.1 | 编写 `search/tavily_client.go` | Tavily API 封装 | ✅ 含超时、`maxResults`、AI 摘要、topic |
| 2.2 | 编写 `mcp/search_tools.go` | `search_knowledge_base` 工具注册 | ✅ `WithExternalSearch` 条件注册 |
| 2.3 | 升级 System Prompt | 搜索策略指引 | ⚠️ 内联在 `controllers/diagnosis_controller.go`，未落地独立 prompt 文件 |
| 2.4 | 集成审计日志 | 外部调用元数据记录 | ❌ 钩子已留，`main.go` 传 nil，无落库 |
| 2.5 | 端到端测试 (模拟告警→诊断→搜索) | 完整流程验证 | ❌ `services/search/` 与 `mcp/search_tools.go` 均无专测 |

**验收标准**:
- Agent 可在置信度不足时自动调用搜索工具（✅ 工具全量注册，由 LLM 自主决定）
- 搜索结果正确注入诊断上下文（✅ 工具返回文本进入 ReAct 循环）
- 审计日志正确写入数据库（❌ 未接线）
- 脱敏层在每次搜索前生效（✅ 两个 handler 都先 `Redact` + `ShouldBlock`）

### Phase 3: GitHub Issues 搜索 (第 5 周) 🟢 锦上添花 — ✅ 已上线

| 序号 | 任务 | 产出 | 状态 |
|------|------|------|------|
| 3.1 | 编写 `search/github_client.go` | GitHub REST API 封装 | ✅ 强制 `is:issue`、`repo:`/`state:` 过滤、按 updated 排序、token 可选 |
| 3.2 | 注册 `search_github_issues` 工具 | GitHub 搜索 MCP Tool | ✅ 无 token 也注册（60 次/小时） |
| 3.3 | 集成测试 | 端到端验证 | ❌ 无专测；返回结果未过注入过滤 |

### Phase 4: 生产上线 (第 6 周) 🔵 — ⚠️ 部分

| 序号 | 任务 | 产出 | 状态 |
|------|------|------|------|
| 4.1 | 编写运维文档 | SearXNG 部署指南 / API Key 管理 | ⚠️ API Key 管理见 README「安全配置」与本文 §7；SearXNG 未部署（Out of Scope） |
| 4.2 | Prometheus 指标暴露 | 搜索调用次数、阻断次数、延迟 | ❌ 无搜索专用指标（现有仅 `diagnosis_total`、`llm_fallback_total`、MCP 拓扑缓存命中/未命中） |
| 4.3 | Gray 发布 (部分用户开启) | 逐步放量 | ⚠️ 只有全局 `external_search.enabled` 开关，无按用户灰度 |
| 4.4 | 最终复盘与优化 | 根据实际使用情况调整策略 | ❌ 未见记录 |

---

## 10. 风险评估与缓解

### 10.1 风险矩阵

| 风险 | 影响 | 概率 | 缓解措施 |
|------|------|------|---------|
| **敏感数据泄露** | 🔴 严重 | 🔴 高 | 严格脱敏层 + 高 PII 阻断 + 审计日志 |
| **Prompt Injection** | 🟡 中等 | 🟡 中等 | inbound 过滤 + Agent 不受外部指令影响的约束 |
| **搜索引擎 API 超时** | 🟡 中等 | 🟡 中等 | 全局超时 15s + 失败降级为内部诊断 |
| **LLM 幻觉** (错误引用搜索结果) | 🟡 中等 | 🟡 中等 | Prompt 要求引用源 URL + 人工复核 |
| **Token 消耗超预算** | 🟢 低 | 🔴 高 | 限制搜索结果数量 (默认 3 条) + 结果内容截断 (实现为 400 字符/条) |

### 10.2 故障恢复

**搜索引擎不可用时**:
- Agent 自动降级为纯内部诊断 (现有逻辑不变)
- 不影响现有 `query_topology`/`get_pod_logs` 等功能
- 记录降级事件到日志

**脱敏层异常时**:
- **Fail-Safe 设计**: 如果 `Sanitizer` 本身发生 panic/error，**完全阻止搜索**，而不是跳过脱敏
- 降级为内部诊断模式

---

## 11. 验收标准

> 勾选状态于 2026-11-12 对照代码核实；未勾选项为「未实现」或「未做端到端度量」，不代表功能缺失即阻塞上线。

### 功能验收

- [x] Agent 可在内部排查无果时调用 `search_knowledge_base` 工具（工具已注册，全量交给 LLM 自主选择）
- [ ] 外部搜索返回结果正确增强诊断质量（相比无搜索场景）——未做对比评估
- [x] `search_github_issues` 可找到相关 Issues 并引用到诊断报告（客户端 + 工具已上线；引用质量未评估）
- [x] 配置开关 `enabled: false` 时，外部工具对 Agent 不可见（`cmd/main.go` 以 `cfg.ExternalSearch.Enabled` 为前置条件）

### 安全验收

- [x] 任何包含内部 IP 的查询被脱敏——实际占位符为 `[REDACTED_IP_V4_<n>]`（原方案写的 `[REDACTED_IP]` 不带序号）
- [x] 任何包含 Token/密码的查询被脱敏——占位符为 `[REDACTED_JWT_<n>]` / `[REDACTED_SECRET_<n>]`
- [x] 高 PII 占比查询被完全阻断（`ShouldBlock()`，含单测）
- [ ] Prompt Injection 模式被正确检测并过滤——`FilterResults()` 已实现且有单测，但 GitHub 路径未调用，且随包 YAML 键名不匹配导致模式列表为空（§0）
- [ ] 审计日志正确记录所有外部调用的元数据——钩子已留但传 nil，未落库
- [x] 脱敏层异常时搜索被安全阻断 (fail-safe)——`NewSanitizer` 失败即不调用 `SetExternalSearch`，工具不注册

### 性能验收

- [ ] 外部搜索不使诊断总延迟超过 30 秒——未度量
- [x] 单次搜索 API 调用超时 ≤ 15 秒（Tavily `timeoutSeconds` 默认 15、GitHub 默认 10，均可配）
- [ ] 搜索结果 Token 消耗 < 1000——实现为 3 条 × **400** 字符截断（原方案写 300），另有 AI 摘要不计入该上限，未度量

---

## 12. 参考资料

| 资源 | 链接 |
|------|------|
| Eino 官方文档 | https://www.cloudwego.io/docs/eino |
| Eino Tool 实现指南 | https://www.cloudwego.io/docs/eino/core_modules/components/tools_node_guide |
| llm-search-mediator | https://github.com/SecAI-Hub/llm-search-mediator |
| AIOpsDoom / AIOpsShield (RSA 2025) | https://arxiv.org/abs/2508.06394 |
| Microsoft Presidio | https://microsoft.github.io/presidio |
| Kong AI Gateway PII 策略 | https://konghq.com/blog/enterprise/building-pii-sanitization-for-llms-and-agentic-ai |
| Gravitee AI Gateway PII Filter | https://www.gravitee.io/blog/how-to-prevent-pii-leaks-in-ai-systems |
| Tavily API 文档 | https://docs.tavily.com |
| GitHub Search API | https://docs.github.com/en/rest/search |

---

*文档结束。如有疑问或建议，请提交 Issue 或联系架构组。*
