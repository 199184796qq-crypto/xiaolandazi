import assert from 'node:assert/strict'
import { readFileSync, mkdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { createServer } from 'vite'
import ts from 'typescript'

const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const source = readFileSync(new URL('../src/liveCostSettings.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText
const helpers = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(compiled))
const initial = {
  version: 1, revision: 0, tts_price_micros_per_10k: 800000,
  default_model_rates: [{ max_input_tokens: 0, input_micros_per_million: 200000, cached_input_micros_per_million: 40000, output_micros_per_million: 800000 }],
  model_prices: [{ provider: '*', model: 'qwen3.7-flash', tiers: [
    { max_input_tokens: 32000, input_micros_per_million: 200000, cached_input_micros_per_million: 40000, output_micros_per_million: 800000 },
    { max_input_tokens: 256000, input_micros_per_million: 600000, cached_input_micros_per_million: 120000, output_micros_per_million: 2400000 },
    { max_input_tokens: 0, input_micros_per_million: 1200000, cached_input_micros_per_million: 240000, output_micros_per_million: 4800000 },
  ] }],
  mainline_hourly_micros: 1000000, optional_hourly_micros: 1200000, total_hourly_micros: 1300000,
}
assert.deepEqual(helpers.liveCostDraftToSettings(helpers.liveCostSettingsToDraft(initial)), initial)
assert.equal(helpers.liveCostYuanToMicros('0.000001', '金额'), 1)
assert.equal(helpers.liveCostYuanToMicros('0.700001', '金额'), 700001)
assert.equal(helpers.liveCostYuanToMicros('1000000', '金额'), 1000000000000)
assert.equal(helpers.liveCostMicrosToYuan(700001), '0.700001')
assert.equal(helpers.liveCostMicrosToYuan(0), '0')
for (const value of ['-1', '0', '', '0.1234567', '1e3', '1000000.000001', 'Infinity']) {
  assert.throws(() => helpers.liveCostYuanToMicros(value, '金额'))
}
assert.equal(helpers.liveCostYuanToMicros('0', '缓存价格', true), 0)
const invalid = mutate => { const d = helpers.liveCostSettingsToDraft(initial); mutate(d); assert.throws(() => helpers.liveCostDraftToSettings(d)) }
invalid(d => { d.mainline = '1.3' })
invalid(d => { d.total = '0' })
invalid(d => { d.fallback[0].maxInput = '32000' })
invalid(d => { d.models[0].tiers[1].maxInput = '32000' })
invalid(d => { d.models[0].tiers[0].maxInput = '0' })
invalid(d => { d.models.push(structuredClone(d.models[0])) })
invalid(d => { d.models[0].model = 'model*' })
invalid(d => { d.models[0].provider = 'provider*' })
invalid(d => { d.models[0].model = 'model name' })
invalid(d => { d.models[0].model = '字'.repeat(54) })
invalid(d => { d.models[0].tiers[0].maxInput = '1000000000001' })
invalid(d => { d.revision = 1000000000000 })
invalid(d => { d.models[0].tiers = Array.from({ length: 17 }, () => structuredClone(d.models[0].tiers[0])) })
invalid(d => { d.models = Array.from({ length: 33 }, (_, i) => ({ provider: '*', model: `test-${i}`, tiers: [structuredClone(d.fallback[0])] })) })
const oversized = helpers.liveCostSettingsToDraft(initial)
oversized.models = Array.from({ length: 32 }, (_, i) => ({ provider: '*', model: `fixture-${i}`, tiers: Array.from({ length: 16 }, (_, j) => ({ ...structuredClone(oversized.fallback[0]), maxInput: j === 15 ? '0' : String((j + 1) * 1000) })) }))
assert.throws(() => helpers.liveCostDraftToSettings(oversized), /内容过长/, 'combined rules must fit the UTF-8 byte limit even when each count is valid')
const zero = helpers.liveCostSettingsToDraft(initial)
zero.mainline = zero.optional = '0'; zero.fallback[0].cached = '0'
assert.equal(helpers.liveCostDraftToSettings(zero).optional_hourly_micros, 0)

const view = readFileSync(new URL('../src/views/SystemSettingsView.vue', import.meta.url), 'utf8')
assert.match(view, /<LiveCostSettingsPanel v-if="showLiveSettings && canManageAgentPrompts"\s*\/>/)
const catalogSource = readFileSync(new URL('../src/systemSettingsCatalog.ts', import.meta.url), 'utf8')
const catalogCompiled = ts.transpileModule(catalogSource, { compilerOptions: { module: ts.ModuleKind.ESNext } }).outputText
const catalog = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(catalogCompiled))
const entry = catalog.systemSettingsCategories.find(x => x.key === 'live').groups.flatMap(x => x.entries).find(x => x.key === 'live-cost-settings')
assert.equal(entry.to, '/system/settings/live#live-cost-settings')
assert.equal(catalog.canViewSystemSettingsEntry(entry, { actor: { role: 'platform_admin' } }), true)
assert.equal(catalog.canViewSystemSettingsEntry(entry, { actor: { role: 'staff' }, staff_access: { is_super_admin: true, permissions: ['system.settings.view'] } }), true)
assert.equal(catalog.canViewSystemSettingsEntry(entry, { actor: { role: 'staff' }, staff_access: { is_super_admin: false, permissions: ['system.settings.view'] } }), false)

let config = structuredClone(initial), failure = 0, puts = [], gets = 0
const server = await createServer({ server: { host: '127.0.0.1', port: 0, hmr: false }, appType: 'custom' })
server.middlewares.use((req, res, next) => {
  if (req.url !== '/__live_cost') return next()
  res.setHeader('Content-Type', 'text/html; charset=utf-8')
  res.end('<html><head><meta charset="utf-8"><style>body{margin:0;padding:16px;font-family:"Microsoft YaHei";background:#f5f7fc}*{box-sizing:border-box}</style></head><body><div id="app"></div><script type="module">import{createApp}from"/node_modules/.vite/deps/vue.js";import Panel from"/src/components/LiveCostSettingsPanel.vue";createApp(Panel).mount("#app")</script></body></html>')
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const page = await browser.newPage({ viewport: { width: 1500, height: 1100 } }), errors = [], unexpected = []
page.on('pageerror', e => errors.push(e.message))
await page.route('**/api/v1/**', async route => {
  const req = route.request(), path = new URL(req.url()).pathname
  if (path !== '/api/v1/system/live-cost-settings') {
    unexpected.push(path); await route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"测试不允许调用其他接口"}' }); return
  }
  if (req.method() === 'PUT') {
    const payload = req.postDataJSON(); puts.push(payload)
    if (failure) {
      const error = failure === 409 ? '费用配置已变化，请重新读取后再保存' : failure === 503 ? '保存直播费用配置失败，请重新读取后重试' : '声音单价无效，请检查输入金额'
      await route.fulfill({ status: failure, contentType: 'application/json', body: JSON.stringify({ error }) }); return
    }
    assert.equal(payload.revision, config.revision, 'save must use the fetched revision')
    config = { ...payload, revision: config.revision + 1 }
  } else { gets++ }
  await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ config }) })
})
const label = text => page.getByLabel(text, { exact: true })
const save = () => page.getByRole('button', { name: '保存成本设置', exact: true })
const reread = () => page.getByRole('button', { name: '重新读取成本设置', exact: true })
try {
  await page.goto(server.resolvedUrls.local[0] + '__live_cost')
  await label('每万计费字符声音单价（元）').waitFor()
  assert.equal(await label('每万计费字符声音单价（元）').inputValue(), '0.8')
  await page.getByText('约 1.6 元／万汉字', { exact: true }).waitFor()
  assert.equal(await label('模型规则1第2档完整输入上限').inputValue(), '256000')
  await label('每万计费字符声音单价（元）').fill('0.700001')
  await label('主线更新小时目标（元）').fill('1.1')
  await label('全部可选工作小时软线（元）').fill('1.25')
  await label('直播间小时总目标（元）').fill('1.5')
  await save().click(); await page.getByRole('status').waitFor()
  assert.equal(puts.length, 1); assert.equal(puts[0].tts_price_micros_per_10k, 700001)
  assert.equal(puts[0].mainline_hourly_micros, 1100000); assert.equal(puts[0].optional_hourly_micros, 1250000)
  assert.equal(puts[0].total_hourly_micros, 1500000); assert.deepEqual(puts[0].model_prices, initial.model_prices)
  assert.equal(puts[0].revision, 0); assert.equal(puts[0].version, 1)
  await page.reload(); await label('每万计费字符声音单价（元）').waitFor()
  assert.equal(await label('每万计费字符声音单价（元）').inputValue(), '0.700001')
  assert.equal(await label('主线更新小时目标（元）').inputValue(), '1.1')

  await label('每万计费字符声音单价（元）').fill('0.1234567'); await save().click()
  await page.getByRole('alert').waitFor(); assert.equal(puts.length, 1, 'invalid decimal must not reach API')
  await label('每万计费字符声音单价（元）').fill('0.8')
  failure = 400; await save().click(); await page.getByRole('alert').waitFor()
  assert.match(await page.getByRole('alert').innerText(), /声音单价无效/)
  assert.equal(await save().isEnabled(), true, 'invalid server input may be corrected without stale overwrite')
  failure = 409; await save().click()
  await page.waitForFunction(() => document.querySelector('[role=alert]')?.textContent.includes('当前修改尚未保存'))
  assert.equal(await save().isDisabled(), true, 'conflict must lock saving until a successful reload')
  assert.equal(config.tts_price_micros_per_10k, 700001)
  const attempts = puts.length
  failure = 0; await reread().click(); await page.waitForFunction(() => !document.querySelector('#live-cost-settings button[type=submit]').disabled)
  assert.equal(puts.length, attempts); assert.equal(await label('每万计费字符声音单价（元）').inputValue(), '0.700001')
  failure = 503; await save().click(); await page.getByRole('alert').waitFor()
  assert.match(await page.getByRole('alert').innerText(), /保存直播费用配置失败/)
  assert.doesNotMatch(await page.getByRole('alert').innerText(), /配置已变化/, 'storage failure must not be mislabeled as a version conflict')
  failure = 0; await reread().click(); await page.waitForFunction(() => !document.querySelector('#live-cost-settings button[type=submit]').disabled)

  await page.getByRole('button', { name: '添加模型规则', exact: true }).click()
  await label('模型规则2模型名称').fill('fixture-model'); await label('模型规则2第1档输入单价').fill('0.3')
  await label('模型规则2第1档缓存输入单价').fill('0'); await label('模型规则2第1档输出单价').fill('0.9')
  await page.getByRole('button', { name: '模型规则2添加档位', exact: true }).click()
  await label('模型规则2第1档完整输入上限').fill('16000'); await label('模型规则2第1档输入单价').fill('0.2')
  await label('模型规则2第1档缓存输入单价').fill('0'); await label('模型规则2第1档输出单价').fill('0.6')
  assert.equal(await label('模型规则2第2档完整输入上限').inputValue(), '0')
  await save().click(); await page.getByRole('status').waitFor()
  assert.equal(config.model_prices.length, 2); assert.equal(config.model_prices[1].provider, '*')
  assert.deepEqual(config.model_prices[1].tiers.map(x => x.max_input_tokens), [16000, 0])
  await page.getByRole('button', { name: '删除模型规则2第1档', exact: true }).click()
  await page.getByRole('button', { name: '删除模型规则2', exact: true }).click()
  await label('默认第1档输入单价').fill('0.22')
  await save().click(); await page.getByRole('status').waitFor()
  assert.equal(config.model_prices.length, 1); assert.equal(config.default_model_rates[0].input_micros_per_million, 220000)

  const screenshotDir = process.env.LIVE_COST_SCREENSHOT_DIR
  if (screenshotDir) { mkdirSync(screenshotDir, { recursive: true }); await page.screenshot({ path: screenshotDir + '/cost-desktop.png', fullPage: true }) }
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
  await page.setViewportSize({ width: 390, height: 844 })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, '390px layout must not overflow')
  if (screenshotDir) await page.screenshot({ path: screenshotDir + '/cost-mobile.png', fullPage: true })
  assert.ok(gets >= 3); assert.deepEqual(errors, []); assert.deepEqual(unexpected, [])
  console.log('PASS: exact micro-CNY conversion, limits, budget order, defaults, model/tiers, load/save/re-enter, Chinese 400/409 and conflict lock, platform/super-admin entry, 1500/390 layouts; fixture API only')
} finally { await browser.close(); await server.close() }
