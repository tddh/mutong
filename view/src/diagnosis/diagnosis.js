import { createApp, ref, nextTick, onMounted, computed } from 'vue'
import { marked } from 'marked'
import { NavBar } from '../components/SharedComponents.js'
import { API } from '../utils/api.js'
import '../styles/common.css'
import './diagnosis.css'

marked.use({ breaks: true, gfm: true })

function renderMarkdown(text) {
  if (!text) return ''
  return marked.parse(text)
}

function buildHistory(messages) {
  return messages
    .filter((m) => ['user', 'assistant', 'tool_call', 'tool'].includes(m.role))
    .map((m) => {
      const entry = { role: m.role, content: m.content }
      if (m.tool_calls) entry.tool_calls = m.tool_calls
      if (m.tool_call_id) entry.tool_call_id = m.tool_call_id
      return entry
    })
}

const App = {
  components: { NavBar },
  setup() {
    const messages = ref([])
    const context = ref(null)
    const inputMessage = ref('')
    const isLoading = ref(false)
    const chatContainer = ref(null)
    const hasContext = ref(false)
    const businessImpact = ref(null)
    const businessAppCalls = ref(null)
    const businessImpactCollapsed = ref(true) // 默认折叠，最大化聊天区域

    // 增量渲染缓冲区
    const tokenBuffer = []
    let flushTimer = null
    let currentStreamMsg = null // 当前流式消息引用

    // 重试机制
    const lastUserMessage = ref('')

    // 工具调用浮动面板
    const toolCalls = ref([])

    // 聚合工具调用（相同名称合并计数）
    const aggregatedToolCalls = computed(() => {
      const grouped = {}
      for (const tc of toolCalls.value) {
        if (!grouped[tc.name]) {
          grouped[tc.name] = { name: tc.name, count: 0, status: 'completed' }
        }
        grouped[tc.name].count += 1
        if (tc.status === 'pending') {
          grouped[tc.name].status = 'pending'
        }
      }
      return Object.values(grouped)
    })

    // 流式超时控制
    let ttftTimer = null
    let idleTimer = null

    // 会话历史
    const isAuthenticated = ref(false)
    const sessions = ref([])
    const currentSessionId = ref(null)

    async function checkAuth() {
      try {
        const res = await fetch('/api/auth/me')
        isAuthenticated.value = res.ok
        if (res.ok) loadSessionList()
      } catch {
        isAuthenticated.value = false
      }
    }

    async function loadSessionList() {
      if (!isAuthenticated.value) return
      try {
        const res = await fetch('/api/v1/diagnosis/chat/sessions?limit=50')
        const data = await res.json()
        sessions.value = data.sessions || []
      } catch {
        // ignore
      }
    }

    async function loadSession(id) {
      try {
        const res = await fetch(`/api/v1/diagnosis/chat/sessions/${id}`)
        if (!res.ok) return
        const data = await res.json()
        if (!data || !data.session) return
        const s = data.session
        currentSessionId.value = id
        messages.value = s.messages || []
        context.value = s.context || null
        hasContext.value = !!(
          s.context?.resource_kind ||
          s.context?.alert ||
          (s.messages && s.messages.length > 0)
        )
      } catch {
        // ignore
      }
    }

    async function deleteSession(id) {
      try {
        await fetch(`/api/v1/diagnosis/chat/sessions/${id}`, { method: 'DELETE' })
        sessions.value = sessions.value.filter((s) => s.id !== id)
        if (currentSessionId.value === id) newChat()
      } catch {
        // ignore
      }
    }

    function newChat() {
      currentSessionId.value = null
      messages.value = []
      context.value = null
      hasContext.value = false
      toolCalls.value = []
    }

    async function init() {
      const params = new URLSearchParams(window.location.search)
      const fingerprint = params.get('alert_fingerprint')

      if (fingerprint) {
        await startChat({ alert_fingerprint: fingerprint })
      }
    }

    async function startChat(params) {
      try {
        const result = await API.diagnosis.chat.context(params)
        context.value = result.context
        hasContext.value = !!(
          context.value?.resource_kind ||
          context.value?.resource_name ||
          context.value?.alert
        )

        if (result.context?.impact?.businessImpact) {
          businessImpact.value = result.context.impact.businessImpact
        }
        if (result.context?.business_app_calls) {
          businessAppCalls.value = result.context.business_app_calls
        }

        messages.value = (result.initial_messages || []).map((msg, idx) => ({
          ...msg,
          id: msg.id || 'msg_init_' + idx,
        }))

        if (messages.value.length === 0) {
          messages.value.push({
            id: 'msg_welcome',
            role: 'assistant',
            content: '诊断上下文已加载，请输入问题开始诊断。',
            timestamp: new Date().toISOString(),
          })
        }
      } catch (error) {
        console.error('Failed to start chat:', error)
        messages.value.push({
          id: 'msg_error',
          role: 'system',
          content: '❌ 启动对话失败: ' + error.message,
          timestamp: new Date().toISOString(),
        })
      }
    }

    async function sendMessage() {
      if (!inputMessage.value.trim() || isLoading.value) return

      const userMsg = inputMessage.value.trim()
      messages.value.push({
        id: 'msg_user_' + Date.now(),
        role: 'user',
        content: userMsg,
        timestamp: new Date().toISOString(),
      })

      inputMessage.value = ''
      lastUserMessage.value = userMsg
      scrollToBottom()
      isLoading.value = true

      // 清理旧定时器
      if (ttftTimer) clearTimeout(ttftTimer)
      if (idleTimer) clearTimeout(idleTimer)

      try {
        const history = buildHistory(messages.value)

        const assistantMsg = {
          id: 'msg_streaming_' + Date.now(),
          role: 'assistant',
          content: '',
          timestamp: new Date().toISOString(),
          hasError: false,
        }
        messages.value.push(assistantMsg)
        currentStreamMsg = assistantMsg

        // 增量渲染：启动 30ms 定时刷新
        tokenBuffer.length = 0

        // 分层超时：只保留总超时。首 token/空闲超时已禁用——
        // 首步 LLM 思考可超过 60s，且传输链路可能攒批到达，激进超时会误掐断正常请求
        const FETCH_TIMEOUT = 300000 // 总超时 300s
        const TTFT_TIMEOUT = 0 // 首 token 超时：禁用
        const IDLE_TIMEOUT = 0 // 空闲超时：禁用

        const controller = new AbortController()
        const fetchTimeoutId = setTimeout(() => controller.abort(), FETCH_TIMEOUT)
        let firstTokenReceived = false

        const response = await fetch('/api/v1/diagnosis/chat/ask', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            messages: history,
            context: context.value,
            session_id: currentSessionId.value || '',
          }),
          signal: controller.signal,
        })
        clearTimeout(fetchTimeoutId)

        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''

        // 启动 TTFT 超时（禁用时为 0，不启动）
        if (TTFT_TIMEOUT > 0) {
          ttftTimer = setTimeout(() => {
            if (!firstTokenReceived) {
              controller.abort()
            }
          }, TTFT_TIMEOUT)
        }

        let lastChunkTime = Date.now()

        const resetIdleTimer = () => {
          if (IDLE_TIMEOUT <= 0) return
          if (idleTimer) clearTimeout(idleTimer)
          idleTimer = setTimeout(() => {
            controller.abort()
          }, IDLE_TIMEOUT)
        }

        while (true) {
          const { value, done } = await reader.read()
          if (done) break

          lastChunkTime = Date.now()
          resetIdleTimer()

          buffer += decoder.decode(value, { stream: true })

          const lines = buffer.split('\n')
          buffer = lines.pop() || ''

          for (const line of lines) {
            if (!line.trim()) continue
            try {
              const event = JSON.parse(line)
              switch (event.type) {
                case 'keepalive':
                  // 服务端保活事件：视为连接活跃，取消首 token 超时（空闲计时已由 chunk 到达重置）
                  if (!firstTokenReceived && ttftTimer) {
                    clearTimeout(ttftTimer)
                    ttftTimer = null
                  }
                  break
                case 'token':
                case 'stream_chunk':
                  if (!firstTokenReceived) {
                    firstTokenReceived = true
                    if (ttftTimer) {
                      clearTimeout(ttftTimer)
                      ttftTimer = null
                    }
                  }
                  if (event.content.trim()) {
                    tokenBuffer.push(event.content)
                    if (!flushTimer) {
                      flushTimer = setTimeout(() => flushTokens(), 30)
                    }
                  }
                  break
                case 'tool_start':
                case 'tool_call':
                  flushTokensNow()
                  toolCalls.value = [
                    ...toolCalls.value,
                    {
                      name: event.name,
                      status: 'pending',
                      id: 'tool_' + Date.now(),
                    },
                  ]
                  break
                case 'done':
                case 'action':
                  if (event.type === 'action') {
                    if (event.action_type === 'exit') {
                      isLoading.value = false
                      flushTokensNow()
                      if (event.session_id) {
                        currentSessionId.value = event.session_id
                        if (!sessions.value.some((s) => s.id === event.session_id)) {
                          sessions.value.unshift({
                            id: event.session_id,
                            title: (lastUserMessage.value || '').substring(0, 50),
                            resource_kind: context.value?.resource_kind || '',
                            resource_name: context.value?.resource_name || '',
                            last_active_at: new Date().toISOString(),
                            created_at: new Date().toISOString(),
                            status: 1,
                          })
                        }
                      }
                      // 工具标记完成，3 秒后清除
                      toolCalls.value = toolCalls.value.map((t) => ({ ...t, status: 'completed' }))
                      setTimeout(() => {
                        toolCalls.value = []
                      }, 3000)
                    }
                    break
                  }
                  flushTokensNow()
                  isLoading.value = false
                  toolCalls.value = toolCalls.value.map((t) => ({ ...t, status: 'completed' }))
                  setTimeout(() => {
                    toolCalls.value = []
                  }, 3000)
                  break
              }
            } catch (e) {
              console.warn('[Diagnosis] JSON parse error, line:', line.substring(0, 100), e)
            }
          }
        }

        flushTokensNow()
        isLoading.value = false
        clearTimeout(ttftTimer)
        clearTimeout(idleTimer)
        toolCalls.value = toolCalls.value.map((t) => ({ ...t, status: 'completed' }))
        setTimeout(() => {
          toolCalls.value = []
        }, 3000)

        if (!assistantMsg.content) {
          assistantMsg.content = 'AI 未返回有效内容，请重试或尝试换个问法。'
        }
        hasContext.value = true
        scrollToBottom()
      } catch (error) {
        flushTokensNow()
        clearTimeout(ttftTimer)
        clearTimeout(idleTimer)
        ttftTimer = null
        idleTimer = null
        isLoading.value = false
        const assistantMsg = getLastUserAssistantMsg()
        if (assistantMsg) {
          assistantMsg.hasError = true
          if (error.name === 'AbortError') {
            if (!assistantMsg.content) {
              assistantMsg.content = '⏱️ 请求超时，请重试或尝试更简单的问题'
            } else {
              assistantMsg.content += '\n\n⏱️ 响应中断，已返回部分结果。'
            }
          } else {
            assistantMsg.content = '❌ 请求失败: ' + error.message
          }
        }
        scrollToBottom()
      } finally {
        currentStreamMsg = null
        if (flushTimer) {
          clearTimeout(flushTimer)
          flushTimer = null
        }
        if (ttftTimer) {
          clearTimeout(ttftTimer)
          ttftTimer = null
        }
        if (idleTimer) {
          clearTimeout(idleTimer)
          idleTimer = null
        }
      }
    }

    function flushTokensNow() {
      if (flushTimer) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
      flushTokens()
    }

    function flushTokens() {
      if (tokenBuffer.length === 0) return
      if (!currentStreamMsg) return
      currentStreamMsg.content += tokenBuffer.join('')
      tokenBuffer.length = 0
      scrollToBottom()
    }

    function getLastUserAssistantMsg() {
      for (let i = messages.value.length - 1; i >= 0; i--) {
        const m = messages.value[i]
        if (m.role === 'assistant' && m.id && m.id.startsWith('msg_streaming_')) {
          return m
        }
      }
      return null
    }

    function toggleToolCall(msg) {
      msg.collapsed = !msg.collapsed
    }

    async function retryMessage() {
      if (isLoading.value) return
      if (!lastUserMessage.value) return

      inputMessage.value = lastUserMessage.value
      await sendMessage()
    }

    function scrollToBottom() {
      nextTick(() => {
        if (chatContainer.value) {
          chatContainer.value.scrollTop = chatContainer.value.scrollHeight
        }
      })
    }

    function handleKeyPress(e) {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        sendMessage()
      }
    }

    function toggleBusinessImpact() {
      businessImpactCollapsed.value = !businessImpactCollapsed.value
    }

    function formatTime(t) {
      if (!t) return ''
      const d = new Date(t)
      const diff = Date.now() - d.getTime()
      if (diff < 60000) return '刚刚'
      if (diff < 3600000) return Math.floor(diff / 60000) + ' 分钟前'
      if (diff < 86400000) return Math.floor(diff / 3600000) + ' 小时前'
      return d.toLocaleDateString()
    }

    function riskLabel(level) {
      const map = { critical: '🔴 严重', high: '🟠 高风险', medium: '🟡 中风险', low: '🟢 低风险' }
      return map[level] || level
    }

    onMounted(() => {
      checkAuth()
      init()
    })

    return {
      messages,
      context,
      inputMessage,
      isLoading,
      chatContainer,
      hasContext,
      isAuthenticated,
      sessions,
      currentSessionId,
      businessImpact,
      businessAppCalls,
      businessImpactCollapsed,
      toolCalls,
      aggregatedToolCalls,
      startChat,
      sendMessage,
      toggleBusinessImpact,
      handleKeyPress,
      riskLabel,
      renderMarkdown,
      retryMessage,
      lastUserMessage,
      loadSession,
      deleteSession,
      newChat,
      formatTime,
    }
  },
  template: `
        <nav-bar></nav-bar>
        <div class="diagnosis-page">
            <div class="diagnosis-layout">
                <div class="diagnosis-sidebar">
                    <div v-if="isAuthenticated" class="session-panel">
                        <button class="new-chat-btn" @click="newChat">+ 新对话</button>
                        <div class="session-list" v-if="sessions.length">
                            <div v-for="s in sessions" :key="s.id"
                                 :class="['session-item', { active: s.id === currentSessionId }]"
                                 @click="loadSession(s.id)">
                                <div class="session-title">{{ s.title || '新对话' }}</div>
                                <div class="session-meta">{{ s.resource_kind || '' }} · {{ formatTime(s.last_active_at) }}</div>
                                <button class="session-delete" @click.stop="deleteSession(s.id)">×</button>
                            </div>
                        </div>
                        <div v-else class="session-empty">暂无对话记录</div>
                    </div>
                    <div v-if="hasContext" class="context-card">
                        <h3>📋 诊断上下文</h3>
                        <div class="context-info">
                            <div v-if="context?.severity || context?.alert_name || context?.summary" class="context-group">
                                <div class="context-group-title">告警信息</div>
                                <div v-if="context?.severity" class="info-item">
                                    <span class="label">严重级别</span>
                                    <span class="value" :style="{ color: context?.severity === 'critical' ? '#cf1322' : '#d46b08' }">{{ context?.severity }}</span>
                                </div>
                                <div v-if="context?.alert_name" class="info-item">
                                    <span class="label">告警名称</span>
                                    <span class="value">{{ context?.alert_name }}</span>
                                </div>
                                <div v-if="context?.summary" class="info-item">
                                    <span class="label">摘要</span>
                                    <span class="value">{{ context?.summary }}</span>
                                </div>
                            </div>
                            <div class="context-group">
                                <div class="context-group-title">资源信息</div>
                                <div class="info-item">
                                    <span class="label">资源类型</span>
                                    <span class="value">{{ context?.resource_kind || '-' }}</span>
                                </div>
                                <div class="info-item">
                                    <span class="label">资源名称</span>
                                    <span class="value">{{ context?.resource_name || '-' }}</span>
                                </div>
                                <div class="info-item">
                                    <span class="label">命名空间</span>
                                    <span class="value">{{ context?.namespace || '-' }}</span>
                                </div>
                            </div>
                            <div v-if="context?.node_name || context?.owner_kind || (context?.topology_path && context?.topology_path.length)" class="context-group">
                                <div class="context-group-title">拓扑信息</div>
                                <div v-if="context?.node_name" class="info-item">
                                    <span class="label">所在节点</span>
                                    <span class="value">{{ context?.node_name }}</span>
                                </div>
                                <div v-if="context?.owner_kind" class="info-item">
                                    <span class="label">所属资源</span>
                                    <span class="value">{{ context?.owner_kind }}/{{ context?.owner_name || '-' }}</span>
                                </div>
                                <div v-if="context?.topology_path && context?.topology_path.length" class="info-item-stack">
                                    <span class="label">拓扑链路</span>
                                    <span class="value">{{ context?.topology_path.join(' → ') }}</span>
                                </div>
                            </div>
                            <div v-if="context?.enrich_tags" class="context-group">
                                <div class="context-group-title">业务标签</div>
                                <div class="info-item">
                                    <span class="label">应用</span>
                                    <span class="value">{{ context.enrich_tags.businessApp || '-' }}</span>
                                </div>
                                <div class="info-item">
                                    <span class="label">团队</span>
                                    <span class="value">{{ context.enrich_tags.team || '-' }}</span>
                                </div>
                                <div class="info-item">
                                    <span class="label">关键度</span>
                                    <span class="value">{{ context.enrich_tags.criticality || '-' }}</span>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>

                <div class="chat-panel">
                    <div v-if="businessImpact" class="biz-impact-card" :class="{ collapsed: businessImpactCollapsed }">
                        <div class="biz-impact-header" @click="toggleBusinessImpact">
                            <div class="biz-impact-header-left">
                                <span class="collapse-arrow">{{ businessImpactCollapsed ? '▶' : '▼' }}</span>
                                <h3>📊 业务影响分析</h3>
                                <div :class="['risk-badge', businessImpact.riskLevel]">
                                    {{ riskLabel(businessImpact.riskLevel) }}
                                </div>
                                <span class="impact-stats">
                                    <template v-if="businessImpact.directImpacts?.length">
                                        直接影响 {{ businessImpact.directImpacts.length }} 个
                                    </template>
                                    <template v-if="businessImpact.directImpacts?.length && businessImpact.indirectImpacts?.length">
                                         ·
                                    </template>
                                    <template v-if="businessImpact.indirectImpacts?.length">
                                        下游 {{ businessImpact.indirectImpacts.length }} 跳
                                    </template>
                                </span>
                            </div>
                            <button class="collapse-toggle-btn" @click.stop="toggleBusinessImpact">
                                {{ businessImpactCollapsed ? '展开' : '收起' }}
                            </button>
                        </div>
                        <div class="biz-impact-body" v-show="!businessImpactCollapsed">
                            <p v-if="businessImpact.summary" class="impact-summary">{{ businessImpact.summary }}</p>
                            <div v-if="businessImpact.directImpacts && businessImpact.directImpacts.length" class="impact-section">
                                <h4>🔴 直接影响</h4>
                                <div v-for="(item, idx) in businessImpact.directImpacts" :key="'d'+idx" class="impact-item">
                                    <div class="impact-app">{{ item.appName }}</div>
                                    <div class="impact-meta">
                                        <span v-if="item.criticality" :class="['crit-tag', item.criticality]">{{ item.criticality }}</span>
                                        <span v-if="item.team" class="team-tag">{{ item.team }}</span>
                                    </div>
                                    <div v-if="item.reasoning" class="impact-reason">{{ item.reasoning }}</div>
                                </div>
                            </div>
                            <div v-if="businessImpact.indirectImpacts && businessImpact.indirectImpacts.length" class="impact-section">
                                <h4>🟡 间接影响 (下游)</h4>
                                <div v-for="(item, idx) in businessImpact.indirectImpacts" :key="'i'+idx" class="impact-item">
                                    <div class="impact-app">{{ item.appName }}</div>
                                    <div class="impact-meta">
                                        <span class="hop-tag">跳数: {{ item.hopDistance }}</span>
                                        <span v-if="item.criticality" :class="['crit-tag', item.criticality]">{{ item.criticality }}</span>
                                        <span v-if="item.team" class="team-tag">{{ item.team }}</span>
                                    </div>
                                    <div v-if="item.impactPath" class="impact-path">{{ item.impactPath }}</div>
                                    <div v-if="item.reasoning" class="impact-reason">{{ item.reasoning }}</div>
                                </div>
                            </div>
                        </div>
                    </div>
                    <div ref="chatContainer" class="chat-messages">
                        <template v-for="msg in messages" :key="msg.id">
                            <div :class="['message', msg.role]">
                                <div class="message-content">
                                    <template v-if="msg.role === 'system'">
                                        <span class="system-text">{{ msg.content }}</span>
                                    </template>
                                    <template v-else-if="msg.role === 'assistant' || msg.role === 'tool'">
                                        <div class="bubble" v-html="renderMarkdown(msg.content)"></div>
                                        <div v-if="msg.hasError" class="retry-row">
                                            <button class="retry-btn" @click="retryMessage">🔄 重新生成</button>
                                        </div>
                                    </template>
                                    <template v-else>
                                        <div class="bubble">
                                            <p>{{ msg.content }}</p>
                                        </div>
                                    </template>
                                </div>
                            </div>
                        </template>
                    </div>

                    <div v-if="aggregatedToolCalls.length" class="tool-float-panel">
                        <div v-for="tc in aggregatedToolCalls" :key="tc.name" :class="['tool-float-item', tc.status]">
                            <span class="tool-dot"></span>
                            <span class="tool-name">{{ tc.name }}</span>
                            <span v-if="tc.count > 1" class="tool-count">×{{ tc.count }}</span>
                        </div>
                    </div>

                    <div class="chat-input">
                        <textarea
                            v-model="inputMessage"
                            placeholder="输入你的问题... (Enter 发送，Shift+Enter 换行)"
                            @keydown="handleKeyPress"
                            :disabled="isLoading"
                            rows="2"
                        ></textarea>
                        <button
                            class="send-btn"
                            @click="sendMessage"
                            :disabled="!inputMessage.trim() || isLoading"
                        >
                            发送
                        </button>
                    </div>
                </div>
            </div>
        </div>
    `,
}

const app = createApp(App)
app.mount('#app')
