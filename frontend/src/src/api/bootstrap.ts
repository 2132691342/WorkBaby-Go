import { initApi, apiGet } from './http'
import { UI_API_CONTRACT_VERSION } from './contract'

/**
 * 双主机启动引导：从 Wails `app:ready` 事件拿到 serverPort 并初始化 HTTP 层，
 * 随后校验前后端契约版本。
 * 降级顺序：Wails 事件 → localStorage('wb.serverPort') → 抛错（App.vue 显示「后端未就绪」）。
 */
export async function bootstrapServer(): Promise<void> {
  // 1) Wails 事件（桌面端）：等 app:ready 携带 serverPort
  try {
    const rt = await import('@/wailsjs/runtime/runtime')
    await new Promise<void>((resolve, reject) => {
      const off = rt.EventsOn('app:ready', (payload: Record<string, unknown>) => {
        const port = payload?.server_port as number | undefined
        if (typeof port === 'number' && port > 0) {
          off()
          void initAndVerify(port).then(resolve, reject)
        }
      })
      // 兜底：3s 内没等到事件 → 尝试 localStorage
      setTimeout(() => {
        const cached = localStorage.getItem('wb.serverPort')
        if (cached) {
          off()
          void initAndVerify(cached).then(resolve, reject)
          return
        }
        off()
        resolve()
      }, 3000)
    })
    return
  } catch {
    // 无 wails 注入（纯浏览器 dev）：直接读 localStorage
  }

  // 2) localStorage 兜底
  const cached = localStorage.getItem('wb.serverPort')
  if (cached) {
    await initAndVerify(cached)
    return
  }
  throw new Error('server not ready: missing serverPort')
}

function initAndVerify(port: string | number): Promise<void> {
  initApi(port)
  return checkContract()
}

/** 契约版本比对：后端 meta/contract 与前端常量不一致时显式抛错，避免全站静默 404。 */
async function checkContract(): Promise<void> {
  let server = UI_API_CONTRACT_VERSION
  try {
    server = await apiGet<number>('/api/v1/meta/contract')
  } catch {
    return // 后端不可达由后续业务调用暴露，此处不阻断
  }
  if (server !== UI_API_CONTRACT_VERSION) {
    throw new Error(
      `[contract] 前后端契约版本不一致：后端 v${server} / 前端 v${UI_API_CONTRACT_VERSION}。请同步升级后重启。`
    )
  }
}
