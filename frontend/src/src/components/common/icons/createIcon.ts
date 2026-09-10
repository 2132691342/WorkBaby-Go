import { defineComponent, h } from 'vue'

/** SVG 节点：标签名 + 属性。与 lucide createLucideIcon 数据结构一致。 */
export type IconNode = readonly [string, Record<string, string | number | undefined>]

/**
 * 自绘图标工厂：补足组件库缺失的图标（Bot / Brain / Sparkles / GitBranch 等）。
 * props 与 lucide 一致（size / color / stroke-width）；24 viewBox 线性风格，与组件库图标同族。
 */
export function createIcon(name: string, nodes: readonly IconNode[]) {
  return defineComponent({
    name,
    inheritAttrs: false,
    props: {
      size: { type: [Number, String], default: 24 },
      color: { type: String, default: 'currentColor' },
      strokeWidth: { type: [Number, String], default: 2 },
      absoluteStrokeWidth: { type: Boolean, default: false },
    },
    setup(props, { attrs, slots }) {
      return () =>
        h(
          'svg',
          {
            xmlns: 'http://www.w3.org/2000/svg',
            width: props.size,
            height: props.size,
            viewBox: '0 0 24 24',
            fill: 'none',
            stroke: props.color,
            'stroke-width': props.absoluteStrokeWidth
              ? (Number(props.strokeWidth) * 24) / Number(props.size)
              : props.strokeWidth,
            'stroke-linecap': 'round',
            'stroke-linejoin': 'round',
            ...attrs,
          },
          [...nodes.map(([tag, attrs]) => h(tag, attrs)), ...(slots.default ? [slots.default()] : [])],
        )
    },
  })
}
