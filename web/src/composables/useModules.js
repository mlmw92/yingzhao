import { ref } from 'vue'
import http from '../api/http'

// 模块级单例：侧栏、路由守卫、命令面板、设置页共用同一份关闭清单，
// 避免各自拉一次接口后出现「菜单隐藏了但深链还能进」。
const disabled = ref([])
const loaded = ref(false)

let inflight = null

export function useModules() {
  async function load(force = false) {
    if (loaded.value && !force) return
    if (inflight) return inflight
    inflight = http
      .get('/api/v1/ui/settings')
      .then((cfg) => {
        disabled.value = cfg.disabled || []
        loaded.value = true
      })
      .catch(() => {
        // 拉取失败按全开处理：宁可多显示，也不要把功能藏起来
        disabled.value = []
      })
      .finally(() => {
        inflight = null
      })
    return inflight
  }

  function isDisabled(key) {
    return !!key && disabled.value.includes(key)
  }

  // 页面与所在域任一被关即不可见
  function isVisible(pageKey, domainKey) {
    return !isDisabled(pageKey) && !isDisabled(domainKey)
  }

  return { disabled, loaded, load, isDisabled, isVisible }
}
