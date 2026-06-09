# Mutong (重明)

> 📦 **Open Source Repositories**
> - **GitHub**: [https://github.com/tddh/mutong](https://github.com/tddh/mutong)
> - **Gitee**:  [https://gitee.com/tddh/mutong](https://gitee.com/tddh/mutong)

## Overview

**Mutong (重明)** is a Kubernetes resource visualization and intelligent operations (AIOps) platform. It automatically collects resources from K8s clusters, builds complete resource topology relationships using a graph database, and provides interactive topology visualization, intelligent alert convergence and diagnosis, declarative inspection, self-healing execution, and post-incident retrospective analysis — all in one platform.

Built on the "**Observe, Understand, Remediate, Learn**" philosophy, Mutong starts from resource visualization, progresses through hybrid AI diagnosis to identify root causes and automatically execute remediation actions, and finally transforms each incident into structured, searchable knowledge assets — forming a complete operations loop.

### Name Origin

> **离，丽也。日月丽乎天，百谷草木丽乎土，重明以丽乎正，乃化成天下。** — *I Ching, Li Hexagram, Tuan Zhuan* (离卦·彖传)

### Core Values

- **Resource Visualization**: Stores K8s resource topology in Nebula Graph, supports complex relationship queries and interactive topology visualization with three rendering engines (G6 / force-graph / d3)
- **Alert Convergence & Intelligent Diagnosis**: Topology-aware alert suppression and causal chain suppression to eliminate alert storms; hybrid AI diagnosis engine (rule-based fast path + LLM deep analysis) with automatic fallback and multi-dimensional confidence scoring
- **Declarative Inspection**: YAML-based inspection rules with 6 built-in check types, Cron scheduling, historical report comparison and trend analysis
- **Self-Healing Executor**: Risk-graded (Low/Medium/High) automatic remediation (Pod eviction, Deployment scaling, HPA management) with manual approval and auto-execution modes, complete audit logging
- **Retrospective Knowledge**: Event timeline + causal chain DAG + LLM postmortem reports + graph-enhanced hybrid retrieval (pgvector semantic + NebulaGraph topology), transforming every incident into reusable knowledge

### 💡 Why this project / Evolution

Mutong's evolution maps the three stages of Kubernetes operations, each step driven by real-world pain points:

| Phase | Pain Point | Solution |
| :--- | :--- | :--- |
| **🟢 2022 Visualization** | Invisible relationships, unknown blast radius | **NebulaGraph Topology**: K8s resource mapping for cascading impact analysis |
| **🟡 2023 Automation** | Manually typing commands while looking at the map is tiring | **API-First Strategy**: Exposed ops as APIs for CLI/AI (since my frontend skills are weak) |
| **🔴 2025 Intelligence** | Root cause ambiguity, siloed expert knowledge | **LLM Agent Hybrid Diagnosis**: Rule fallback + AI-driven troubleshooting loop |

> Every feature is forged in production scenarios. See [Full Evolution Story →](docs/evolution.en.md)

## Features

### 1. Resource Collection & Storage
- **Multiple Resource Types**: Pods, Services, Deployments, ConfigMaps, Nodes, StatefulSets, DaemonSets, Ingresses, PVCs, and more — plus auto-discovered application services via Beyla eBPF + OTel Collector
- **Graph Database**: Nebula Graph for storing resource topology (nodes + edges), with nGQL query support and injection protection
- **Relational Database**: PostgreSQL + pgvector (GORM driver, connection pool 100) for users, roles, audit logs, diagnosis results, postmortems, and vectorized incident knowledge
- **Message Queue**: Kafka (franz-go client) for async message processing with dead letter queue and exponential backoff retry
- **Session Cache**: Redis (go-redis/v9) for diagnosis session caching and session-level result storage
- **In-Memory Cache**: BigCache (v3) for sharded high-performance caching with configurable capacity and cleanup policies

### 2. Resource Relationship Analysis & Visualization
- Automatic topology construction (Pod-Deployment, Service-Pod, Node-Pod, Ingress-Service, etc.)
- Complex nGQL-based resource search and multi-level relationship queries
- Three rendering engines: @antv/g6 (2D interactive), force-graph (3D force-directed), d3 (custom SVG)
- Multi-dimensional filtering by namespace, resource type, labels, and fuzzy name matching

### 3. Alert Convergence & Intelligent Diagnosis
- **Topology-Aware Suppression**: Auto-suppresses derived alerts within the same topology scope
- **Causal Chain Suppression**: Identifies causal relationships between alerts (e.g., Kafka failure suppresses upstream Consumer timeout)
- **Declarative Suppression Rules**: YAML-configured suppression by topology scope (same_node / calls_app_upstream / same_owner)
- **Owner/Stakeholder Dual Routing**: Middleware alerts notify both the responsible party (PagerDuty) and all affected business stakeholders (Slack)
- **Hybrid AI Diagnosis Engine**:
  - Multi-dimensional auto-confidence scoring: rule diagnosis score + related alerts + business criticality
  - LLM deep analysis via Eino (cloudwego/eino) ReAct Agent + Function Calling with automatic MCP tool invocation
  - Automatic fallback to rule-only diagnosis when LLM is unavailable
  - Persistent diagnosis results (root cause, business impact, call chain, metric snapshots, log fragments, topology snapshots → PostgreSQL)

### 4. Inspection System
- Declarative YAML rules: `query` (nGQL) + `check` + `severity` + `suggestion`
- 3 built-in check types: `min_rows` (threshold validation), `field_contains` (substring matching), `command` (external plugin via stdin/stdout JSON)
- 6 built-in rule categories: certificate expiry, single point of failure, data silo detection, resource quota monitoring, monitoring blind spot scanning, image auditing
- Cron scheduling + manual trigger
- Historical report comparison, trend analysis, and pass/fail/warn statistics

### 5. Self-Healing Executor
- 11 operation types: `restart_pod`, `delete_pod`, `scale_deployment`, `create_hpa`, `update_hpa`, `update_configmap`, `update_secret`, `update_resource_limits`, `update_deployment_image`, `update_annotations`, `update_labels`
- 3 risk levels: Low / Medium / High
- Dual execution mode: manual approval (default) / auto mode (risk threshold based)
- Complete audit logging to PostgreSQL

### 6. Retrospective Analysis
- Event timeline with MTTD (Mean Time To Detect) annotation
- Causal chain analysis with DAG visualization of fault propagation paths
- LLM-generated structured postmortem reports (WhatWentWell / WhatWentWrong / ContributingFactors)
- Diagnosis cache reuse (root cause, business impact, call chains, metric snapshots, log fragments, topology snapshots)
- Graph-enhanced hybrid retrieval: pgvector semantic + NebulaGraph topology dual-path retrieval with fusion ranking
- Retrospective-resource bidirectional association

### 7. Logs & Metrics
- Elasticsearch log queries with keyword search and severity filtering
- Prometheus metric queries at Pod/Node/Deployment level
- BigCache for query result caching

### 8. Web Terminal
- WebSocket-based K8s container terminal sessions
- Multi-container switching within a Pod
- Independent session lifecycle management
- Auto-resize on terminal resize events

### 9. Distributed Tracing
- OpenTelemetry integration with SkyWalking OAP backend support
- Service dependency analysis and visualization

### 10. MCP Tool Server
Implements the Model Context Protocol (MCP) with 20 built-in tools for LLM Function Calling:

| Tool | Description |
|------|-------------|
| `query_topology` | Query K8s resource topology (nodes + edges) |
| `get_active_alerts` | Get active alert list |
| `get_alert_detail` | Get alert details by fingerprint |
| `run_diagnosis` | Run AI diagnosis workflow |
| `inspect_resource` | Check K8s resource real-time status |
| `get_inspection_report` | Get latest inspection report |
| `list_resources_from_graph` | List resources from NebulaGraph |
| `list_k8s_resources` | Query resources from K8s API |
| `list_resources_from_cache` | Query resources from local informer cache |
| `get_resource_metrics` | Get resource Prometheus metric snapshot |
| `query_metric_timeseries` | Query metric time-series data |
| `get_system_health` | Get system component health status |
| `get_metric_catalog` | Get available Prometheus metric catalog |
| `get_pod_logs` | Get pod logs (K8s API) |
| `get_pod_logs_es` | Get pod logs (Elasticsearch) |
| `search_logs` | Full-text log search (ES) |
| `get_error_logs` | Get Error-level logs (ES) |
| `search_similar_cases` | Vector similarity search for historical cases |
| `list_alerts` | Simplified active alert list |
| `generate_retrospective` | Generate postmortem report (conditional) |

### 11. RBAC
- 3 roles: `admin` (full access), `operator` (read + execute), `viewer` (read-only)
- Dashboard-level permissions with different default views per role
- Token-based authentication middleware with rate limiting

### 12. Business Topology
- Auto-discovery via Beyla eBPF (HTTP/gRPC calls) → OTel Collector → Kafka → business attribution
- Auto-built application call topology with G6 interactive rendering
- Business label sync from K8s labels, mapping business apps to K8s workloads

### 13. CLI Tool (mutongctl)
Full-featured CLI for CI/CD pipelines, automation scripts, and on-call troubleshooting. See [mutongctl Usage Manual](docs/mutongctl-usage.md) for details.

```bash
mutongctl alert list --severity critical
mutongctl diagnose run --fingerprint <fp>
mutongctl logs pod nginx -n production --tail 50
mutongctl inspect run && mutongctl inspect report
```

## Frontend Pages

15 standalone pages with responsive layout and unified sticky NavBar:

| Page | Entry | Description |
|------|-------|-------------|
| Dashboard | `view/src/index.html` | 6 core metrics + quick links |
| Topology | `view/src/topology/index.html` | Force-graph, dual view (physical/business) |
| Resource List | `view/src/resource-table.html` | Paginated table (500/page), kind-colored |
| Alert Management | `view/src/alerts/index.html` | Aggregated alerts, dual-tab drawer |
| AI Diagnosis | `view/src/diagnosis/index.html` | Chat-style Markdown, stateless diagnosis mode |
| Inspection Report | `view/src/inspection/index.html` | Manual trigger, 6 built-in rule categories |
| Inspection History | `view/src/inspection-history/index.html` | Filter, compare, trend analysis |
| Retrospective Analysis | `view/src/retrospective/index.html` | Timeline + DAG + topology + impact assessment |
| Retrospective History | `view/src/retrospective-history/index.html` | Filter, edit (UPSERT), Markdown export |
| Logs | `view/src/logs/index.html` | ES log search, 7 filters, level-colored |
| Monitoring | `view/src/monitoring/index.html` | Metric card grid, 8 Prometheus metric types |
| Web Terminal | `view/src/terminal/index.html` | xterm.js 5.5, 5-step connection, auto-resize |
| Trace | `view/src/trace/index.html` | SkyWalking, service/operation/TraceID query |
| Clusters | `view/src/clusters/index.html` | Cluster list + health check, 30s auto-refresh |
| System Status | `view/src/status/index.html` | 5 status cards, 30s auto-refresh |

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.25 |
| Web Framework | Gin v1.10 |
| Graph DB | NebulaGraph v3 |
| Relational DB | PostgreSQL + pgvector (GORM) |
| Message Queue | Kafka (franz-go) |
| Cache | BigCache v3 + Redis |
| Logging | Zap + Lumberjack |
| Monitoring | Prometheus + gops |
| Tracing | OpenTelemetry + SkyWalking |
| AI Framework | Eino (cloudwego/eino) |
| LLM | OpenAI / Claude / MiniMax (configurable) |
| eBPF | Beyla (Grafana) |
| Profiling | Pyroscope |
| Frontend | Vue 3 + Vite |
| Visualization | @antv/g6 + force-graph + d3 |
| Package Manager | Bun |
| CLI | Cobra + Viper |
| Build | Just (Justfile) |

## Quick Start

### Prerequisites

| Component | Version | Purpose | Install |
|-----------|---------|---------|---------|
| Go | 1.25+ | Backend build | [go.dev/dl](https://go.dev/dl/) |
| Bun | 1.x+ | Frontend package manager | [bun.sh](https://bun.sh/) |
| NebulaGraph | 3.8+ | Graph database for topology | [nebula-graph.io](https://www.nebula-graph.io/download) |
| Kafka (or Redpanda) | Kafka 2.x+ / Redpanda v24+ | Async message queue | [kafka.apache.org](https://kafka.apache.org/downloads) |
| Kubernetes cluster | — | Resource collection & self-healing | kubeconfig access required |

**Optional components** (not required, corresponding features unavailable without them):

| Component | Purpose |
|-----------|---------|
| PostgreSQL 15+ (with pgvector) | Relational data & vector search |
| Prometheus | Metrics query & alert rules |
| Elasticsearch | Log search |
| Redis | Diagnosis session cache |
| Beyla (eBPF) + OTel Collector | Business topology auto-discovery (see [deploy/](deploy/)) |
| SkyWalking OAP | Distributed tracing backend |
| Pyroscope | Continuous profiling |
| LLM API Key | AI diagnosis deep analysis & postmortem generation |

### Setup

```bash
# 1. Clone
git clone https://gitee.com/tddh/mutong.git
cd mutong

# 2. Configure
# Copy the example configs and edit them:
for f in configs/*.yaml.example; do cp "$f" "${f%.example}"; done
cp agent.yaml.example agent.yaml
# Edit configs/*.yaml with your environment settings

# 3. Initialize databases
just init-nebula
# Run scripts/init_postgres.sh for PostgreSQL schema

# 4. Build & run
just build-mac       # macOS local dev
just run             # run with configs/ directory

# Debug mode
just run-debug
```

### Frontend Development

```bash
cd view && bun install
just dev-ui           # Dev mode with HMR
just build-ui         # Production build
```

## API Overview

The platform exposes REST APIs for all modules. Key endpoint groups:

| Module | Base Path |
|--------|-----------|
| K8s Resources | `/k8s/resources/*` |
| Alerts | `/api/v1/alerts/*` |
| AI Diagnosis | `/api/v1/diagnosis/*` |
| Inspection | `/api/v1/inspection/*` |
| Executor | `/api/v1/executor/*` |
| Logs | `/api/v1/logs/*` |
| Metrics & Monitoring | `/api/v1/metrics/*`, `/api/v1/monitoring/*` |
| Retrospective | `/api/v1/retrospective/*` |
| Business Topology | `/api/v1/business-topology/*` |
| Trace | `/api/v1/trace/*` |
| Terminal | `/api/v1/terminal/*` |
| RBAC | `/users`, `/roles` |
| System | `/api/v1/system/*`, `/api/v1/clusters/*`, `/api/v1/stats/*` |

See the [Chinese README](README.md) for the complete API reference with all endpoints.

## Build Commands

Key `just` commands:

| Command | Description |
|---------|------------|
| `just build` | Build Linux amd64 + macOS arm64 binaries |
| `just build-mac` | Build macOS arm64 only |
| `just test` | Run all unit tests with race detection |
| `just test-cover` | Run tests with coverage report |
| `just lint` | Static analysis (go vet) |
| `just fmt` | Format Go code (gofmt) |
| `just dev-ui` | Frontend dev server |
| `just build-ui` | Build frontend |
| `just all` | fmt + test + build |
| `just swagger` | Generate Swagger API docs |

## Troubleshooting

### Startup & Connectivity

**Q: Startup fails with "cannot connect to NebulaGraph"**  
Verify NebulaGraph is running: `nebula-console -addr localhost -port 9669 -u root -p nebula`. Confirm `configs/config.core.yaml` has correct `nebula.host` and `nebula.port`.

**Q: No data in topology page**  
Check Kafka is running and broker/topic are correctly configured in `configs/config.infra.yaml`. Initial Informer sync takes 1-2 minutes.

### AI Diagnosis

**Q: AI diagnosis unavailable, LLM not connected**  
Verify LLM config in `configs/config.diagnosis.yaml`: `provider` is not `none`, `apiKey` is set, `baseURL` is accessible. Use `MUTONG_LLM_API_KEY` env var for key management.

**Q: Semantic search not working**  
Ensure `embedding.model` is configured in `configs/config.diagnosis.yaml`. Graph-based search still works without embedding config.

### Performance

**Q: System is slow**  
Adjust BigCache `hardMaxCacheSize` (recommended: 4096 MB) and `shards` (recommended: 256). Enable Nebula cleanup (`nebulaCleanup.enabled: true`, `retentionDays: 7`). PostgreSQL connection pool should be kept at `pool: 100`.

## 🔒 Security

Mutong is designed with enterprise security in mind:

- **Authentication**: Argon2id password hashing, OAuth2/OIDC (PKCE), Session Cookie HMAC signing. Login includes rate limiting and anti-bruteforce.
- **Data Integrity**: PAT/SAT tokens are stored as SHA-256 hashes; nGQL queries are automatically sanitized to prevent injection; sensitive configs support environment variable overrides.
- **Operational Safety**: All remediation actions are risk-graded with full audit logs. High-risk operations require manual approval and are blocked by default.

## License

This project is licensed under the [MIT License](LICENSE).

## Community & Contributing

| [Code of Conduct](CODE_OF_CONDUCT.md) | [Contributing Guide](CONTRIBUTING.md) | [Roadmap](docs/TODO.md) | [Security Policy](SECURITY.md) |
|---|---|---|---|
| Learn our values | Submit code & bug fixes | See what's next | How to safely report vulnerabilities |

## Acknowledgements

- [Gin](https://github.com/gin-gonic/gin) — Go HTTP web framework
- [Nebula Graph](https://github.com/vesoft-inc/nebula) — Open-source distributed graph database
- [Apache Kafka](https://kafka.apache.org/) — Distributed messaging
- [Eino](https://github.com/cloudwego/eino) — AI Agent framework (ByteDance)
- [Vue.js](https://vuejs.org/) — Progressive frontend framework
- [AntV G6](https://g6.antv.vision/) — Graph visualization engine
- [Apache SkyWalking](https://skywalking.apache.org/) — APM
- [Grafana Beyla](https://github.com/grafana/beyla) — eBPF auto-instrumentation
- [Prometheus](https://prometheus.io/) — Monitoring & alerting
- [Grafana Pyroscope](https://grafana.com/oss/pyroscope/) — Continuous profiling
- [OpenTelemetry](https://opentelemetry.io/) — Observability framework
