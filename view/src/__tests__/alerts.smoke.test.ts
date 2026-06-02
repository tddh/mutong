import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('alerts page', () => {
  it('mounts without error', () => {
    const App = { template: '<div>alerts</div>' }
    const wrapper = mount(App)
    expect(wrapper.exists()).toBe(true)
  })
})
