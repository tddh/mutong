import { createApp, ref } from 'vue'

export function buildSSOLoginURL() {
  return '/api/auth/oidc/login'
}

export function safeRedirect(raw) {
  if (typeof raw !== 'string' || raw === '') return '/view/index.html'
  if (raw.includes('..')) return '/view/index.html'
  if (raw.startsWith('/view/')) return raw
  if (raw.startsWith('/device')) return raw
  if (raw.startsWith('/oauth2/')) return raw
  return '/view/index.html'
}

/* 会话凭据只存后端下发的 HttpOnly cookie（mutong_session）：
   sessionStorage 里的 token 可被任意 XSS 读走。后端 BearerTokenMiddleware
   对全部 API 与 terminal WebSocket 握手都接受该 cookie，前端无需持有 token。
   这里仅缓存非敏感的用户展示信息。 */
export async function performLogin(username, password) {
  const loginRes = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
    credentials: 'include',
  })

  if (!loginRes.ok) {
    const data = await loginRes.json().catch(() => ({}))
    throw new Error(data.error || '用户名或密码错误')
  }

  const data = await loginRes.json()
  sessionStorage.setItem('user', JSON.stringify(data.user))
  // 清理旧版本残留的 token，避免升级后继续暴露在 sessionStorage
  sessionStorage.removeItem('access_token')
  return data
}

createApp({
  setup() {
    const username = ref('')
    const password = ref('')
    const error = ref('')
    const loading = ref(false)
    const authMode = ref('loading')
    const ssoLabel = ref('SSO')

    ;(async () => {
      try {
        const res = await fetch('/api/auth/login-status')
        const data = await res.json()
        authMode.value = data.mode || 'local'
        ssoLabel.value = data.provider_label || 'SSO'
      } catch {
        authMode.value = 'local'
      }
    })()

    function loginWithZitadel() {
      window.location.href = '/api/auth/oidc/login'
    }

    async function handleLogin() {
      error.value = ''
      if (!username.value || !password.value) {
        error.value = '请输入用户名和密码'
        return
      }
      loading.value = true

      try {
        await performLogin(username.value, password.value)
        const redirect = new URLSearchParams(location.search).get('redirect')
        window.location.href = safeRedirect(redirect)
      } catch (e) {
        error.value = e.message
      } finally {
        loading.value = false
      }
    }

    return { username, password, error, loading, authMode, ssoLabel, loginWithZitadel, handleLogin }
  },
}).mount('#app')
