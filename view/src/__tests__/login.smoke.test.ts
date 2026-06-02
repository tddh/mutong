import { describe, expect, it } from 'vitest'

describe('login redirect helper', () => {
  it('builds zitadel login redirect via backend entrypoint', async () => {
    const mod = await import('../login.js')
    expect(mod.buildSSOLoginURL()).toBe('/api/auth/oidc/login')
  })
})

describe('safeRedirect', () => {
  it('allows safe in-app /view/ paths', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('/view/index.html')).toBe('/view/index.html')
    expect(safeRedirect('/view/topology/index.html')).toBe('/view/topology/index.html')
    expect(safeRedirect('/view/retrospective/index.html')).toBe('/view/retrospective/index.html')
  })

  it('rejects absolute URLs (open redirect)', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('https://evil.com/phish')).toBe('/view/index.html')
    expect(safeRedirect('http://example.com')).toBe('/view/index.html')
  })

  it('rejects protocol-relative URLs', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('//evil.com')).toBe('/view/index.html')
  })

  it('rejects non-/view/ paths', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('/api/something')).toBe('/view/index.html')
    expect(safeRedirect('/etc/passwd')).toBe('/view/index.html')
  })

  it('falls back on empty / null / undefined', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('')).toBe('/view/index.html')
    expect(safeRedirect(null)).toBe('/view/index.html')
    expect(safeRedirect(undefined)).toBe('/view/index.html')
  })

  it('rejects path traversal payloads', async () => {
    const { safeRedirect } = await import('../login.js')
    expect(safeRedirect('/view/../api/admin')).toBe('/view/index.html')
    expect(safeRedirect('/view/foo/../../etc/passwd')).toBe('/view/index.html')
    expect(safeRedirect('/view/./../../bin/sh')).toBe('/view/index.html')
  })
})
