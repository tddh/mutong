import { h, computed } from 'vue'
import { Icon } from './icons.js'

/* 无状态呈现层。全部只用语义 token 与 data-* 属性选择器上色，
   组件自身不出现任何色号——这样暗色不需要组件级适配。
   class 一律 .m- 前缀，避免与 7 份页面 CSS 里的既有类名相撞。 */

export { Icon }

/** 页头：标题 + 说明 + 右侧操作区 */
export const PageHeader = {
  name: 'PageHeader',
  props: {
    title: { type: String, default: '' },
    desc: { type: String, default: '' },
  },
  setup(props, { slots }) {
    return () =>
      h('header', { class: 'm-page-header' }, [
        h('div', { class: 'm-page-header-text' }, [
          props.title ? h('h1', { class: 'm-page-title' }, props.title) : null,
          props.desc ? h('p', { class: 'm-page-desc' }, props.desc) : null,
          slots.default ? h('div', { class: 'm-page-header-slot' }, slots.default()) : null,
        ]),
        slots.actions ? h('div', { class: 'm-page-actions' }, slots.actions()) : null,
      ])
  },
}

/** 面板：替代此前 .card / .status-card / .section / .report-card 四种重复卡片壳 */
export const Panel = {
  name: 'Panel',
  props: {
    title: { type: String, default: '' },
    /** 标题右侧的辅助说明 */
    hint: { type: String, default: '' },
    loading: { type: Boolean, default: false },
    /** 去掉内边距，让表格自己贴边 */
    flush: { type: Boolean, default: false },
  },
  setup(props, { slots }) {
    return () =>
      h('section', { class: ['m-panel', props.flush && 'is-flush'] }, [
        props.title || slots.actions
          ? h('div', { class: 'm-panel-head' }, [
              h('div', { class: 'm-panel-titles' }, [
                h('h2', { class: 'm-panel-title' }, props.title),
                props.hint ? h('span', { class: 'm-panel-hint' }, props.hint) : null,
              ]),
              slots.actions ? h('div', { class: 'm-panel-actions' }, slots.actions()) : null,
            ])
          : null,
        h(
          'div',
          { class: 'm-panel-body' },
          props.loading ? [h(Skeleton, { rows: 3 })] : slots.default ? slots.default() : null,
        ),
      ])
  },
}

const SEVERITIES = ['critical', 'error', 'warning', 'info', 'pass', 'pending', 'default']

/** 状态徽标：替代 alerts / status / logs / clusters / inspection 里各写一遍的同类实现 */
export const StatusBadge = {
  name: 'StatusBadge',
  props: {
    /** critical|error|warning|info|pass|pending|default，未知值归 default 而不抛错 */
    sev: { type: String, default: 'default' },
    text: { type: String, default: '' },
    /** 带前导圆点，用于状态列表 */
    dot: { type: Boolean, default: false },
  },
  setup(props) {
    const sev = computed(() => (SEVERITIES.includes(props.sev) ? props.sev : 'default'))
    return () =>
      h('span', { class: 'm-badge', 'data-sev': sev.value }, [
        props.dot ? h('i', { class: 'm-badge-dot' }) : null,
        h('span', null, props.text),
      ])
  },
}

/** 按钮：variant 控颜色、size 控高度，图标按钮传 icon 不传 text */
export const MBtn = {
  name: 'MBtn',
  props: {
    variant: { type: String, default: 'default' }, // primary|default|danger|text
    size: { type: String, default: 'md' }, // sm|md
    icon: { type: String, default: '' },
    loading: { type: Boolean, default: false },
    disabled: { type: Boolean, default: false },
    type: { type: String, default: 'button' },
    title: { type: String, default: '' },
  },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        {
          class: ['m-btn', `is-${props.variant}`, `is-${props.size}`],
          type: props.type,
          disabled: props.disabled || props.loading,
          title: props.title,
          onClick: (e) => {
            if (props.disabled || props.loading) return
            emit('click', e)
          },
        },
        [
          props.loading
            ? h('span', { class: 'm-btn-spinner' })
            : props.icon
              ? h(Icon, { name: props.icon, size: props.size === 'sm' ? 14 : 16 })
              : null,
          slots.default ? h('span', { class: 'm-btn-label' }, slots.default()) : null,
        ],
      )
  },
}

/** 工具条：筛选器与操作的统一容器，自动换行 */
export const Toolbar = {
  name: 'Toolbar',
  setup(props, { slots }) {
    return () => h('div', { class: 'm-toolbar' }, slots.default ? slots.default() : null)
  },
}

/** 空态：区分"还没有数据"与"筛选后无结果"，后者给清除筛选的出口 */
export const EmptyState = {
  name: 'EmptyState',
  props: {
    icon: { type: String, default: 'archive' },
    title: { type: String, default: '暂无数据' },
    desc: { type: String, default: '' },
  },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'm-empty' }, [
        h(Icon, { name: props.icon, size: 34, stroke: 1.3 }),
        h('p', { class: 'm-empty-title' }, props.title),
        props.desc ? h('p', { class: 'm-empty-desc' }, props.desc) : null,
        slots.default ? h('div', { class: 'm-empty-action' }, slots.default()) : null,
      ])
  },
}

/** 统计卡：从 SharedComponents.js 迁入，props 与结构保持不变以免打断仪表盘页 */
export const StatCard = {
  name: 'StatCard',
  props: ['title', 'value', 'unit', 'color', 'icon'],
  template: `
        <div class="stat-card">
            <div class="stat-content">
                <span class="stat-title">{{ title }}</span>
                <span class="stat-value" :style="{ color: color }">{{ value }}<small class="unit">{{ unit }}</small></span>
            </div>
            <div class="stat-icon" v-if="icon">{{ icon }}</div>
        </div>
    `,
}

/** 骨架屏：加载态不再只有一行"加载中..."文字 */
export const Skeleton = {
  name: 'Skeleton',
  props: {
    rows: { type: Number, default: 3 },
    /** text|card|table */
    kind: { type: String, default: 'text' },
  },
  setup(props) {
    return () =>
      h(
        'div',
        { class: ['m-skeleton', `is-${props.kind}`], 'aria-hidden': 'true' },
        Array.from({ length: props.rows }, (_, i) =>
          h('span', { class: 'm-skeleton-bar', style: { width: `${92 - i * 8}%` } }),
        ),
      )
  },
}
