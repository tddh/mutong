import { beforeEach, describe, expect, it, vi } from 'vitest'

describe('auth store init', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('marks loggedIn when /api/auth/me succeeds with cookie session', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ uuid: 'u-1', username: 'alice', role: 'admin', auth_provider: 'zitadel' }),
    }))
    const { useAuth } = await import('../stores/auth.js')
    const store = useAuth()
    await store.init()
    expect(store.status).toBe('loggedIn')
    expect(store.user.username).toBe('alice')
  })

  it('clears sessionStorage when init fails (no cookie, no token)', async () => {
    sessionStorage.setItem('access_token', 'stale-token')
    sessionStorage.setItem('user', JSON.stringify({ username: 'ghost' }))
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    const { useAuth } = await import('../stores/auth.js')
    const store = useAuth()
    await store.init()
    expect(store.status).toBe('loggedOut')
    expect(store.accessToken).toBeNull()
    expect(store.user).toBeNull()
    expect(sessionStorage.getItem('access_token')).toBeNull()
    expect(sessionStorage.getItem('user')).toBeNull()
  })
})
