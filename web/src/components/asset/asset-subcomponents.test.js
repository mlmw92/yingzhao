// 批次 12：资产台账拆出的子组件契约。
//
// 为什么要有这个文件：台账从单文件（2217 行）拆成「KPI 行 / 筛选面板 / 主表格 / 详情抽屉 /
// 弹窗集合 / 属性编辑器」后，各块之间的**边界**成了新的出错点——KPI 卡不再是可点的 div、
// 筛选条要把下钻条件也汇成可撤销的 chips、抽屉三个 Tab 各自四态、行内「更多」不再含彻底删除。
// 这些行为改坏了不会报错，只会悄悄退化，因此逐条钉住。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

// 这些子组件经 useAssetTypes 间接触发 getAssetTypes；测试里不真正打网络。
vi.mock('../../api/asset', () => ({
  getAssetTypes: vi.fn(() => Promise.resolve({ types: [] })),
  listAssets: vi.fn(() => Promise.resolve({ assets: [], total: 0 })),
}))

import AssetKpiRow from './AssetKpiRow.vue'
import AssetFilterPanel from './AssetFilterPanel.vue'
import AssetTable from './AssetTable.vue'
import AssetDetailDrawer from './AssetDetailDrawer.vue'
import AssetModals from './AssetModals.vue'
import AssetAttrEditor from './AssetAttrEditor.vue'

// jsdom 未实现 ResizeObserver，Element Plus 的表格/弹窗会用到它。
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// 抽屉/弹窗默认 teleport 到 body：改成本地渲染，断言落在 wrapper 上更好写。
// 同时把 #footer 具名插槽也渲染出来（弹窗的操作按钮都在里面）。
const PassthroughStub = {
  props: ['modelValue', 'title'],
  template: '<div class="stub"><slot /><slot name="footer" /></div>',
}
const globalOpts = {
  plugins: [ElementPlus],
  stubs: {
    'el-drawer': PassthroughStub,
    'el-dialog': PassthroughStub,
    // AI 诊断面板会打接口，这里不关心它
    AiDiagnosePanel: { template: '<div class="ai-stub" />' },
  },
}

let wrappers = []
function track(w) {
  wrappers.push(w)
  return w
}
// el-table 在 mount 时才注册列并渲染行体：等一个 tick 再断言行内容
async function mountReady(comp, opts) {
  const w = track(mount(comp, opts))
  await flushPromises()
  return w
}
beforeEach(() => {
  wrappers = []
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  document.body.innerHTML = ''
})

const SUMMARY = { total: 12, missing: 3, noOwner: 2, conflict: 1, changes: 5, ignored: 4 }

describe('AssetKpiRow', () => {
  it('可下钻的 5 张卡是真按钮，第 6 张（近 7 天变更）不是', () => {
    const w = track(mount(AssetKpiRow, { props: { summary: SUMMARY }, global: globalOpts }))
    expect(w.findAll('button.kpi-click')).toHaveLength(5)
    expect(w.find('div.kpi-click.kpi-static').exists()).toBe(true)
    // 静态卡不应该是按钮（否则键盘/读屏会给出一个点了没反应的操作）
    expect(w.findAll('button').some((b) => b.text().includes('近 7 天变更'))).toBe(false)
  })

  it('点卡片 emit 对应的下钻事件（失联带 missing 参数）', async () => {
    const w = track(mount(AssetKpiRow, { props: { summary: SUMMARY }, global: globalOpts }))
    const btns = w.findAll('button.kpi-click')
    await btns[0].trigger('click')
    expect(w.emitted('drill-all')).toHaveLength(1)
    await btns[1].trigger('click')
    expect(w.emitted('drill-status')[0]).toEqual(['missing'])
    await btns[2].trigger('click')
    expect(w.emitted('drill-no-owner')).toHaveLength(1)
    await btns[3].trigger('click')
    expect(w.emitted('drill-conflict')).toHaveLength(1)
    await btns[4].trigger('click')
    expect(w.emitted('drill-ignored')).toHaveLength(1)
  })
})

describe('AssetFilterPanel', () => {
  const baseFilter = { type: '', status: '', source: '', node: '', keyword: '', label: '', ignored: '' }
  const baseDrill = { ownerMissing: false, conflict: false, ignored: false }
  const types = [{ key: 'host', title: '主机' }]

  it('把 filter 与 drill 的生效条件都汇成 chips，key 带维度前缀', () => {
    const w = track(mount(AssetFilterPanel, {
      props: {
        filter: { ...baseFilter, type: 'host', keyword: 'web' },
        drill: { ...baseDrill, ownerMissing: true },
        assetTypes: types,
      },
      global: globalOpts,
    }))
    expect(w.findAll('.fc-chip')).toHaveLength(3)
    expect(w.text()).toContain('类型：主机')
    expect(w.text()).toContain('关键词：web')
    expect(w.text()).toContain('下钻：无责任人')
  })

  it('撤销某条 chip 时按带前缀的 key 精确上报', async () => {
    const w = track(mount(AssetFilterPanel, {
      props: {
        filter: { ...baseFilter, type: 'host' },
        drill: { ...baseDrill, conflict: true },
        assetTypes: types,
      },
      global: globalOpts,
    }))
    await w.findAll('.fc-x')[1].trigger('click')
    expect(w.emitted('remove-chip')[0]).toEqual(['drill.conflict'])
  })

  it('清空 chips emit reset', async () => {
    const w = track(mount(AssetFilterPanel, {
      props: { filter: { ...baseFilter, node: 'web-01' }, drill: baseDrill, assetTypes: types },
      global: globalOpts,
    }))
    await w.find('.fc-clear').trigger('click')
    expect(w.emitted('reset')).toHaveLength(1)
  })

  it('「更多筛选」默认收起，点击后展开（来源/归属节点/标签/含已忽略）', async () => {
    const w = track(mount(AssetFilterPanel, {
      props: { filter: baseFilter, drill: baseDrill, assetTypes: types },
      global: globalOpts,
    }))
    expect(w.find('.more').isVisible()).toBe(false)
    await w.findAll('button').find((b) => b.text().includes('更多筛选')).trigger('click')
    expect(w.find('.more').isVisible()).toBe(true)
  })

  it('「含已忽略」勾选经 update:showIgnored 上报', async () => {
    const w = track(mount(AssetFilterPanel, {
      props: { filter: baseFilter, drill: baseDrill, assetTypes: types, showIgnored: false },
      global: globalOpts,
    }))
    w.findComponent({ name: 'ElCheckbox' }).vm.$emit('update:modelValue', true)
    await flushPromises()
    expect(w.emitted('update:showIgnored')[0]).toEqual([true])
  })
})

describe('AssetTable', () => {
  const ITEM = {
    id: 1, typeKey: 'host', naturalKey: 'web-01', name: 'web-01', node: 'web-01',
    status: 'online', source: 'auto', owner: '', ignored: false, values: {}, labels: { env: 'prod' },
  }

  it('名称/副标题/标签/状态/来源都在行里', async () => {
    const w = await mountReady(AssetTable, {
      props: { items: [ITEM], canWrite: true, emptyTitle: '空', emptyHints: [] },
      global: globalOpts,
    })
    expect(w.text()).toContain('web-01')
    expect(w.text()).toContain('env:prod')
    // 状态与来源都走 StatusPill 的文本
    expect(w.text()).toContain('上报正常')
    expect(w.text()).toContain('自动')
  })

  it('点「详情」emit detail（带整行）', async () => {
    const w = await mountReady(AssetTable, {
      props: { items: [ITEM], canWrite: true }, global: globalOpts,
    })
    await w.findAll('button').find((b) => b.text() === '详情').trigger('click')
    expect(w.emitted('detail')[0][0].id).toBe(1)
  })

  it('canWrite=false 时不出现「编辑」与「更多」', async () => {
    const w = await mountReady(AssetTable, {
      props: { items: [ITEM], canWrite: false }, global: globalOpts,
    })
    expect(w.findAll('button').some((b) => b.text() === '编辑')).toBe(false)
    expect(w.findAll('button').some((b) => b.text().includes('更多'))).toBe(false)
  })

  it('行内「更多」只剩忽略/恢复，彻底删除不在其中（已移到抽屉危险区）', async () => {
    const w = await mountReady(AssetTable, {
      props: { items: [ITEM], canWrite: true }, global: globalOpts,
    })
    const more = w.findAll('button').find((b) => b.text().includes('更多'))
    await more.trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('忽略（从台账隐藏）')
    expect(document.body.textContent).not.toContain('彻底删除')
  })

  it('已忽略的行「更多」给的是恢复', async () => {
    const w = await mountReady(AssetTable, {
      props: { items: [{ ...ITEM, ignored: true }], canWrite: true }, global: globalOpts,
    })
    await w.findAll('button').find((b) => b.text().includes('更多')).trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('恢复（重新纳入台账）')
  })
})

describe('AssetDetailDrawer', () => {
  const DETAIL = {
    id: 1, typeKey: 'host', naturalKey: 'web-01', name: 'web-01', node: 'web-01',
    status: 'online', owner: '', ignored: false, labels: {}, attrs: [], conflictCount: 0,
    hasDiscovery: false, container: null,
  }

  it('三个 Tab 各自四态：错误条与重试按钮各自独立上报', async () => {
    const w = track(mount(AssetDetailDrawer, {
      props: {
        modelValue: true, detail: DETAIL, canWrite: true,
        attrError: '属性挂了', historyError: '历史挂了', linksError: '关系挂了',
      },
      global: globalOpts,
    }))
    const errs = w.findAll('.sec-error')
    expect(errs).toHaveLength(3)
    const retries = w.findAll('.sec-error button')
    await retries[0].trigger('click')
    await retries[1].trigger('click')
    await retries[2].trigger('click')
    expect(w.emitted('retry-attr')).toHaveLength(1)
    expect(w.emitted('retry-history')).toHaveLength(1)
    expect(w.emitted('retry-links')).toHaveLength(1)
  })

  it('纯人工资产给「彻底删除」，采集资产只给说明', () => {
    const a = track(mount(AssetDetailDrawer, {
      props: { modelValue: true, detail: { ...DETAIL, hasDiscovery: false }, canWrite: true },
      global: globalOpts,
    }))
    expect(a.text()).toContain('彻底删除该资产')

    const b = track(mount(AssetDetailDrawer, {
      props: { modelValue: true, detail: { ...DETAIL, hasDiscovery: true }, canWrite: true },
      global: globalOpts,
    }))
    expect(b.text()).not.toContain('彻底删除该资产')
    expect(b.text()).toContain('请用「忽略」')
  })

  it('冲突字段的「恢复采集值」上报字段名', async () => {
    const detail = {
      ...DETAIL, conflictCount: 1,
      attrs: [
        { key: 'cpuCores', value: '4', source: 'discovery' },
        { key: 'cpuCores', value: '8', source: 'manual' },
      ],
    }
    const w = await mountReady(AssetDetailDrawer, {
      props: { modelValue: true, detail, canWrite: true }, global: globalOpts,
    })
    await w.findAll('button').find((b) => b.text().includes('恢复采集值')).trigger('click')
    expect(w.emitted('restore-attr')[0]).toEqual(['cpuCores'])
  })

  it('关联关系页签的「关系图」/「添加关系」上报对应事件', async () => {
    const w = track(mount(AssetDetailDrawer, {
      props: { modelValue: true, detail: DETAIL, canWrite: true }, global: globalOpts,
    }))
    await w.findAll('button').find((b) => b.text().includes('关系图')).trigger('click')
    expect(w.emitted('open-topology')).toHaveLength(1)
    await w.findAll('button').find((b) => b.text().includes('添加关系')).trigger('click')
    expect(w.emitted('add-link')).toHaveLength(1)
  })
})

describe('AssetModals', () => {
  const baseProps = {
    form: { typeKey: 'host', naturalKey: '', name: '', node: '' },
    editAttrs: [{ key: '', value: '' }],
    editLabels: [],
    linkForm: { direction: 'out', kind: 'runs_on', peer: '' },
    batchOwner: { visible: false, owner: '' },
    batchLabel: { visible: false, key: '', value: '', mode: 'set' },
  }

  it('新建弹窗「提交」上报 submit-create', async () => {
    const w = track(mount(AssetModals, {
      props: { ...baseProps, createVisible: true }, global: globalOpts,
    }))
    await w.findAll('button').find((b) => b.text() === '提交').trigger('click')
    expect(w.emitted('submit-create')).toHaveLength(1)
  })

  it('批量转派「清空责任人」上报 submit-batch-owner(true)', async () => {
    const w = track(mount(AssetModals, {
      props: { ...baseProps, batchOwner: { visible: true, owner: '' }, selectionCount: 3 },
      global: globalOpts,
    }))
    await w.findAll('button').find((b) => b.text() === '清空责任人').trigger('click')
    expect(w.emitted('submit-batch-owner')[0]).toEqual([true])
  })

  it('批量结果面板逐条列出未成功项', () => {
    const w = track(mount(AssetModals, {
      props: {
        ...baseProps, batchResultVisible: true,
        batchResult: {
          opLabel: '转派责任人', total: 3, ok: 1, failed: 2,
          items: [
            { id: 1, ok: true },
            { id: 2, ok: false, node: 'web-02', name: 'db-02', error: '范围外' },
            { id: 3, ok: false, node: 'web-03', error: '不存在' },
          ],
        },
      },
      global: globalOpts,
    }))
    expect(w.text()).toContain('成功 1 条，失败 2 条')
    expect(w.text()).toContain('范围外')
    expect(w.text()).toContain('不存在')
    expect(w.findAll('.batch-row')).toHaveLength(2)
  })

  it('添加关系「确定」上报 submit-link-add', async () => {
    const w = track(mount(AssetModals, {
      props: { ...baseProps, linkAddVisible: true }, global: globalOpts,
    }))
    await w.findAll('button').find((b) => b.text() === '确定').trigger('click')
    expect(w.emitted('submit-link-add')).toHaveLength(1)
  })
})

describe('AssetAttrEditor', () => {
  it('按字典把值控件换成枚举下拉', () => {
    const w = track(mount(AssetAttrEditor, {
      props: {
        rows: [{ key: 'role', value: '' }],
        dict: { role: { kind: 'enum', enum: [{ value: 'master' }, { value: 'replica' }] } },
      },
      global: globalOpts,
    }))
    expect(w.find('.el-select').exists()).toBe(true)
    expect(w.find('.el-input').exists()).toBe(true) // 键仍是自由文本
  })

  it('字典没配的字段仍是自由文本输入', () => {
    const w = track(mount(AssetAttrEditor, {
      props: { rows: [{ key: 'foo', value: '' }], dict: {} },
      global: globalOpts,
    }))
    expect(w.find('.el-select').exists()).toBe(false)
  })

  it('增删行就地改数组（父组件持有）', async () => {
    const rows = [{ key: 'a', value: '1' }]
    const w = track(mount(AssetAttrEditor, { props: { rows, dict: {} }, global: globalOpts }))
    await w.findAll('button').find((b) => b.text().includes('添加属性')).trigger('click')
    expect(rows).toHaveLength(2)
    await w.findAll('button').find((b) => b.text() === '删除').trigger('click')
    expect(rows).toHaveLength(1)
  })
})
