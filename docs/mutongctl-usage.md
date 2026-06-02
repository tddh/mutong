# mutongctl CLI 使用手册

> 版本: v1.0 | 适用于 Mutong AIOps 平台

## 一、安装与配置

### 安装

```bash
# 从源码编译
just build-cli
# 二进制位于 artifacts/mutongctl-{linux-amd64,darwin-arm64}

# 直接运行
go run ./cmd/mutongctl/
```

### 配置

```bash
# 设置 server 地址和 token
mutongctl config set server https://mutong.example.com

# 查看当前配置
mutongctl config get

# 列出所有可用 context
mutongctl config contexts

# 多环境切换
mutongctl config set server https://mutong-staging.example.com
mutongctl config use-context staging
```

配置文件位置：`~/.mutong/config.yaml`

```yaml
current-context: production
contexts:
  production:
    server: https://mutong.example.com
    namespace: default
  staging:
    server: https://mutong-staging.example.com
```

认证优先级：`auth login` (OAuth2, ~/.mutong/tokens.json) → `--token` flag → `MUTONG_TOKEN` 环境变量

### 认证登录

```bash
# 浏览器授权码登录（推荐）
mutongctl auth login [-s <server-url>]

# 手动 PAT
mutongctl --token mtp_xxx alert list
```
启动浏览器 OAuth2 授权码流程（PKCE），在浏览器中登录后自动回调保存 token 到 `~/.mutong/tokens.json`。

也可通过 `--token` flag 或 `MUTONG_TOKEN` 环境变量手动提供 PAT（Personal Access Token，在 Web UI 创建）。

### 全局标志

| 标志 | 简写 | 说明 | 默认值 |
|------|------|------|--------|
| `--server` | `-s` | Mutong 服务地址 | `http://localhost:8888` |
| `--token` | `-t` | 认证 Token | 配置文件或环境变量 |
| `--output` | `-o` | 输出格式: json/table/md/yaml | TTY=table, 管道=json |
| `--verbose` | `-v` | 详细输出 (-v 环境信息, -vv 请求详情) | false |
| `--no-color` | — | 禁用颜色输出 | 管道自动 true |
| `--confirm` | — | 确认危险操作 | false |
| `--admin-key` | — | 管理员密钥（executor 等危险操作需要） | 环境变量 `MUTONG_ADMIN_KEY` 或配置文件 |

### 环境诊断

```bash
# 版本信息
mutongctl --version

# 环境诊断（Agent 排查第一步）
mutongctl debug-info [-o json]    # server / token来源 / 连通性
```

---

## 二、命令速查

### 资源查询

```bash
# 列出资源
mutongctl list pods [-n namespace]
mutongctl list nodes
mutongctl list deployments [-n namespace]
mutongctl list statefulsets [-n namespace]
mutongctl list daemonsets [-n namespace]
mutongctl list services [-n namespace]

# 获取资源详情
mutongctl get pod <name> -n namespace
mutongctl get node <name>

# 拓扑查询
mutongctl topo get <uid> --depth 2
mutongctl topo search --kind Pod --name "nginx-*"
```

### 告警

```bash
# 列出活跃告警
mutongctl alert list [--severity critical] [--namespace production] [-o json]
mutongctl alert list -o md | less    # Markdown 表格

# 获取告警详情
mutongctl alert get <fingerprint> [-o json]

# 查看抑制状态
mutongctl alert suppression
```

### AI 诊断

```bash
# 一键诊断（后端自动编排上下文采集 + LLM 分析）
mutongctl diagnose run --fingerprint <fp> [-o json]

# 查看诊断引擎状态
mutongctl diagnose status
```

**说明**：`diagnose run` 后端自动并行采集拓扑快照、指标、日志、历史案例，LLM 综合分析后返回结构化结果（根因 + 影响 + 建议）。

### 巡检

```bash
mutongctl inspect run                     # 触发巡检
mutongctl inspect report                  # 最新报告
mutongctl inspect list
mutongctl inspect get <report-id>
mutongctl inspect compare <id1> <id2>
mutongctl inspect trend --days 30
```

### 复盘

```bash
mutongctl retro generate <fingerprint>        # 生成复盘报告
mutongctl retro list --severity critical --page 1
mutongctl retro get <fingerprint>
mutongctl retro export <fingerprint> > report.md
mutongctl retro search "Pod OOMKilled"        # 混合检索历史案例
mutongctl retro timeline <fingerprint>        # 事件时间线
mutongctl retro causal-chain <fingerprint>    # 因果链分析
```

### 日志

```bash
mutongctl logs pod <name> -n default --tail 100
mutongctl logs search "OOMKilled" -n production --since 1h
mutongctl logs errors -n default --since 30m
```

### 指标

```bash
mutongctl metrics resource Pod --name nginx -n production [-o md]
mutongctl metrics resource Node --name node-1
mutongctl metrics timeseries --expr "rate(cpu[5m])" --start 1700000000 --end 1700003600 --step 60
```

### 集群与系统

```bash
mutongctl cluster list
mutongctl cluster health <name>
mutongctl cluster stats <name>
mutongctl system status [-o md]
```

### 业务拓扑

```bash
mutongctl biz apps [--team "交易平台组"]
mutongctl biz graph [--businessUnit "core"]
```

### 执行器（危险操作需 --confirm，支持 11 种操作类型）

```bash
mutongctl exec status
mutongctl exec audit --action restart_pod

# 10 种操作类型
mutongctl exec execute --action restart_pod --target <pod> -n default --confirm
mutongctl exec execute --action delete_pod --target <pod> -n default --confirm
mutongctl exec execute --action scale_deployment --target <deploy> -n default --replicas 3 --confirm
mutongctl exec execute --action create_hpa --target <deploy> -n default --min 1 --max 5 --cpu 50 --confirm
mutongctl exec execute --action update_hpa --target <deploy> -n default --min 2 --max 10 --cpu 80 --confirm
mutongctl exec execute --action update_configmap --target <cm> -n default --data key=val --confirm
mutongctl exec execute --action update_secret --target <secret> -n default --data key=val --confirm
mutongctl exec execute --action update_resource_limits --target <deploy> -n default --cpu-limit 500m --mem-limit 1Gi --confirm
mutongctl exec execute --action update_deployment_image --target <deploy> -n default --image nginx:1.22 --confirm
mutongctl exec execute --action update_annotations --target <deploy> -n default --ann key=val --confirm
mutongctl exec execute --action update_labels --target <deploy> -n default --label key=val --confirm
```

### 终端（⚠️ 仅人类交互）

```bash
mutongctl terminal --pod <name> -n default [-c container]
```

### 统计

```bash
mutongctl stats overview
mutongctl stats sync
```

### Shell 补全

```bash
source <(mutongctl completion bash)
source <(mutongctl completion zsh)
source <(mutongctl completion powershell)
mutongctl completion fish | source
```

---

## 三、输出格式

### 四种输出模式

| 模式 | 触发 | 适用场景 |
|------|------|---------|
| **Table** | TTY 默认 | 人类终端查看 |
| **Markdown** | `-o md` | Agent 对话渲染、文档嵌入 |
| **JSON** | 管道 / `-o json` | 程序解析、管道 jq |
| **YAML** | `-o yaml` | 配置导出 |

### 示例

```bash
# 表格（终端）
mutongctl alert list --severity critical

# Markdown（Agent 推荐）
mutongctl system status -o md

# JSON + jq 管道
mutongctl list pods -o json | jq '.[] | select(.status=="Running") | .name'

# 管道自动 JSON
mutongctl alert list | jq '.[].fingerprint'
```

---

## 四、Agent 使用指南

### Skill 调用

Agent 可通过 opencode Skill 自动调用 mutongctl：

```yaml
# 已注册 13 个 Skill，分为三类：

# 数据获取（-o json，链式调用）:
mutong-list-pods, mutong-alert-list, mutong-alert-get,
mutong-diagnose-run, mutong-logs-pod, mutong-logs-search, mutong-metrics-pod

# 用户展示（-o md，直接渲染）:
mutong-inspect-run, mutong-inspect-report, mutong-retro-search,
mutong-topo-get, mutong-system-status

# 诊断工具:
mutong-debug-info
```

### 典型排查流程

```bash
# 1. 环境诊断
mutongctl debug-info -o json

# 2. 发现告警
mutongctl alert list -n production -o json

# 3. 一键诊断（自动编排上下文）
mutongctl diagnose run --fingerprint <fp> -o json

# 4. 验证指标
mutongctl metrics resource Pod --name nginx -n production -o json

# 5. 查看日志
mutongctl logs pod nginx -n production --tail 50

# 6. 展示历史案例
mutongctl retro search "OOMKilled" -o md
```

### 错误处理

所有错误使用 `[ERROR]/[CONTEXT]/[HINT]` 格式，Agent 可据此自动修复：

```
[ERROR] 认证失败 (exit_code=3)
[CONTEXT] server=http://localhost:8888, auth_source=env:MUTONG_TOKEN
[HINT] 运行 'mutongctl config set token <your-token>' 重新设置
```

---

## 五、退出码

| 退出码 | 含义 |
|--------|------|
| 0 | 成功 |
| 1 | API 错误 |
| 2 | 参数错误 |
| 3 | 认证失败 |
| 4 | 网络错误 |
| 5 | 资源不存在 |

---

## 六、构建

```bash
just build-cli-mac    # macOS 本地构建
just build-cli        # 跨平台构建（linux+darwin）
just test-cli         # 运行全部 CLI 测试
just all-cli          # 测试 + 构建
```
