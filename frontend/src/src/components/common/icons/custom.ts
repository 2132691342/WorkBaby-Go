import { createIcon } from './createIcon'

/**
 * EP 图标库缺失图标的自绘定义（图标统一）。
 *
 * <p>EP（@element-plus/icons-vue）没有的图标统一在此自绘，数据取自 lucide（ISC 协议）path，
 * 行为与 lucide-vue-next 完全一致（props：size / color / stroke-width）。
 *
 * <p>命名与 lucide 保持一致（`<Xxx />`），模板零改动，仅换 import 来源。
 */

export const Activity = createIcon('Activity', [
  ['path', { d: 'M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2' }],
])

export const Archive = createIcon('Archive', [
  ['rect', { width: '20', height: '5', x: '2', y: '3', rx: '1' }],
  ['path', { d: 'M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8' }],
  ['path', { d: 'M10 12h4' }],
])

export const MoreHorizontal = createIcon('MoreHorizontal', [
  ['circle', { cx: '12', cy: '12', r: '1', fill: 'currentColor' }],
  ['circle', { cx: '19', cy: '12', r: '1', fill: 'currentColor' }],
  ['circle', { cx: '5', cy: '12', r: '1', fill: 'currentColor' }],
])

export const Terminal = createIcon('Terminal', [
  ['polyline', { points: '4 17 10 11 4 5' }],
  ['line', { x1: '12', x2: '20', y1: '19', y2: '19' }],
])

export const Webhook = createIcon('Webhook', [
  ['path', { d: 'M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 0 1 2 17c.01-.7.2-1.4.57-2' }],
  ['path', { d: 'm6 17 3.13-5.78c.53-.97.1-2.18-.5-3.1a4 4 0 1 1 6.89-4.06' }],
  ['path', { d: 'm12 6 3.13 5.73C15.66 12.7 16.9 13 18 13a4 4 0 0 1 0 8' }],
])

export const Mail = createIcon('Mail', [
  ['rect', { width: '20', height: '16', x: '2', y: '4', rx: '2' }],
  ['path', { d: 'm22 7-10 6L2 7' }],
])

export const AtSign = createIcon('AtSign', [
  ['circle', { cx: '12', cy: '12', r: '4' }],
  ['path', { d: 'M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8' }],
])

export const Bot = createIcon('Bot', [
  ['path', { d: 'M12 8V4H8' }],
  ['rect', { width: '16', height: '12', x: '4', y: '8', rx: '2' }],
  ['path', { d: 'M2 14h2' }],
  ['path', { d: 'M20 14h2' }],
  ['path', { d: 'M15 13v2' }],
  ['path', { d: 'M9 13v2' }],
])

export const Braces = createIcon('Braces', [
  ['path', { d: 'M8 3H7a2 2 0 0 0-2 2v5a2 2 0 0 1-2 2 2 2 0 0 1 2 2v5c0 1.1.9 2 2 2h1' }],
  ['path', { d: 'M16 21h1a2 2 0 0 0 2-2v-5c0-1.1.9-2 2-2a2 2 0 0 1-2-2V5a2 2 0 0 0-2-2h-1' }],
])

export const Brain = createIcon('Brain', [
  ['path', { d: 'M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z' }],
  ['path', { d: 'M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z' }],
  ['path', { d: 'M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4' }],
  ['path', { d: 'M17.599 6.5a3 3 0 0 0 .399-1.375' }],
  ['path', { d: 'M6.003 5.125A3 3 0 0 0 6.401 6.5' }],
  ['path', { d: 'M3.477 10.896a4 4 0 0 1 .585-.396' }],
  ['path', { d: 'M19.938 10.5a4 4 0 0 1 .585.396' }],
  ['path', { d: 'M6 18a4 4 0 0 1-1.967-.516' }],
  ['path', { d: 'M19.967 17.484A4 4 0 0 1 18 18' }],
])

export const Calculator = createIcon('Calculator', [
  ['rect', { width: '16', height: '20', x: '4', y: '2', rx: '2' }],
  ['line', { x1: '8', x2: '16', y1: '6', y2: '6' }],
  ['line', { x1: '16', x2: '16', y1: '14', y2: '18' }],
  ['path', { d: 'M16 10h.01' }],
  ['path', { d: 'M12 10h.01' }],
  ['path', { d: 'M8 10h.01' }],
  ['path', { d: 'M12 14h.01' }],
  ['path', { d: 'M8 14h.01' }],
  ['path', { d: 'M12 18h.01' }],
  ['path', { d: 'M8 18h.01' }],
])

export const Cat = createIcon('Cat', [
  ['path', { d: 'M12 5c.67 0 1.35.09 2 .26 1.78-2 5.03-2.84 6.42-2.26 1.4.58-.42 7-.42 7 .57 1.07 1 2.24 1 3.44C21 17.9 16.97 21 12 21s-9-3-9-7.56c0-1.25.5-2.4 1-3.44 0 0-1.89-6.42-.5-7 1.39-.58 4.72.23 6.5 2.23A9.04 9.04 0 0 1 12 5Z' }],
  ['path', { d: 'M8 14v.5' }],
  ['path', { d: 'M16 14v.5' }],
  ['path', { d: 'M11.25 16.25h1.5L12 17l-.75-.75Z' }],
])

export const Circle = createIcon('Circle', [
  ['circle', { cx: '12', cy: '12', r: '10' }],
])

export const Code = createIcon('Code', [
  ['polyline', { points: '16 18 22 12 16 6' }],
  ['polyline', { points: '8 6 2 12 8 18' }],
])

export const Code2 = createIcon('Code2', [
  ['path', { d: 'm18 16 4-4-4-4' }],
  ['path', { d: 'm6 8-4 4 4 4' }],
  ['path', { d: 'm14.5 4-5 16' }],
])

export const Database = createIcon('Database', [
  ['ellipse', { cx: '12', cy: '5', rx: '9', ry: '3' }],
  ['path', { d: 'M3 5V19A9 3 0 0 0 21 19V5' }],
  ['path', { d: 'M3 12A9 3 0 0 0 21 12' }],
])

export const Dices = createIcon('Dices', [
  ['rect', { width: '12', height: '12', x: '2', y: '10', rx: '2', ry: '2' }],
  ['path', { d: 'm17.92 14 3.5-3.5a2.24 2.24 0 0 0 0-3l-5-4.92a2.24 2.24 0 0 0-3 0L10 6' }],
  ['path', { d: 'M6 18h.01' }],
  ['path', { d: 'M10 14h.01' }],
  ['path', { d: 'M15 6h.01' }],
  ['path', { d: 'M18 9h.01' }],
])

export const Eraser = createIcon('Eraser', [
  ['path', { d: 'm7 21-4.3-4.3c-1-1-1-2.5 0-3.4l9.6-9.6c1-1 2.5-1 3.4 0l5.6 5.6c1 1 1 2.5 0 3.4L13 21' }],
  ['path', { d: 'M22 21H7' }],
  ['path', { d: 'm5 11 9 9' }],
])

export const FolderSearch = createIcon('FolderSearch', [
  ['path', { d: 'M10.7 20H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H20a2 2 0 0 1 2 2v4.1' }],
  ['path', { d: 'm21 21-1.9-1.9' }],
  ['circle', { cx: '17', cy: '17', r: '3' }],
])

export const GitBranch = createIcon('GitBranch', [
  ['line', { x1: '6', x2: '6', y1: '3', y2: '15' }],
  ['circle', { cx: '18', cy: '6', r: '3' }],
  ['circle', { cx: '6', cy: '18', r: '3' }],
  ['path', { d: 'M18 9a9 9 0 0 1-9 9' }],
])

export const Globe = createIcon('Globe', [
  ['circle', { cx: '12', cy: '12', r: '10' }],
  ['path', { d: 'M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20' }],
  ['path', { d: 'M2 12h20' }],
])

export const Hash = createIcon('Hash', [
  ['line', { x1: '4', x2: '20', y1: '9', y2: '9' }],
  ['line', { x1: '4', x2: '20', y1: '15', y2: '15' }],
  ['line', { x1: '10', x2: '8', y1: '3', y2: '21' }],
  ['line', { x1: '16', x2: '14', y1: '3', y2: '21' }],
])

export const LayoutTemplate = createIcon('LayoutTemplate', [
  ['rect', { width: '18', height: '7', x: '3', y: '3', rx: '1' }],
  ['rect', { width: '9', height: '7', x: '3', y: '14', rx: '1' }],
  ['rect', { width: '5', height: '7', x: '16', y: '14', rx: '1' }],
])

export const Lightbulb = createIcon('Lightbulb', [
  ['path', { d: 'M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5' }],
  ['path', { d: 'M9 18h6' }],
  ['path', { d: 'M10 22h4' }],
])

export const PackageCheck = createIcon('PackageCheck', [
  ['path', { d: 'm16 16 2 2 4-4' }],
  ['path', { d: 'M21 10V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l2-1.14' }],
  ['path', { d: 'm7.5 4.27 9 5.15' }],
  ['polyline', { points: '3.29 7 12 12 20.71 7' }],
  ['line', { x1: '12', x2: '12', y1: '22', y2: '12' }],
])

export const Palette = createIcon('Palette', [
  ['circle', { cx: '13.5', cy: '6.5', r: '.5', fill: 'currentColor' }],
  ['circle', { cx: '17.5', cy: '10.5', r: '.5', fill: 'currentColor' }],
  ['circle', { cx: '8.5', cy: '7.5', r: '.5', fill: 'currentColor' }],
  ['circle', { cx: '6.5', cy: '12.5', r: '.5', fill: 'currentColor' }],
  ['path', { d: 'M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z' }],
])

export const PawPrint = createIcon('PawPrint', [
  ['circle', { cx: '11', cy: '4', r: '2' }],
  ['circle', { cx: '18', cy: '8', r: '2' }],
  ['circle', { cx: '20', cy: '16', r: '2' }],
  ['path', { d: 'M9 10a5 5 0 0 1 5 5v3.5a3.5 3.5 0 0 1-6.84 1.045Q6.52 17.48 4.46 16.84A3.5 3.5 0 0 1 5.5 10Z' }],
])

export const Radio = createIcon('Radio', [
  ['path', { d: 'M4.9 19.1C1 15.2 1 8.8 4.9 4.9' }],
  ['path', { d: 'M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5' }],
  ['circle', { cx: '12', cy: '12', r: '2' }],
  ['path', { d: 'M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5' }],
  ['path', { d: 'M19.1 4.9C23 8.8 23 15.1 19.1 19' }],
])

export const Regex = createIcon('Regex', [
  ['path', { d: 'M17 3v10' }],
  ['path', { d: 'm12.67 5.5 8.66 5' }],
  ['path', { d: 'm12.67 10.5 8.66-5' }],
  ['path', { d: 'M9 17a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v2a2 2 0 0 0 2 2h2a2 2 0 0 0 2-2v-2z' }],
])

export const Rocket = createIcon('Rocket', [
  ['path', { d: 'M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z' }],
  ['path', { d: 'm12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z' }],
  ['path', { d: 'M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0' }],
  ['path', { d: 'M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5' }],
])

export const Save = createIcon('Save', [
  ['path', { d: 'M15.2 3a2 2 0 0 1 1.4.6l3.8 3.8a2 2 0 0 1 .6 1.4V19a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z' }],
  ['path', { d: 'M17 21v-7a1 1 0 0 0-1-1H8a1 1 0 0 0-1 1v7' }],
  ['path', { d: 'M7 3v4a1 1 0 0 0 1 1h7' }],
])

export const ScanText = createIcon('ScanText', [
  ['path', { d: 'M3 7V5a2 2 0 0 1 2-2h2' }],
  ['path', { d: 'M17 3h2a2 2 0 0 1 2 2v2' }],
  ['path', { d: 'M21 17v2a2 2 0 0 1-2 2h-2' }],
  ['path', { d: 'M7 21H5a2 2 0 0 1-2-2v-2' }],
  ['path', { d: 'M7 8h8' }],
  ['path', { d: 'M7 12h10' }],
  ['path', { d: 'M7 16h6' }],
])

export const ScrollText = createIcon('ScrollText', [
  ['path', { d: 'M15 12h-5' }],
  ['path', { d: 'M15 8h-5' }],
  ['path', { d: 'M19 17V5a2 2 0 0 0-2-2H4' }],
  ['path', { d: 'M8 21h12a2 2 0 0 0 2-2v-1a1 1 0 0 0-1-1H11a1 1 0 0 0-1 1v1a2 2 0 1 1-4 0V5a2 2 0 1 0-4 0v2a1 1 0 0 0 1 1h3' }],
])

export const Server = createIcon('Server', [
  ['rect', { width: '20', height: '8', x: '2', y: '2', rx: '2', ry: '2' }],
  ['rect', { width: '20', height: '8', x: '2', y: '14', rx: '2', ry: '2' }],
  ['line', { x1: '6', x2: '6.01', y1: '6', y2: '6' }],
  ['line', { x1: '6', x2: '6.01', y1: '18', y2: '18' }],
])

export const Shield = createIcon('Shield', [
  ['path', { d: 'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z' }],
])

export const Sparkles = createIcon('Sparkles', [
  ['path', { d: 'M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z' }],
  ['path', { d: 'M20 3v4' }],
  ['path', { d: 'M22 5h-4' }],
  ['path', { d: 'M4 17v2' }],
  ['path', { d: 'M5 18H3' }],
])

export const Square = createIcon('Square', [
  ['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2' }],
])

export const Table = createIcon('Table', [
  ['path', { d: 'M12 3v18' }],
  ['rect', { width: '18', height: '18', x: '3', y: '3', rx: '2' }],
  ['path', { d: 'M3 9h18' }],
  ['path', { d: 'M3 15h18' }],
])

export const Type = createIcon('Type', [
  ['polyline', { points: '4 7 4 4 20 4 20 7' }],
  ['line', { x1: '9', x2: '15', y1: '20', y2: '20' }],
  ['line', { x1: '12', x2: '12', y1: '4', y2: '20' }],
])

export const ZapOff = createIcon('ZapOff', [
  ['path', { d: 'M10.513 4.856 13.12 2.17a.5.5 0 0 1 .86.46l-1.377 4.317' }],
  ['path', { d: 'M15.656 10H20a1 1 0 0 1 .78 1.63l-1.72 1.773' }],
  ['path', { d: 'M16.273 16.273 10.88 21.83a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14H4a1 1 0 0 1-.78-1.63l4.507-4.643' }],
  ['path', { d: 'm2 2 20 20' }],
])

// P2 进度面板/任务面板专用图标（EP 库缺）
export const CircleX = createIcon('CircleX', [
  ['circle', { cx: '12', cy: '12', r: '10' }],
  ['path', { d: 'm15 9-6 6' }],
  ['path', { d: 'm9 9 6 6' }],
])

export const ListTree = createIcon('ListTree', [
  ['path', { d: 'M21 12h-3' }],
  ['path', { d: 'M14 12H7' }],
  ['path', { d: 'M5 12H3' }],
  ['path', { d: 'M9 6h6' }],
  ['path', { d: 'M11 18h6' }],
  ['path', { d: 'M11 6l-3 6' }],
  ['path', { d: 'M8 12l-3 6' }],
])

export const FilePlus2 = createIcon('FilePlus2', [
  ['path', { d: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z' }],
  ['path', { d: 'M14 2v6h6' }],
  ['path', { d: 'M9 15h6' }],
  ['path', { d: 'M12 12v6' }],
])

/** 需要导出的自绘图标清单（供 index.ts 使用）。 */
export const customIcons: Record<string, ReturnType<typeof createIcon>> = {
  Activity,
  AtSign,
  Bot,
  Braces,
  Brain,
  Calculator,
  Cat,
  Circle,
  CircleX,
  Code,
  Code2,
  Database,
  Dices,
  Eraser,
  FilePlus2,
  FolderSearch,
  GitBranch,
  Globe,
  Hash,
  LayoutTemplate,
  Lightbulb,
  ListTree,
  PackageCheck,
  Palette,
  PawPrint,
  Radio,
  Regex,
  Rocket,
  Save,
  ScanText,
  ScrollText,
  Server,
  Shield,
  Sparkles,
  Square,
  Table,
  Type,
  ZapOff,
}
