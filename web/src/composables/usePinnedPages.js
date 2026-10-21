import { ref } from 'vue'

const KEY = 'yingzhao-pinned-pages'
const MAX = 5

function load() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || '[]')
    return Array.isArray(raw) ? raw.slice(0, MAX) : []
  } catch {
    return []
  }
}

// 常用置顶：存 localStorage，与首页布局同一取向（不新增接口）。
// 多设备同步需要进 users.yaml，属后续批次。
export function usePinnedPages() {
  const pinned = ref(load())

  function persist() {
    try {
      localStorage.setItem(KEY, JSON.stringify(pinned.value))
    } catch {
      /* ignore */
    }
  }

  function isPinned(key) {
    return pinned.value.includes(key)
  }

  function toggle(key) {
    if (!key) return
    pinned.value = isPinned(key)
      ? pinned.value.filter((k) => k !== key)
      : [...pinned.value, key].slice(-MAX)
    persist()
  }

  return { pinned, isPinned, toggle }
}
