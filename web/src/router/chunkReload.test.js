import { describe, it, expect, vi } from 'vitest'
import { isChunkLoadError, installChunkReload } from './chunkReload'

function fakeRouter() {
  const handlers = []
  return { onError: (fn) => handlers.push(fn), fire: (e) => handlers.forEach((h) => h(e)) }
}

function fakeStorage() {
  const m = new Map()
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v) }
}

describe('chunk 加载失败恢复', () => {
  it('识别动态导入失败', () => {
    expect(isChunkLoadError(new Error('Failed to fetch dynamically imported module: http://x/assets/A.js'))).toBe(true)
    expect(isChunkLoadError(new Error('Importing a module script failed.'))).toBe(true)
    expect(isChunkLoadError(new Error('network down'))).toBe(false)
  })

  it('第一次失败刷新，时间窗内不重复刷新', () => {
    const router = fakeRouter()
    const storage = fakeStorage()
    const reload = vi.fn()
    let t = 1000
    installChunkReload(router, { reload, storage, now: () => t })

    router.fire(new Error('Failed to fetch dynamically imported module: /assets/A.js'))
    expect(reload).toHaveBeenCalledTimes(1)

    t += 3000
    router.fire(new Error('Failed to fetch dynamically imported module: /assets/A.js'))
    expect(reload).toHaveBeenCalledTimes(1)

    t += 20000
    router.fire(new Error('Failed to fetch dynamically imported module: /assets/B.js'))
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('非 chunk 错误不刷新', () => {
    const router = fakeRouter()
    const reload = vi.fn()
    installChunkReload(router, { reload, storage: fakeStorage() })
    router.fire(new Error('request failed'))
    expect(reload).not.toHaveBeenCalled()
  })
})
