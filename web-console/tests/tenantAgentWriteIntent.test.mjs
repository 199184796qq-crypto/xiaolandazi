import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'

let server
let isTenantMaintenanceWriteIntent

before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  const mod = await server.ssrLoadModule('/src/tenantAgentWriteIntent.ts')
  isTenantMaintenanceWriteIntent = mod.isTenantMaintenanceWriteIntent
})

after(async () => { await server?.close() })

test('识别租户事实/福利/商品/话术/方案写命令', () => {
  for (const value of [
    '帮我加一条安徽包邮',
    '把安微包邮加进方案',
    '新增一条事实',
    '修改产地这条事实',
    '停用这个福利',
    '把2号链接的包邮改成不包邮',
    '删除3号链接',
    '加一条话术参考',
    '切换到另一个方案',
  ]) {
    assert.equal(isTenantMaintenanceWriteIntent(value), true, value)
  }
})

test('普通问答和导航不进入写链路', () => {
  for (const value of [
    '今天直播怎么样',
    '我的方案有哪些事实',
    '打开方案页面',
    '安徽包邮吗',
    '这个系统能做什么',
    '',
  ]) {
    assert.equal(isTenantMaintenanceWriteIntent(value), false, value)
  }
})
