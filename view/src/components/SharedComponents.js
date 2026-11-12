/* 兼容转发层：15 个页面入口以 `import { NavBar } from '.../SharedComponents.js'` 引用，
   组件本体已按职责拆到同目录下的独立文件。保留此文件以免一次改 15 处 import；
   新页面请直接引用具体模块。 */
export { NavBar } from './NavBar.js'
export { StatCard, PageHeader, Panel, StatusBadge, MBtn, EmptyState, Skeleton, Icon } from './primitives.js'
