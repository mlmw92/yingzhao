// 极简 Markdown 渲染：只支持模型实际会用的子集（标题 / 粗体 / 列表 / 行内代码 / 分隔线）。
//
// 为什么不用 marked + DOMPurify：这里渲染的是**模型输出**，属于不可信内容。
// 引入完整解析器还要再配一套 sanitizer；而我们的需求只有上面这几样。
// 先转义 HTML、再注入自己生成的标签，是这里唯一需要守住的安全边界。

const ESCAPE = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ESCAPE[c])
}

// 行内格式：输入已经转义过，这里只处理 **粗体** 与 `代码`。
function inline(s) {
  return s.replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
}

export function renderMarkdown(text) {
  const src = escapeHtml(text || '')
  const lines = src.split(/\r?\n/)
  const out = []
  let list = null // 'ul' | 'ol'

  const closeList = () => {
    if (list) {
      out.push(`</${list}>`)
      list = null
    }
  }

  for (const raw of lines) {
    const line = raw.trimEnd()
    if (!line.trim()) {
      closeList()
      continue
    }

    if (/^\s*---+\s*$/.test(line)) {
      closeList()
      out.push('<hr>')
      continue
    }

    const heading = line.match(/^\s*#{1,6}\s+(.*)$/)
    if (heading) {
      closeList()
      out.push(`<h4>${inline(heading[1])}</h4>`)
      continue
    }

    const ul = line.match(/^\s*[-*+]\s+(.*)$/)
    if (ul) {
      if (list !== 'ul') {
        closeList()
        out.push('<ul>')
        list = 'ul'
      }
      out.push(`<li>${inline(ul[1])}</li>`)
      continue
    }

    const ol = line.match(/^\s*\d+[.)]\s+(.*)$/)
    if (ol) {
      if (list !== 'ol') {
        closeList()
        out.push('<ol>')
        list = 'ol'
      }
      out.push(`<li>${inline(ol[1])}</li>`)
      continue
    }

    closeList()
    out.push(`<p>${inline(line)}</p>`)
  }
  closeList()
  return out.join('')
}
