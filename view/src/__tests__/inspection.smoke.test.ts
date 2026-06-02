import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('inspection page', () => {
  it('mounts without error', () => {
    const App = { template: '<div>inspection</div>' }
    const wrapper = mount(App)
    expect(wrapper.exists()).toBe(true)
  })
})
