// 部署后旧页面加载旧 chunk 会 404：浏览器里跑的是上一版的 JS，
// 它引用的 /assets/xxx-旧hash.js 已被新部署删除。
// 这里在路由层捕获这类错误并自动刷新一次；用时间窗防止反复刷新。

const RELOAD_KEY = 'chunk-reload-at'

export function isChunkLoadError(err) {
  const msg = String((err && err.message) || err || '')
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS/i.test(msg)
}

export function installChunkReload(router, opts = {}) {
  const reload = opts.reload || (() => window.location.reload())
  const now = opts.now || (() => Date.now())
  const storage = opts.storage || window.sessionStorage
  const windowMs = opts.windowMs || 10000

  function recover(err) {
    if (!isChunkLoadError(err)) return
    const last = Number(storage.getItem(RELOAD_KEY) || 0)
    // last 为 0 表示本次会话还没刷过：不能因为 now 小于时间窗就拒绝第一次恢复
    if (last > 0 && now() - last < windowMs) {
      // 刚刷过又失败：说明不是版本切换，别再循环刷新
      return
    }
    storage.setItem(RELOAD_KEY, String(now()))
    reload()
  }

  // 路由懒加载失败
  router.onError(recover)
  // Vite 预加载资源失败（CSS/依赖模块）
  window.addEventListener('vite:preloadError', (e) => {
    if (e && typeof e.preventDefault === 'function') e.preventDefault()
    recover(e && e.payload)
  })
}
