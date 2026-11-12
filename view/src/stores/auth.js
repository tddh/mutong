import { reactive } from 'vue'

/* 凭据单轨：会话只认后端 HttpOnly cookie（mutong_session），前端不持有 token。
   sessionStorage 仅缓存非敏感的用户展示信息，供首帧免闪烁；真实状态以 /api/auth/me 为准。 */
const authState = reactive({
  status: sessionStorage.getItem('user') ? 'loggedIn' : 'unknown',
  user: JSON.parse(sessionStorage.getItem('user') || 'null'),

  get isAuthenticated() {
    return this.status === 'loggedIn'
  },

  async init() {
    try {
      const res = await fetch('/api/auth/me', { credentials: 'include' })
      if (res.ok) {
        this.user = await res.json()
        sessionStorage.setItem('user', JSON.stringify(this.user))
        this.status = 'loggedIn'
        return
      }
    } catch {
      // 网络失败按未登录处理，由页面引导重新登录
    }

    this.status = 'loggedOut'
    this.user = null
    sessionStorage.removeItem('user')
    // 清理旧版本残留的 token
    sessionStorage.removeItem('access_token')
  },

  async logout() {
    await fetch('/api/auth/logout', { method: 'POST', credentials: 'include' })
    this.user = null
    this.status = 'loggedOut'
    sessionStorage.removeItem('user')
    sessionStorage.removeItem('access_token')
    window.location.href = '/view/login.html'
  },
})

export function useAuth() {
  return authState
}