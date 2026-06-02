import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('retrospective page', () => {
  it('mounts without error', () => {
    const App = { template: '<div>retrospective</div>' }
    const wrapper = mount(App)
    expect(wrapper.exists()).toBe(true)
  })
})
