import { Teleport, h, onMounted, onUnmounted } from 'vue'
import { Icon } from './icons.js'

/* 浮层统一走 Teleport 到 body：集群页原先把遮罩画在 .container 内部，
   一旦祖先元素带上 transform 就会改变 fixed 的包含块、弹窗被裁切。
   层级一律取 --z-mask / --z-panel，不再各处写 z-index:999 / 1000。 */

export const MModal = {
  name: 'MModal',
  props: {
    open: { type: Boolean, default: false },
    title: { type: String, default: '' },
    width: { type: Number, default: 560 },
    /** 遮罩点击是否关闭；涉及不可逆操作时应设为 false */
    maskClosable: { type: Boolean, default: true },
  },
  emits: ['close'],
  setup(props, { slots, emit }) {
    const close = () => emit('close')
    const onKey = (e) => {
      if (e.key === 'Escape' && props.open) close()
    }
    onMounted(() => document.addEventListener('keydown', onKey))
    onUnmounted(() => document.removeEventListener('keydown', onKey))

    return () => {
      if (!props.open) return null
      return h(Teleport, { to: 'body' }, [
        h(
          'div',
          {
            class: 'm-mask',
            onClick: () => {
              if (props.maskClosable) close()
            },
          },
          [
            h(
              'div',
              {
                class: 'm-modal',
                role: 'dialog',
                'aria-modal': 'true',
                'aria-label': props.title,
                style: `max-width:${props.width}px`,
                onClick: (e) => e.stopPropagation(),
              },
              [
                h('div', { class: 'm-modal-head' }, [
                  h('h2', { class: 'm-modal-title' }, props.title),
                  h(
                    'button',
                    {
                      class: 'm-modal-close',
                      type: 'button',
                      'aria-label': '关闭',
                      onClick: close,
                    },
                    [h(Icon, { name: 'close', size: 16 })],
                  ),
                ]),
                h('div', { class: 'm-modal-body' }, slots.default ? slots.default() : null),
                slots.footer
                  ? h('div', { class: 'm-modal-foot' }, slots.footer())
                  : null,
              ],
            ),
          ],
        ),
      ])
    }
  },
}

/** 右侧抽屉：用于详情侧看，不打断主列表 */
export const MDrawer = {
  name: 'MDrawer',
  props: {
    open: { type: Boolean, default: false },
    title: { type: String, default: '' },
    width: { type: Number, default: 520 },
  },
  emits: ['close'],
  setup(props, { slots, emit }) {
    const close = () => emit('close')
    const onKey = (e) => {
      if (e.key === 'Escape' && props.open) close()
    }
    onMounted(() => document.addEventListener('keydown', onKey))
    onUnmounted(() => document.removeEventListener('keydown', onKey))

    return () => {
      if (!props.open) return null
      return h(Teleport, { to: 'body' }, [
        h('div', { class: 'm-mask', onClick: close }, [
          h(
            'aside',
            {
              class: 'm-drawer',
              role: 'dialog',
              'aria-modal': 'true',
              'aria-label': props.title,
              style: `width:${props.width}px`,
              onClick: (e) => e.stopPropagation(),
            },
            [
              h('div', { class: 'm-modal-head' }, [
                h('h2', { class: 'm-modal-title' }, props.title),
                h(
                  'button',
                  {
                    class: 'm-modal-close',
                    type: 'button',
                    'aria-label': '关闭',
                    onClick: close,
                  },
                  [h(Icon, { name: 'close', size: 16 })],
                ),
              ]),
              h('div', { class: 'm-modal-body' }, slots.default ? slots.default() : null),
              slots.footer ? h('div', { class: 'm-modal-foot' }, slots.footer()) : null,
            ],
          ),
        ]),
      ])
    }
  },
}
