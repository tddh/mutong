import { createApp, h, reactive } from 'vue'
import { Icon } from './icons.js'

/* 命令式轻提示。此前每个页面各自往 DOM 里塞消息节点、样式与层级互不相同，
   审批弹窗一出现就被提示盖住。统一到一个 z-index 层（--z-toast）与一种动效。
   用法：toast('已重启 Pod', 'pass')  /  toast(err, 'critical') */

const KINDS = {
  pass: { icon: 'check', ms: 2600 },
  info: { icon: 'info', ms: 3000 },
  warning: { icon: 'warning', ms: 3600 },
  critical: { icon: 'error', ms: 5200 },
}

const state = reactive({ items: [] })
let seq = 0
let mounted = false

const ToastStack = {
  props: { items: { type: Array, required: true } },
  emits: ['dismiss'],
  setup(props, { emit }) {
    return () =>
      h(
        'div',
        { class: 'm-toasts', role: 'status', 'aria-live': 'polite' },
        props.items.map((t) =>
          h('div', { class: 'm-toast', key: t.id, 'data-kind': t.kind }, [
            h(Icon, { name: KINDS[t.kind].icon, size: 16 }),
            h('span', { class: 'm-toast-text' }, t.text),
            h('button', {
              class: 'm-toast-close',
              type: 'button',
              'aria-label': '关闭提示',
              onClick: () => emit('dismiss', t.id),
            }, [h(Icon, { name: 'close', size: 14 })]),
          ]),
        ),
      )
  },
}

function ensureMounted() {
  if (mounted || typeof document === 'undefined') return
  mounted = true
  const host = document.createElement('div')
  host.id = 'mutong-toasts'
  document.body.appendChild(host)
  createApp(ToastStack, {
    items: state.items,
    onDismiss: (id) => {
      const i = state.items.findIndex((t) => t.id === id)
      if (i >= 0) state.items.splice(i, 1)
    },
  }).mount(host)
}

export function toast(text, kind = 'info', ms) {
  ensureMounted()
  const k = KINDS[kind] ? kind : 'info'
  const item = { id: ++seq, kind: k, text: String(text ?? '') }
  state.items.push(item)
  const ttl = ms ?? KINDS[k].ms
  if (ttl > 0) {
    setTimeout(() => {
      const i = state.items.findIndex((t) => t.id === item.id)
      if (i >= 0) state.items.splice(i, 1)
    }, ttl)
  }
  return item.id
}

toast.pass = (t, ms) => toast(t, 'pass', ms)
toast.info = (t, ms) => toast(t, 'info', ms)
toast.warning = (t, ms) => toast(t, 'warning', ms)
toast.error = (t, ms) => toast(t, 'critical', ms)
