// 中间件监控页「展示类型」弹窗：勾选项必须真的渲染出来。
//
// 缺陷现场：标签映射引用了一个**不存在**的对象（builtinSpecs），渲染期抛 ReferenceError，
// 整个勾选列表渲染不出来 —— 弹窗只剩标题、说明和三个按钮，看起来像"没有类型可选"。
// 这类"静默空白"只能靠断言弹窗里真的有勾选项来兜住。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import http from '../api/http'
import MiddlewareView from './MiddlewareView.vue'

vi.mock('../api/http', () => ({ default: { get: vi.fn(), put: vi.fn(), post: vi.fn() } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))

const CONFIG = {
  available: ['redis', 'postgres', 'zookeeper'],
  labels: { redis: 'Redis', postgres: 'PostgreSQL', zookeeper: 'ZooKeeper' },
  enabled: [],
}

// 各 Tab 都是异步组件（会各自拉数据），这里一律替换掉，只测弹窗本身
const TAB_STUBS = {}
for (const name of [
  'RedisTab', 'MySQLTab', 'PostgresTab', 'NginxTab', 'KafkaTab', 'DockerTab', 'RocketMQTab',
  'K8sTab', 'MongoTab', 'FastDFSTab', 'RabbitMQTab', 'ElasticsearchTab', 'ClickHouseTab',
  'NacosTab', 'ZooKeeperTab',
]) {
  TAB_STUBS[name] = { template: '<div />' }
}

describe('中间件监控「展示类型」弹窗', () => {
  let wrapper

  beforeEach(() => {
    vi.clearAllMocks()
    http.get.mockResolvedValue(CONFIG)
    http.put.mockResolvedValue(CONFIG)
  })

  afterEach(() => {
    if (wrapper) wrapper.unmount()
    wrapper = undefined
    document.body.innerHTML = ''
  })

  async function openDialog() {
    wrapper = mount(MiddlewareView, {
      global: { plugins: [ElementPlus], stubs: TAB_STUBS },
      attachTo: document.body,
    })
    await flushPromises()
    const btn = wrapper.findAll('button').find((b) => b.text().includes('展示类型'))
    expect(btn).toBeDefined()
    await btn.trigger('click')
    await flushPromises()
  }

  it('列出全部类型，且用服务端给的展示名', async () => {
    await openDialog()

    const dialog = document.querySelector('.el-dialog')
    expect(dialog).toBeTruthy()
    const boxes = dialog.querySelectorAll('.el-checkbox')
    expect(boxes).toHaveLength(3)

    const text = dialog.textContent
    expect(text).toContain('Redis')
    expect(text).toContain('PostgreSQL')
    expect(text).toContain('ZooKeeper')
  })

  it('勾选后保存，提交选中的类型 key', async () => {
    await openDialog()

    const boxes = document.querySelectorAll('.el-dialog .el-checkbox')
    boxes[0].click()
    await flushPromises()

    const save = wrapper.findAll('button').find((b) => b.text().includes('保存'))
    await save.trigger('click')
    await flushPromises()

    expect(http.put).toHaveBeenCalledWith('/api/v1/middleware/view-config', { enabled: ['redis'] })
  })

  it('「恢复全部」清空勾选并保存（空清单 = 展示全部）', async () => {
    await openDialog()

    document.querySelector('.el-dialog .el-checkbox').click()
    await flushPromises()

    const restore = wrapper.findAll('button').find((b) => b.text().includes('恢复全部'))
    await restore.trigger('click')
    await flushPromises()

    expect(http.put).toHaveBeenCalledWith('/api/v1/middleware/view-config', { enabled: [] })
  })
})
