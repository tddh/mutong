# Project Evolution

**Mutong** (originally named **resourcemap**) started as a simple visualization tool and evolved into an AI-driven operations platform.

## Thoughts on Interaction Model (and a bit of selfishness)

In this project, I made a very conscious trade-off: **There are almost no complex operation buttons on the Web.**

Although Mutong supports 11 complex remediation operations (scaling, hot updates, HPA, etc.), I chose not to build traditional form-based UIs. There are two reasons for this:

1. **The Professional Reason**: I believe "API-First" and "Conversational UI" are the future. Instead of forcing users to navigate complex forms and modals, the AI can simply understand intent and execute via API.
2. **The Brutally Honest Reason**: **My frontend skills are terrible.** As an SRE, I can write Go and nGQL all day, but asking me to build dozens of validated Vue forms and tweak CSS... that's just torture.

**Since I can't win that fight, I changed the battlefield:**
I encapsulated all these capabilities as APIs and handed them over to the **CLI (mutongctl)** and **AI Agents**.
* **Web UI stays minimal**: Focus on what I'm good at — Topology (Observation) and Chat (Interaction).
* **CLI handles the heavy lifting**: Scripts are faster than mouse clicks for senior engineers anyway.
* **AI as the UX layer**: Users speak natural language, and the AI invokes those backend APIs that I "forgot" to build buttons for.

It turns a frontend weakness into a modern, "AI-Native" architectural advantage.

## Phase 1: Resource Topology (2022)
**Core Problem**: As K8s cluster size increased, relationships between resources (Pods, Services, Ingresses) became hard to track. "Invisibility" was the biggest pain point.
**Solution**: Introduce a graph database (NebulaGraph).
* **Origin**: Wanted a global topology view rather than manual `kubectl get` queries.
* **Implementation**: Synced K8s resources to a graph database via Informer cache to enable complex relationship queries and frontend visualization.
* **Outcome**: Solved impact analysis, cascading fault tracing, and orphan resource discovery.

## Phase 2: Automation & Governance (2023)
**Core Problem**: With visibility achieved, alert storms and slow manual remediation became the next bottleneck.
**Solution**: Move from "seeing" to "acting".
* **Alert Convergence**: Implemented causal chain suppression based on topology to eliminate noise.
* **Self-Healing Executor**: Defined risk grading (Low/Medium/High) for auto/semi-auto remediation (Pod restarts, scaling, etc.).
* **Outcome**: Handled high-frequency repetitive operations and established a robust audit mechanism.

## Phase 3: AI-Assisted Diagnosis (2025)
**Core Problem**: Automation handled "what to do", but "why it broke" (root cause analysis) still relied heavily on individual tribal knowledge, making MTTR hard to reduce further.
**Solution**: Implement an AI-driven diagnosis loop.
* **Hybrid Architecture**: Designed a "Rule-based Fast Path" (low cost, deterministic) + "LLM Deep Analysis" (high flexibility) dual-path system.
* **MCP Tooling**: Encapsulated 20 operational actions (Topology search, log analysis, metrics check) as tools for LLM autonomous troubleshooting.
* **Knowledge Retention**: Introduced pgvector + NebulaGraph hybrid retrieval to vectorize incident retrospectives for future search.
* **Outcome**: Enhanced troubleshooting efficiency using AI while keeping rule-based fallbacks.

## Phase 4: Self-Healing Closed Loop (2026)
**Core Problem**: AI could pinpoint root causes and propose fixes, but the "fix" itself still required manual commands — the loop was broken between diagnosis and execution. Yet handing execution directly to an LLM carries unacceptable risk.
**Solution**: Add **execution tools** to the MCP tool server, gated by a propose-and-approve mechanism.
* **Two tool groups**:
    * **Safe group** (Pod restart, Deployment rollout restart, scaling): invocations pass through the existing gate — auto mode + diagnosis-confidence threshold — with full audit trails;
    * **Proposal group** (image change, resource limits adjustment): the LLM can only create pending-approval proposals; these are **never auto-executed** and must be approved by a human in the UI.
* **Admission constraints**: every execution tool requires an alert fingerprint; calls without a supporting diagnosis result (no confidence score) are rejected outright, preventing the LLM from acting on nothing; duplicate pending plans for the same fingerprint are deduplicated.
* **Auto-diagnosis persistence**: alert-triggered diagnoses are written to cache and database and automatically produce pending plans, so the UI reuses them instead of re-diagnosing on demand.
* **Real bugs fixed along the way**: alert self-suppression (suppression rules matching the alert's own fingerprint), knowledge-base actions mismatched with executor enums, slow chat diagnoses aborted by frontend timeouts, audit table losing execution parameters, etc.
* **Outcome**: a complete diagnosis → plan → approval → execution → audit loop. The button for dangerous actions remains in human hands, while every step before it runs automatically.

---
**Summary**:
Mutong did not start with AI in mind. It followed a path of **Visibility (Topology) → Automation → Intelligence (AI Diagnosis) → Self-Healing Closed Loop**. Each step addressed a specific bottleneck in the operations workflow. And on the question of "how much execution authority to grant", the answer has always been: let AI propose, let humans decide.
