---
name: mutongctl
description: 通过 mutongctl CLI 操作重明（Mutong）AIOps 平台，覆盖 K8s 资源查询、告警管理、AI 诊断、巡检复盘、日志指标等运维任务。
---

# mutongctl — 重明 AIOps 平台 CLI

`mutongctl` 是重明（Mutong / 牧童）AIOps 平台的命令行工具，通过 HTTP 调用后端 API。

**使用方式**：直接在终端执行 `mutongctl <command>`。Agent 调用时自动使用 `-o json` 获取结构化数据，链式组合多个命令完成复杂排查。

**配置**：`mutongctl auth login` 启动浏览器 OAuth2 授权码流程（PKCE），在浏览器中登录后自动回调获取 token，保存到 `~/.mutong/tokens.json`。也可通过 `--token` 或 `MUTONG_TOKEN` 环境变量手动提供 PAT。

## 命令速查

### 认证登录
```bash
mutongctl auth login [-s <server-url>]
```
启动浏览器 OAuth2 授权码流程，登录后自动回调保存 token。

### 资源查询
```bash
mutongctl list pods [-n namespace] [-o json]
mutongctl list nodes|deployments|statefulsets|daemonsets|services [-n ns]
mutongctl get pod <name> -n namespace
mutongctl get node <name>
```

### 拓扑查询
```bash
mutongctl topo get <uid> --depth 2 [-o json]
mutongctl topo search --kind Pod --name "nginx-*"
```

### 告警管理
```bash
mutongctl alert list [-S severity] [--namespace ns] -o json
mutongctl alert get <fingerprint> -o json
mutongctl alert suppression -o json
```

### AI 诊断
```bash
mutongctl diagnose run --fingerprint <fp> -o json
mutongctl diagnose status
```

### 巡检
```bash
mutongctl inspect run -o json
mutongctl inspect report -o json
mutongctl inspect list|get|compare|trend
```

### 复盘
```bash
mutongctl retro generate <fingerprint>
mutongctl retro list [-S severity] [--page 1]
mutongctl retro get <fingerprint>
mutongctl retro export <fingerprint>
mutongctl retro search "关键词" -o json
mutongctl retro timeline <fingerprint> -o json
mutongctl retro causal-chain <fingerprint> -o json
```

### 日志
```bash
mutongctl logs pod <name> [-n ns] [--tail 100]
mutongctl logs search "关键词" [-n ns] [--since 1h]
mutongctl logs errors [-n ns] [--since 1h]
```

### 指标
```bash
mutongctl metrics resource <Pod|Node|Deployment|...> --name <name> [-n ns] -o json
mutongctl metrics timeseries --expr "PromQL" --start <ts> --end <ts> --step 60
```

### 集群与系统
```bash
mutongctl cluster list|health <name>|stats <name>
mutongctl system status -o json
```

### 业务拓扑
```bash
mutongctl biz apps [--businessUnit xxx] [--team xxx]
mutongctl biz graph [--businessUnit xxx]
```

### 执行器（危险操作需 --confirm）
```bash
mutongctl exec execute --action restart_pod|scale_deployment|... --target <name> --confirm
mutongctl exec audit [--action xxx]
mutongctl exec status
```

### 统计
```bash
mutongctl stats overview
mutongctl stats sync
```

### 环境诊断
```bash
mutongctl --version
mutongctl debug-info -o json
```

### 配置管理
```bash
mutongctl config set --server <url> --namespace <ns>
mutongctl config get
mutongctl config contexts
mutongctl config use-context <name>
```

### Shell 补全
```bash
source <(mutongctl completion bash|zsh|fish)
```

## Agent 典型排查流程

```
1. mutongctl debug-info -o json                  # 确认连通性
2. mutongctl alert list -o json                   # 发现告警
3. mutongctl diagnose run --fingerprint <fp> -o json  # 一键诊断
4. mutongctl retro timeline <fp> -o json           # 事件时间线
5. mutongctl retro causal-chain <fp> -o json       # 因果链分析
6. mutongctl metrics resource Pod --name xxx -n ns -o json  # 验证指标
7. mutongctl retro search "关键词" -o json          # 历史案例
```

## 错误处理

所有错误使用 `[ERROR]/[CONTEXT]/[HINT]` 格式：

| 退出码 | 含义 |
|--------|------|
| 0 | 成功 |
| 1 | API 错误 |
| 2 | 参数错误 |
| 3 | 认证失败 |
| 4 | 网络错误 |
| 5 | 资源不存在 |

## 输出格式

| 模式 | 触发 | 适用场景 |
|------|------|---------|
| Table | TTY 默认 | 人类终端 |
| JSON  | `-o json` 或管道 | Agent 调用、程序解析 |
| Markdown | `-o md` | 文档嵌入 |
| YAML | `-o yaml` | 配置导出 |

Agent 调用始终使用 `-o json` 获取结构化数据，链式提取字段后继续调用。
