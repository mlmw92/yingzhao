// 危险参数判定的前端用例。
//
// 只测 `dangerFindings` 这一小块：它是确认框在**提交前**给提示的依据。
// 权威判定在服务端（approvalGate），这里若与它不一致，失败方向是良性的
// （漏报 → 服务端照样拦/留痕；多报 → 多一句提示），但**清单必须来自服务端**，
// 所以这里也顺带钉住"值缺失时不误报"。
import { describe, it, expect } from 'vitest'
import { dangerFindings } from './ops'

// 与 model.DangerRules() 下发的形状一致
const RULES = [
  {
    rule: 'push-persistence-dir', kind: 'file.push', param: 'path',
    values: ['/etc/systemd/system', '/etc/cron.d', '/root/.ssh'], exact: false,
    reason: '该目录里的文件会被系统自己执行',
  },
  {
    rule: 'push-sensitive-file', kind: 'file.push', param: 'path',
    values: ['/etc/passwd', '/etc/shadow'], exact: true,
    reason: '覆盖它会直接改变谁能登进这台机器',
  },
  {
    rule: 'restart-critical-unit', kind: 'svc.restart', param: 'unit',
    values: ['sshd.service', 'systemd-networkd.service'], exact: true,
    reason: '重启它可能让这台机器失联',
  },
]

describe('dangerFindings', () => {
  it('目录前缀命中（写系统会自执行的目录）', () => {
    const got = dangerFindings(RULES, 'file.push', { path: '/etc/systemd/system/backdoor.service' })
    expect(got.map((f) => f.rule)).toEqual(['push-persistence-dir'])
    expect(got[0].reason).toContain('系统自己执行')
  })

  it('精确命中敏感文件；同一条可能命中多条', () => {
    const got = dangerFindings(RULES, 'file.push', { path: '/root/.ssh/authorized_keys' })
    expect(got.map((f) => f.rule).sort()).toEqual(['push-persistence-dir'])
    const two = dangerFindings(RULES, 'file.push', { path: '/etc/passwd' })
    expect(two.map((f) => f.rule)).toEqual(['push-sensitive-file'])
  })

  it('关键单元精确命中', () => {
    const got = dangerFindings(RULES, 'svc.restart', { unit: 'sshd.service' })
    expect(got.length).toBe(1)
  })

  it('普通参数不误报（否则每次重启都弹警告，最后没人看）', () => {
    expect(dangerFindings(RULES, 'file.push', { path: '/opt/app/conf/app.conf' })).toEqual([])
    expect(dangerFindings(RULES, 'file.push', { path: '/etc/systemd/systemd-foo/x' })).toEqual([])
    expect(dangerFindings(RULES, 'svc.restart', { unit: 'nginx.service' })).toEqual([])
  })

  it('规则清单缺失或参数为空时不误报、不抛错', () => {
    expect(dangerFindings(undefined, 'file.push', { path: '/etc/passwd' })).toEqual([])
    expect(dangerFindings(RULES, 'file.push', {})).toEqual([])
    expect(dangerFindings(RULES, 'file.push', { path: '   ' })).toEqual([])
    // 动作不匹配的规则不参与
    expect(dangerFindings(RULES, 'svc.status', { unit: 'sshd.service' })).toEqual([])
  })
})
