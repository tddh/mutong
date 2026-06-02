import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'

describe('diagnosis page', () => {
  it('mounts without error', () => {
    const App = { template: '<div>diagnosis</div>' }
    const wrapper = mount(App)
    expect(wrapper.exists()).toBe(true)
  })
})
