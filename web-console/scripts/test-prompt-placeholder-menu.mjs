// Offline regression of the real maintenance page and API wrapper. No model/provider calls.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const { chromium } = createRequire(new URL('../../collector-worker/package.json', import.meta.url))('playwright-core')
const output = process.env.PROMPT_PLACEHOLDER_TEST_OUTPUT || 'E:/直播伴播-local/prompt-placeholder-menu-20261105/browser'
const catalog = JSON.parse(await readFile(process.env.PROMPT_PLACEHOLDER_CATALOG || 'E:/直播伴播-local/prompt-placeholder-menu-20261105/catalog.json', 'utf8'))
await mkdir(output, { recursive: true })
const fixture = `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:0;padding:16px;background:#f5f6fb;font-family:system-ui;box-sizing:border-box}*{box-sizing:border-box}#app{max-width:1600px;margin:auto}button{font-family:inherit}</style></head><body><div id="app"></div><script type="module">
import {createApp,h,ref} from 'vue';
import View from '/src/views/PlanPromptTemplatesView.vue';
window.fixture={calls:[],catalog:${JSON.stringify(catalog)},revision:'a'.repeat(64),templates:{persona_md:'原始人设',facts_md:'原始事实',red_lines_md:'原始红线',speech_requirements_md:'原始主线要求',interaction_requirements_md:'原始互动要求',rewrite_planning_md:'原始规划'}};
window.fetch=async(url,init={})=>{
 const f=window.fixture,path=new URL(url,location.origin).pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null;
 if(path!=='/api/v1/system/plan-prompt-templates')throw Error('Unexpected business request: '+path);
 f.calls.push({path,method,body});
 if(f.hold){f.hold=false;await new Promise(resolve=>f.release=resolve)}
 if(f.fail){f.fail=false;return Response.json({error:'本地模拟保存失败'},{status:500})}
 if(method==='PUT'){
  if(body.revision!==f.revision)return Response.json({error:'母版已被其他人修改，请重新读取'},{status:409});
  f.templates=structuredClone(body.templates);f.revision='b'.repeat(64);
 }
 return Response.json({templates:structuredClone(f.templates),revision:f.revision,...(f.omitCatalog?{}:{placeholders:f.catalog})});
};
window.fixture.show=ref(true);createApp({render:()=>window.fixture.show.value?h(View):null}).mount('#app');
</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 18630, strictPort: true }, appType: 'custom' })
server.middlewares.use(async (req, res, next) => {
  if (req.url !== '/__placeholders') return next()
  res.setHeader('Content-Type', 'text/html;charset=utf-8'); res.end(await server.transformIndexHtml(req.url, fixture))
})
await server.listen()
const browser = await chromium.launch({ executablePath: 'C:/Program Files/Google/Chrome/Application/chrome.exe', headless: true })
const checks = [], errors = [], external = []
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 }, reducedMotion: 'reduce' })
  await context.route('**/*', route => { const url = new URL(route.request().url()); if (url.hostname === '127.0.0.1') return route.continue(); external.push(url.hostname); return route.abort() })
  const page = await context.newPage(); page.setDefaultTimeout(10000); page.on('pageerror', e => errors.push(e.message))
  const editor = page.locator('#prompt-template-content')
  const menu = page.getByRole('listbox', { name: '内置占位符', exact: true })
  const option = key => menu.getByRole('option').filter({ has: page.locator('code').filter({ hasText: '{{' + key + '}}' }) })
  const open = async (text = '@', caret = text.length) => {
    await editor.fill(text)
    await editor.evaluate((el, index) => { el.focus(); el.setSelectionRange(index, index); el.dispatchEvent(new MouseEvent('click', { bubbles: true })) }, caret)
    await menu.waitFor({ state: 'visible' })
  }
  const caret = () => editor.evaluate(el => el.selectionStart)
  const puts = () => page.evaluate(() => window.fixture.calls.filter(c => c.method === 'PUT'))
  const tab = label => page.getByRole('button', { name: label, exact: true })
  const ready = () => editor.locator('xpath=self::*[not(@readonly)]').waitFor()
  await page.goto(server.resolvedUrls.local[0] + '__placeholders'); await ready()

  const source = '前😀你是@直播间名后文'
  await open(source, source.indexOf('后文'))
  assert.equal(await menu.getByRole('option').count(), 1)
  await option('直播间名').click()
  assert.equal(await editor.inputValue(), '前😀你是{{直播间名}}后文')
  assert.equal(await caret(), '前😀你是{{直播间名}}'.length)
  assert.equal(await editor.evaluate(el => document.activeElement === el), true)
  assert.equal((await puts()).length, 0)
  checks.push('鼠标按光标替换 @查询，保留中文/emoji前后文和焦点，不自动保存')

  await open('@'); assert.equal(await menu.getByRole('option').count(), 18)
  assert.ok(!(await menu.textContent()).includes('{{自家称呼}}'))
  await editor.press('ArrowDown'); await editor.press('ArrowUp'); await editor.press('Tab')
  assert.equal(await editor.inputValue(), '{{直播间名}}')
  await open('@品类要点'); await editor.press('Enter'); assert.equal(await editor.inputValue(), '{{品类要点}}')
  await open('@'); await editor.press('Escape'); await menu.waitFor({ state: 'detached' })
  assert.equal(await editor.inputValue(), '@')
  await editor.press('ArrowLeft'); await editor.press('ArrowRight'); await menu.waitFor({ state: 'visible' })
  await editor.press('Escape')
  await open('@没有这个内置字段'); assert.equal(await menu.getByRole('option').count(), 0)
  await editor.press('Enter'); assert.equal(await editor.inputValue(), '@没有这个内置字段\n')
  checks.push('键盘上下/Enter/Tab、Esc关闭及移动光标重新打开、无匹配正常换行')

  await open('@主播名')
  await editor.evaluate(el => {
    el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
    el.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Enter', isComposing: true }))
  })
  await menu.waitFor({ state: 'detached' }); assert.equal(await editor.inputValue(), '@主播名')
  await editor.evaluate(el => el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true })))
  await menu.waitFor({ state: 'visible' }); await editor.press('Enter')
  assert.equal(await editor.inputValue(), '{{主播名}}')
  await open('文字@称呼'); await editor.evaluate(el => el.setSelectionRange(0, 2)); await menu.waitFor({ state: 'detached' })
  checks.push('中文输入法确认不会误插入；组合结束可选择；选中文字关闭菜单')

  await tab('事实依据').click(); await open('@'); assert.equal(await menu.getByRole('option').count(), 20)
  await open('开始@商品结尾', '开始@商品'.length)
  await menu.getByRole('option').filter({ hasText: '商品卡' }).click()
  assert.equal(await editor.inputValue(), '开始{{#商品}}\n{{/商品}}结尾')
  await tab('话术规划').click(); await open('@'); assert.equal(await menu.getByRole('option').count(), 4)
  await open('规划@样本话术保留', '规划@样本话术'.length); await editor.press('Enter')
  assert.equal(await editor.inputValue(), '规划{{样本话术}}保留')
  await tab('事实依据').click(); assert.equal(await editor.inputValue(), '开始{{#商品}}\n{{/商品}}结尾')
  await open('@'); await tab('主播人设').click(); await menu.waitFor({ state: 'detached' })
  assert.equal(await editor.inputValue(), '文字@称呼')
  checks.push('六页按替换阶段筛选，完整商品块插入，切页保留草稿并关闭旧菜单')

  const limit = '文'.repeat(19999) + '@'
  await open(limit); await option('直播间名').click()
  await page.getByRole('status').filter({ hasText: '插入后超过' }).waitFor()
  assert.equal(await editor.inputValue(), limit)
  await open('待保存@口号'); await editor.press('Enter')
  await page.evaluate(() => window.fixture.hold = true)
  await tab('保存母版').click(); await page.getByRole('button', { name: '保存中…' }).waitFor()
  assert.equal(await editor.getAttribute('readonly'), '')
  await menu.waitFor({ state: 'detached' }); await page.evaluate(() => window.fixture.release()); await ready()
  const saved = (await puts()).at(-1).body
  assert.deepEqual(Object.keys(saved).sort(), ['revision', 'templates'])
  assert.equal(Object.keys(saved.templates).length, 6)
  assert.equal(saved.templates.persona_md, '待保存{{口号}}')
  assert.equal(saved.revision, 'a'.repeat(64))
  await editor.fill('失败时保留{{直播间名}}'); await page.evaluate(() => window.fixture.fail = true); await tab('保存母版').click()
  await page.getByRole('alert').filter({ hasText: '当前草稿已保留' }).waitFor()
  assert.equal(await editor.inputValue(), '失败时保留{{直播间名}}')
  await tab('重新读取').click(); await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(await editor.inputValue(), '失败时保留{{直播间名}}')
  await tab('重新读取').click(); await page.getByRole('dialog').getByRole('button', { name: '确认读取' }).click(); await ready()
  assert.equal(await editor.inputValue(), '待保存{{口号}}')
  checks.push('20,000字符上限不破坏原文；保存时只读、原六份/CAS提交；失败和取消重读保留草稿')

  await page.evaluate(() => window.fixture.catalog = []); await tab('重新读取').click(); await ready()
  await editor.fill('显式空目录@'); await menu.waitFor({ state: 'detached' })
  await tab('重新读取').click(); await page.getByRole('dialog').getByRole('button', { name: '确认读取' }).click(); await ready()
  await page.evaluate(() => window.fixture.omitCatalog = true); await tab('重新读取').click(); await ready()
  await open('旧服务@直播间名'); await editor.press('Enter')
  assert.equal(await editor.inputValue(), '旧服务{{直播间名}}')
  await tab('保存母版').click(); await ready()
  assert.equal((await puts()).at(-1).body.templates.persona_md, '旧服务{{直播间名}}')
  await page.evaluate(catalog => {
    window.fixture.omitCatalog = false
    window.fixture.catalog = catalog
    window.fixture.catalog.push(window.fixture.catalog[0], { key: '伪造', label: '无效', insert_text: '{{伪造}}', description: '', group: '', templates: ['unsupported'] })
  }, catalog); await tab('重新读取').click(); await ready()
  await open('@'); assert.equal(await menu.getByRole('option').count(), 18)
  checks.push('旧服务缺目录使用已验证的兼容目录，可插入保存；重复或无效目录项过滤')

  for (const width of [1440, 777, 390, 320]) {
    await page.setViewportSize({ width, height: 900 })
    const text = ('多行文本与自动换行😀'.repeat(8) + '\n').repeat(25) + '光标处@直播间名后文'
    await open(text, text.indexOf('后文'))
    await editor.evaluate(el => { el.scrollTop = el.scrollHeight; el.dispatchEvent(new Event('scroll')) })
    await page.waitForTimeout(100)
    const box = await page.locator('.prompt-placeholder-popup').boundingBox()
    assert.ok(box && box.x >= 0 && box.x + box.width <= width + 1 && box.y >= 0 && box.y + box.height <= 901, JSON.stringify({ width, box }))
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
    const beforeScroll = await editor.evaluate(el => el.scrollTop)
    await option('直播间名').click()
    assert.ok((await editor.inputValue()).endsWith('光标处{{直播间名}}后文'))
    assert.equal(await editor.evaluate(el => el.scrollTop), beforeScroll)
    await open('@')
    await page.screenshot({ path: `${output}/menu-${width}.png`, fullPage: true })
    await editor.press('Escape')
  }
  checks.push('1440/777/390/320px、自动换行和内部滚动后定位在视口内，插入保留滚动且无横向溢出')

  await open('@'); await page.evaluate(() => window.fixture.show.value = false); await menu.waitFor({ state: 'detached' })
  await page.evaluate(() => { window.dispatchEvent(new Event('resize')); window.dispatchEvent(new Event('scroll')) })
  assert.deepEqual(errors, []); assert.deepEqual(external, [])
  checks.push('组件卸载关闭浮层、移除监听，无浏览器错误或外网业务请求')
  await writeFile(`${output}/result.json`, JSON.stringify({ passed: checks.length, checks, errors, external }, null, 2))
  console.log(JSON.stringify({ passed: checks.length, checks, errors, external }, null, 2))
} catch (error) {
  await writeFile(`${output}/failure.txt`, error.stack || String(error)); throw error
} finally {
  await browser.close(); await server.close()
}
