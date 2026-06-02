import { describe, it, expect } from 'vitest'

// 冒烟测试：确保 vitest 和 jsdom 正常工作
describe('Smoke test', () => {
  it('jsdom environment is available', () => {
    expect(typeof window).toBe('object')
    expect(typeof document).toBe('object')
  })

  it('basic assertions work', () => {
    expect(1 + 1).toBe(2)
  })
})
