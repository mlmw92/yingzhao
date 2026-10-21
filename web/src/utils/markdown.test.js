import { describe, it, expect } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('渲染标题、粗体与分隔线', () => {
    const html = renderMarkdown('**排查思路**\n\n### 结论\n\n---\n\n正常')
    expect(html).toContain('<strong>排查思路</strong>')
    expect(html).toContain('<h4>结论</h4>')
    expect(html).toContain('<hr>')
    expect(html).toContain('<p>正常</p>')
  })

  it('把连续列表项收进同一个列表', () => {
    const html = renderMarkdown('- 第一项\n- 第二项\n\n1. 有序一\n2. 有序二')
    expect(html).toContain('<ul><li>第一项</li><li>第二项</li></ul>')
    expect(html).toContain('<ol><li>有序一</li><li>有序二</li></ol>')
  })

  it('行内代码与粗体可以混排', () => {
    const html = renderMarkdown('看 `cpu_usage` 的 **趋势**')
    expect(html).toContain('<code>cpu_usage</code>')
    expect(html).toContain('<strong>趋势</strong>')
  })

  // 模型输出属于不可信内容：先转义再注入自己的标签，绝不能让原文里的 HTML 生效。
  it('转义模型输出里的 HTML，不执行脚本', () => {
    const html = renderMarkdown('<script>alert(1)</script>\n<img src=x onerror=alert(1)>')
    expect(html).not.toContain('<script>')
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;script&gt;')
    expect(html).toContain('&lt;img')
  })

  it('空内容返回空串', () => {
    expect(renderMarkdown('')).toBe('')
    expect(renderMarkdown(null)).toBe('')
  })
})
