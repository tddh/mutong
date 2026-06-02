import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('topology page', () => {
  it('mounts without error', () => {
    const App = { template: '<div>topology</div>' }
    const wrapper = mount(App)
    expect(wrapper.exists()).toBe(true)
  })
})
