import { describe, it, expect, beforeEach } from 'vitest'
import { usePinnedPages } from './usePinnedPages'

beforeEach(() => localStorage.clear())

describe('usePinnedPages', () => {
  it('最多保留 5 个，重复点击取消置顶', () => {
    const p = usePinnedPages()
    for (const k of ['a', 'b', 'c', 'd', 'e', 'f']) p.toggle(k)
    expect(p.pinned.value).toHaveLength(5)
    // 超出上限时最早的一个（a）已被挤掉，取消置顶要用仍在列表里的 b
    p.toggle('b')
    expect(p.isPinned('b')).toBe(false)
  })

  it('置顶列表持久化到 localStorage', () => {
    const p = usePinnedPages()
    p.toggle('logs')
    expect(JSON.parse(localStorage.getItem('yingzhao-pinned-pages'))).toContain('logs')
  })
})
