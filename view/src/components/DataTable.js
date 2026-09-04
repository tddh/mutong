import { computed, h } from 'vue'
import { Icon } from './icons.js'
import { EmptyState, Skeleton } from './primitives.js'

/* 覆盖此前 5 个页面各写一遍的表格：告警、资源表格、巡检历史、集群、状态。
   分页与排序都是受控的——服务端分页是常态，组件不自己切数据，只上报意图。
   单元格优先级：col.render(row) > 具名插槽 col.slot > row[col.key] 文本。 */
export const DataTable = {
  name: 'DataTable',
  props: {
    columns: { type: Array, required: true },
    rows: { type: Array, default: () => [] },
    keyOf: { type: [String, Function], default: 'id' },
    loading: { type: Boolean, default: false },
    /** 服务端分页时传总数；省略则按当前行数算 */
    total: { type: Number, default: null },
    page: { type: Number, default: 1 },
    pageSize: { type: Number, default: 0 },
    selectable: { type: Boolean, default: false },
    selected: { type: Array, default: () => [] },
    sortKey: { type: String, default: '' },
    sortOrder: { type: String, default: '' }, // asc|desc
    rowClass: { type: Function, default: null },
    emptyTitle: { type: String, default: '暂无数据' },
    emptyDesc: { type: String, default: '' },
  },
  emits: ['update:page', 'update:selected', 'sort', 'row-click'],
  setup(props, { slots, emit }) {
    const rowKey = (row, i) =>
      typeof props.keyOf === 'function' ? props.keyOf(row) : (row[props.keyOf] ?? i)

    const sortedKeys = computed(() => props.rows.map(rowKey))
    const allSelected = computed(
      () => props.rows.length > 0 && props.rows.every((r, i) => props.selected.includes(rowKey(r, i))),
    )
    const pageCount = computed(() => {
      const total = props.total ?? props.rows.length
      if (!props.pageSize) return 1
      return Math.max(1, Math.ceil(total / props.pageSize))
    })

    const cellContent = (col, row, i) => {
      if (typeof col.render === 'function') return col.render(row, i)
      if (col.slot && slots[col.slot]) return slots[col.slot](row, i)
      const v = row[col.key]
      return v === null || v === undefined || v === '' ? '—' : String(v)
    }

    const cellProps = (col) => {
      const cls = []
      if (col.align === 'right') cls.push('is-numeric')
      if (col.mono) cls.push('is-mono')
      if (col.nowrap) cls.push('is-nowrap')
      if (col.muted) cls.push('is-muted')
      return { class: cls, style: col.width ? { width: col.width } : undefined }
    }

    const toggleAll = () => {
      emit('update:selected', allSelected.value ? [] : [...sortedKeys.value])
    }
    const toggleOne = (key) => {
      const has = props.selected.includes(key)
      emit('update:selected', has ? props.selected.filter((k) => k !== key) : [...props.selected, key])
    }
    const onSort = (col) => {
      if (!col.sortable) return
      const order = props.sortKey === col.key && props.sortOrder === 'asc' ? 'desc' : 'asc'
      emit('sort', { key: col.key, order })
    }

    /* 页码条：始终显示当前页邻域，首尾页直连，避免出现 1 … 23 45 67 */
    const pageList = computed(() => {
      const n = pageCount.value
      const cur = props.page
      if (n <= 7) return Array.from({ length: n }, (_, i) => i + 1)
      const set = new Set([1, n, cur, cur - 1, cur + 1])
      return [...set].filter((p) => p >= 1 && p <= n).sort((a, b) => a - b)
    })

    return () => {
      const head = []
      if (props.selectable) {
        head.push(
          h('th', { class: 'm-tbl-check' }, [
            h('input', {
              type: 'checkbox',
              checked: allSelected.value,
              onChange: toggleAll,
              'aria-label': '全选',
            }),
          ]),
        )
      }
      for (const col of props.columns) {
        const active = props.sortKey === col.key
        head.push(
          h(
            'th',
            {
              class: [col.sortable && 'is-sortable'],
              style: col.width ? { width: col.width } : undefined,
              'aria-sort': active ? (props.sortOrder === 'asc' ? 'ascending' : 'descending') : undefined,
              onClick: () => onSort(col),
            },
            [
              col.title,
              col.sortable
                ? h('span', { class: 'm-tbl-sort' }, [
                    active ? h(Icon, { name: 'chevron', size: 12 }) : null,
                  ])
                : null,
            ],
          ),
        )
      }

      const body = props.loading
        ? [
            h('tr', [
              h('td', { colspan: props.columns.length + (props.selectable ? 1 : 0) }, [
                h(Skeleton, { rows: 4 }),
              ]),
            ]),
          ]
        : props.rows.length === 0
          ? [
              h('tr', [
                h('td', { colspan: props.columns.length + (props.selectable ? 1 : 0) }, [
                  slots.empty
                    ? slots.empty()
                    : h(EmptyState, { title: props.emptyTitle, desc: props.emptyDesc }),
                ]),
              ]),
            ]
          : props.rows.map((row, i) => {
              const key = rowKey(row, i)
              const cells = []
              if (props.selectable) {
                cells.push(
                  h('td', { class: 'm-tbl-check' }, [
                    h('input', {
                      type: 'checkbox',
                      checked: props.selected.includes(key),
                      onChange: () => toggleOne(key),
                    }),
                  ]),
                )
              }
              for (const col of props.columns) {
                cells.push(h('td', cellProps(col), cellContent(col, row, i)))
              }
              return h(
                'tr',
                {
                  key,
                  class: [
                    props.selected.includes(key) && 'is-selected',
                    props.rowClass ? props.rowClass(row, i) : null,
                  ],
                  onClick: (e) => {
                    if (e.target?.type === 'checkbox') return
                    emit('row-click', row, i)
                  },
                },
                cells,
              )
            })

      const children = [h('thead', [h('tr', null, head)]), h('tbody', null, body)]
      const showPager = props.pageSize > 0 || props.page > 1
      if (showPager && !props.loading && props.rows.length > 0) {
        children.push(
          h('div', { class: 'm-pager' }, [
            h('span', null, `共 ${props.total ?? props.rows.length} 条`),
            h('div', { class: 'm-pager-pages' }, [
              h(
                'button',
                {
                  class: 'm-pager-btn',
                  type: 'button',
                  disabled: props.page <= 1,
                  onClick: () => emit('update:page', props.page - 1),
                },
                '‹',
              ),
              ...pageList.value.map((p) =>
                h(
                  'button',
                  {
                    class: ['m-pager-btn', p === props.page && 'is-active'],
                    type: 'button',
                    onClick: () => emit('update:page', p),
                  },
                  String(p),
                ),
              ),
              h(
                'button',
                {
                  class: 'm-pager-btn',
                  type: 'button',
                  disabled: props.page >= pageCount.value,
                  onClick: () => emit('update:page', props.page + 1),
                },
                '›',
              ),
            ]),
          ]),
        )
      }

      return h('div', { class: 'm-tbl-wrap' }, [h('table', { class: 'm-tbl' }, children)])
    }
  },
}
