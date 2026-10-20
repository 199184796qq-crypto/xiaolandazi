import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'

const require = createRequire(process.env.PLAN_TEST_RUNTIME || new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const output = process.env.PLAN_TEST_OUTPUT || '../deploy/validation/plan-create-dialog'
const fixtureId = fileURLToPath(new URL('../src/__plan-create-fixture.vue', import.meta.url)).replaceAll('\\', '/')
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const script = source.slice(source.indexOf('<script setup lang="ts">') + '<script setup lang="ts">'.length, source.indexOf('</script>'))
const ast = ts.createSourceFile('view.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
const names = ['loadCurrentPlanIntoManager', 'nextPlanSequence', 'openCreatePlanManager', 'closeCreatePlanManager', 'toggleEditPlanManager', 'createPlanForCurrentRoom', 'saveCurrentPlan', 'submitPlanManager']
const functions = names.map(name => {
  const node = ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name)
  assert.ok(node, name)
  return node.getText(ast)
}).join('\n')
const manager = source.slice(source.indexOf('        <PlanCreateDialog'), source.indexOf('        <div class="strategy-workflow-layout'))
assert.ok(manager.includes('@close="closeCreatePlanManager" @create="createPlanForCurrentRoom"'))
const createEntry = source.match(/<button[^>]+@click="openCreatePlanManager\(\); closePlanActionsMenu\(\)"[^>]*>新建方案<\/button>/)?.[0]
const editEntry = source.match(/<button[^>]+@click="toggleEditPlanManager\(\); closePlanActionsMenu\(\)"[^>]*>修改名称与说明<\/button>/)?.[0]
assert.ok(createEntry && editEntry)
const fixture = `<script setup lang="ts">
import {computed,ref,shallowRef} from 'vue'
import PlanCreateDialog from '/src/components/PlanCreateDialog.vue'
import {createLiveAgentPlan,bindRoomLiveAgentPlan,updateLiveAgentPlan} from '/src/api.ts'
import {applySession} from '/src/session.ts'
import {useFeedbackErrorRef,feedbackState} from '/src/uiFeedback.ts'
import '/src/style.css'
const room=ref({id:26,tenant_id:14,name:'绵都杨鸭子'}),activeRoom=computed(()=>room.value)
const livePlans=ref([{id:6,tenant_id:14,name:'原有方案',description:'已有说明'}]),currentRoomPlanId=ref(6)
const currentPlan=computed(()=>livePlans.value.find(p=>p.id===currentRoomPlanId.value))
const planBusy=ref(false),newPlanName=ref(''),newPlanDescription=ref(''),showPlanManager=ref(false),planManagerMode=ref<'create'|'edit'>('edit'),planCreateError=ref(''),deleteConfirmPlanId=ref(null),settingsError=useFeedbackErrorRef(),messages=ref([]),planActionsMenu=ref(null)
const planCreateReturnFocus=shallowRef(null)
function closePlanActionsMenu(){planActionsMenu.value?.removeAttribute('open')}
async function refreshLivePlans(){if(window.fixture.refreshFailure)throw Error('列表刷新失败');livePlans.value=structuredClone(window.fixture.plans)}
async function refreshCurrentRoomPlan(){}
async function refreshPlanScripts(){}
${functions}
window.fixture={calls:[],plans:JSON.parse(JSON.stringify(livePlans.value)),fail:false,failBinding:false,hold:false,refreshFailure:false,
 state:()=>({show:showPlanManager.value,mode:planManagerMode.value,name:newPlanName.value,description:newPlanDescription.value,busy:planBusy.value,error:planCreateError.value,settingsError:settingsError.value,selected:currentRoomPlanId.value,alertOpen:feedbackState.alertOpen}),
 noRoom:()=>{room.value=undefined},duplicateSubmit:createPlanForCurrentRoom}
window.fetch=async(url,init={})=>{
 const path=new URL(url,location.origin).pathname,body=init.body?JSON.parse(init.body):null
 window.fixture.calls.push({path,method:init.method||'GET',body})
 if(window.fixture.hold){await new Promise(resolve=>window.fixture.release=resolve);window.fixture.hold=false}
 if(path==='/api/v1/live-agent-plans'){
  if(window.fixture.fail)return Response.json({error:'方案名称已存在，请修改后重试'},{status:409})
  const plan={id:window.fixture.plans.length+10,...body};window.fixture.plans.push(plan);return Response.json(plan)
 }
 if(path.endsWith('/room-bindings')){
  if(window.fixture.failBinding)return Response.json({error:'当前直播间不可绑定'},{status:409})
  return Response.json({})
 }
 if(init.method==='PUT'){
  const id=Number(path.split('/').at(-1)),plan={...window.fixture.plans.find(p=>p.id===id),...body};window.fixture.plans=window.fixture.plans.map(p=>p.id===id?plan:p);return Response.json(plan)
 }
 throw Error('Unexpected request: '+path)
}
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'本地测试'}],environment:'test'})
</script>
<template><main style="max-width:1200px;margin:24px auto;padding:20px"><header><h1>智能体方案</h1><details ref="planActionsMenu"><summary>更多</summary><div>${createEntry}${editEntry}</div></details></header>${manager}</main></template>`
const server = await createServer({
  server: { host: '127.0.0.1', port: 0 },
  plugins: [{ name: 'plan-create-dialog-fixture',
    resolveId(id) { if (id === '/__plan-fixture.vue') return fixtureId },
    load(id) { if (id === fixtureId) return fixture },
    configureServer(dev) { dev.middlewares.use(async (req, res, next) => {
      if (req.url !== '/__plan-test') return next()
      const html = await dev.transformIndexHtml(req.url, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from 'vue';import App from '/__plan-fixture.vue';createApp(App).mount('#app')</script></html>`)
      res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(html)
    }) },
  }],
})
await server.listen()
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], layouts = [], errors = []
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  page.setDefaultTimeout(10000)
  page.on('pageerror', error => errors.push(error.message))
  const url = server.resolvedUrls.local[0] + '__plan-test'
  const modal = page.getByRole('dialog', { name: '新建方案', exact: true })
  const name = page.getByRole('textbox', { name: '方案名称', exact: true })
  const description = page.getByRole('textbox', { name: '方案说明（选填）', exact: true })
  const create = page.getByRole('button', { name: '创建并绑定当前直播间', exact: true })
  const state = () => page.evaluate(() => window.fixture.state())
  const open = async () => { await page.getByText('更多', { exact: true }).click(); await page.getByRole('button', { name: '新建方案', exact: true }).click(); await modal.waitFor() }
  await page.goto(url)
  await open()
  assert.equal(await name.inputValue(), '绵都杨鸭子直播间 智能体方案01')
  assert.ok(await name.evaluate(element => document.activeElement === element))
  assert.equal(await page.locator('.strategy-plan-manager-inline').count(), 0)
  await page.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal((await state()).show, false)
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 0)
  assert.equal(await page.evaluate(() => document.activeElement?.tagName), 'SUMMARY')
  assert.equal(await page.evaluate(() => document.body.style.overflow), '')
  checks.push('真实新建入口打开弹窗、自动命名并聚焦；取消无请求且恢复焦点，无内联新建条')
  await open(); await page.keyboard.press('Escape'); await modal.waitFor({ state: 'detached' })
  await open(); await page.mouse.click(2, 2); await modal.waitFor({ state: 'detached' })
  checks.push('Escape 与遮罩点击均可取消，无创建请求')
  await open(); await name.fill('   '); assert.equal(await create.isDisabled(), true)
  await page.evaluate(() => window.fixture.duplicateSubmit()); assert.equal(await page.evaluate(() => window.fixture.calls.length), 0)
  await name.fill('测试新方案'); await description.fill('测试说明')
  await page.evaluate(() => window.fixture.fail = true)
  await create.click(); await modal.getByRole('alert').waitFor()
  assert.match(await modal.getByRole('alert').innerText(), /名称已存在/)
  assert.equal(await name.inputValue(), '测试新方案'); assert.equal(await description.inputValue(), '测试说明')
  assert.equal((await state()).mode, 'create'); assert.equal((await state()).alertOpen, false)
  checks.push('空白名称不提交；创建失败在弹窗内回显并保留名称/说明，不切换编辑或叠加全局错误')
  for (const width of [1920, 1440, 1024, 700, 390, 320]) {
    await page.setViewportSize({ width, height: 800 })
    const layout = await modal.evaluate(element => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth, left: element.getBoundingClientRect().left, right: element.getBoundingClientRect().right, fonts: [...element.querySelectorAll('input,textarea,label,footer button')].map(node => parseFloat(getComputedStyle(node).fontSize)) }))
    assert.ok(layout.scrollWidth <= width && layout.left >= 0 && layout.right <= width && layout.fonts.every(size => size >= 16), JSON.stringify(layout))
    layouts.push(layout)
  }
  await mkdir(output, { recursive: true })
  await page.setViewportSize({ width: 390, height: 800 }); await page.screenshot({ path: output + '/mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1440, height: 900 }); await page.screenshot({ path: output + '/desktop.png', fullPage: true })
  // Tab and Shift+Tab cannot escape the dialog while it is open.
  await create.focus(); await page.keyboard.press('Tab'); assert.equal(await page.evaluate(() => document.activeElement?.getAttribute('aria-label')), '关闭新建方案')
  await page.keyboard.press('Shift+Tab'); assert.ok(await create.evaluate(element => element === document.activeElement))
  checks.push('六种屏宽不溢出、字段/按钮至少16px；键盘焦点限制在弹窗内')
  await page.evaluate(() => { window.fixture.fail = false; window.fixture.hold = true })
  await create.click(); await page.waitForFunction(() => !!window.fixture.release)
  assert.equal(await name.isDisabled(), true); assert.equal(await page.getByRole('button', { name: '取消', exact: true }).isDisabled(), true)
  await page.keyboard.press('Escape'); assert.equal(await modal.count(), 1)
  await page.mouse.click(2, 2); assert.equal(await modal.count(), 1)
  const count = await page.evaluate(() => window.fixture.calls.length)
  await page.evaluate(() => window.fixture.duplicateSubmit()); assert.equal(await page.evaluate(() => window.fixture.calls.length), count)
  await page.evaluate(() => window.fixture.release()); await modal.waitFor({ state: 'detached' }); await page.waitForFunction(() => !window.fixture.state().busy)
  assert.equal((await state()).selected, 11)
  const calls = await page.evaluate(() => window.fixture.calls)
  assert.deepEqual(calls[1].body, { name: '测试新方案', description: '测试说明', tenant_id: 14 })
  assert.equal(calls.filter(call => call.path.endsWith('/room-bindings')).length, 1)
  assert.deepEqual(calls.find(call => call.path.endsWith('/room-bindings')).body, { room_id: 26, tenant_id: 14 })
  assert.equal((await state()).show, false); assert.equal(await page.locator('.strategy-plan-manager-inline').count(), 0)
  checks.push('创建中阻止取消及重复提交；成功仅绑定一次且关闭弹窗，未遗留编辑条')
  await page.reload(); await open(); await page.evaluate(() => window.fixture.failBinding = true); await create.click()
  await modal.waitFor({ state: 'detached' }); await page.waitForFunction(() => !window.fixture.state().busy)
  assert.match((await state()).settingsError, /已创建，但绑定当前直播间失败/)
  assert.equal(await page.evaluate(() => window.fixture.calls.filter(call => call.path === '/api/v1/live-agent-plans').length), 1)
  checks.push('已创建但绑定失败时关闭创建表单并保留原警告，不误导再次创建')
  await page.reload(); await page.getByText('更多', { exact: true }).click(); await page.getByRole('button', { name: '修改名称与说明', exact: true }).click()
  assert.equal(await modal.count(), 0); assert.equal(await page.locator('.strategy-plan-manager-inline').count(), 1)
  const editName = page.locator('.strategy-plan-manager-inline input').first(); await editName.fill('修改原方案')
  await page.getByRole('button', { name: '保存', exact: true }).click(); await page.waitForFunction(() => !window.fixture.state().busy)
  assert.equal(await page.evaluate(() => window.fixture.calls[0].method), 'PUT'); assert.equal((await state()).selected, 6)
  checks.push('原修改名称/说明入口仍编辑原方案并走PUT，不触发创建')
  await page.reload(); await page.evaluate(() => window.fixture.noRoom()); await open()
  await page.getByRole('button', { name: '创建方案', exact: true }).click(); await modal.waitFor({ state: 'detached' }); await page.waitForFunction(() => !window.fixture.state().busy)
  assert.match((await state()).settingsError, /没有可用的直播间/)
  assert.equal(await page.evaluate(() => window.fixture.calls.length), 1)
  checks.push('无直播间时保留只创建方案及明确未绑定提示，未发绑定请求')
  await page.reload(); await open(); await page.evaluate(() => window.fixture.refreshFailure = true); await create.click(); await modal.waitFor({ state: 'detached' }); await page.waitForFunction(() => !window.fixture.state().busy)
  assert.match((await state()).settingsError, /列表刷新失败/)
  assert.equal((await state()).show, false)
  checks.push('创建成功后列表读取失败不重新打开创建表单')
  assert.deepEqual(errors, [])
  await writeFile(output + '/checks.json', JSON.stringify({ checks, layouts, errors, liveRequests: false }, null, 2))
  console.log('PASS: ' + checks.join('；'))
} finally {
  await browser.close(); await server.close()
}
