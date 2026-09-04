import { defineComponent, h } from 'vue'

/** SVG 节点：标签名 + 属性。与 lucide createLucideIcon 数据结构一致。 */
export type IconNode = readonly [string, Record<string, string | number | undefined>]

/**
 * 自绘图标工厂：行为与 lucide-vue-next 完全一致（size / color / stroke-width props），
 * 替代 EP 图标库缺失的图标（Bot / Brain / Sparkles / PawPrint / Rocket / GitBranch 等）。
 *
 * <p>设计说明（图标统一）：
 * <ul>
 *   <li>EP 图标库（@element-plus/icons-vue）缺的图标统一在此自绘，模板用法与 lucide 兼容
 *       （`&lt;Xxx :size="14" color="var(--x)" /&gt;`、`&lt;el-icon&gt;&lt;Xxx /&gt;&lt;/el-icon&gt;`）；</li>
 *   <li>svg 默认 24 viewBox + stroke 线性风格，与 EP 线性图标（Arrow/Check/Close 等）视觉同族；</li>
 *   <li>`absoluteStrokeWidth` 与 lucide 语义一致：传 true 时按 viewBox 换算实际 stroke 宽度。</li>
 * </ul>
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
