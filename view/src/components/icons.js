import { computed, h } from 'vue'

/* 内联 SVG 图标集，替代此前散在菜单与页面里的 emoji 图标。
   零依赖前提下唯一可控的做法：emoji 的尺寸与基线由系统字体决定，无法与
   14px 正文对齐，且在暗色下不受 color 控制。
   统一 24×24 网格、stroke 取 currentColor，故图标颜色随语义文本色自动翻转。 */
const P = {
  dashboard: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
  topology:
    '<circle cx="12" cy="5" r="2.4"/><circle cx="5" cy="18" r="2.4"/><circle cx="19" cy="18" r="2.4"/><path d="M10.4 6.9 6.6 15.7M13.6 6.9l3.8 8.8M7.4 18h9.2"/>',
  alert:
    '<path d="M18 8.5a6 6 0 1 0-12 0c0 6-2.2 7.5-2.2 7.5h16.4S18 14.5 18 8.5"/><path d="M13.7 20a2 2 0 0 1-3.4 0"/>',
  ai: '<rect x="4" y="7.5" width="16" height="12" rx="2.5"/><path d="M12 4v3.5M9.2 12.5v1.6M14.8 12.5v1.6M2.5 12v3M21.5 12v3"/>',
  inspect:
    '<rect x="5" y="3.5" width="14" height="17" rx="2"/><path d="M9 3.5h6v2.6H9z"/><path d="M8.6 13.4l2 2 4.4-4.6"/>',
  history:
    '<path d="M3.5 12a8.5 8.5 0 1 0 2.6-6.1"/><path d="M3.5 4.5V9H8"/><path d="M12 8v4.4l3.2 1.9"/>',
  report:
    '<path d="M14 3.5H7.5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h9a2 2 0 0 0 2-2V8z"/><path d="M14 3.5V8h4.5"/><path d="M9 13h6M9 16.5h4"/>',
  archive:
    '<rect x="3" y="4" width="18" height="4.5" rx="1.2"/><path d="M4.8 8.5V19a1.5 1.5 0 0 0 1.5 1.5h11.4a1.5 1.5 0 0 0 1.5-1.5V8.5"/><path d="M10 12.5h4"/>',
  table:
    '<rect x="3" y="4.5" width="18" height="15" rx="1.8"/><path d="M3 9.5h18M3 14.5h18M9 9.5v10"/>',
  status: '<path d="M3 12.5h4l2.4-6.5 4 13L16 12.5h5"/>',
  terminal:
    '<rect x="2.5" y="4.5" width="19" height="15" rx="2"/><path d="M6.5 9.5l3 3-3 3M12 15.5h5"/>',
  cluster:
    '<rect x="3" y="4" width="18" height="6" rx="1.6"/><rect x="3" y="14" width="18" height="6" rx="1.6"/><path d="M6.8 7h.01M6.8 17h.01"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2.2M12 19.3v2.2M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M2.5 12h2.2M19.3 12h2.2M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6"/>',
  moon: '<path d="M20.5 13.3A8.8 8.8 0 1 1 10.7 3.5a7 7 0 0 0 9.8 9.8z"/>',
  refresh: '<path d="M20.5 12a8.5 8.5 0 1 1-2.6-6.1"/><path d="M20.5 4v5H15.5"/>',
  search: '<circle cx="10.8" cy="10.8" r="6.3"/><path d="M15.4 15.4l4.1 4.1"/>',
  close: '<path d="M6 6l12 12M18 6L6 18"/>',
  chevron: '<path d="M6.5 9.5l5.5 5.5 5.5-5.5"/>',
  arrow: '<path d="M9.5 6.5l5.5 5.5-5.5 5.5"/>',
  check: '<path d="M4.8 12.5l4.7 4.7L19.2 7.3"/>',
  warning: '<path d="M12 4.2 2.8 19.8h18.4z"/><path d="M12 10v4.2M12 17.2h.01"/>',
  error: '<circle cx="12" cy="12" r="8.5"/><path d="M9.2 9.2l5.6 5.6M14.8 9.2l-5.6 5.6"/>',
  info: '<circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.2M12 7.8h.01"/>',
  more: '<circle cx="5.5" cy="12" r="1.4"/><circle cx="12" cy="12" r="1.4"/><circle cx="18.5" cy="12" r="1.4"/>',
  clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.2V12l3.4 2"/>',
}

export const ICON_NAMES = Object.keys(P)

export const Icon = {
  name: 'Icon',
  props: {
    name: { type: String, required: true },
    size: { type: [Number, String], default: 16 },
    // 描边粗细在大图标上会显粗，允许按场景调
    stroke: { type: [Number, String], default: 1.7 },
  },
  setup(props) {
    const markup = computed(() => P[props.name] || '')
    return () =>
      h(
        'svg',
        {
          class: 'icon',
          width: props.size,
          height: props.size,
          viewBox: '0 0 24 24',
          fill: 'none',
          stroke: 'currentColor',
          'stroke-width': props.stroke,
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          'aria-hidden': 'true',
          focusable: 'false',
          innerHTML: markup.value,
        },
      )
  },
}
