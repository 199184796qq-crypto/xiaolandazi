// Render the production fact section and styles with isolated sample data.
import assert from 'node:assert/strict'
import { readFile, mkdir } from 'node:fs/promises'
import { createServer } from 'vite'
import ts from 'typescript'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const webRequire = createRequire(new URL('../package.json', import.meta.url))
const vueRequire = createRequire(webRequire.resolve('vue/package.json'))
const { compile } = vueRequire('@vue/compiler-dom')
const source = await readFile(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const start = source.indexOf('<section class="strategy-formal-facts">')
const template = source.slice(start, source.indexOf('</section>', start) + '</section>'.length)
const renderCode = compile(template, { mode: 'function', prefixIdentifiers: true }).code
const css = [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map(match => match[1]).join('\n')
const catalog = JSON.parse(await readFile(new URL('../../configs/fact-categories.json', import.meta.url), 'utf8'))
const ast = ts.createSourceFile('view.ts', source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1], ts.ScriptTarget.Latest, true)
const helpers = ts.transpileModule(ast.statements.filter(node => ts.isFunctionDeclaration(node) && ['factCategoryLabel','factCategoryDescription','formalFactSourceLabel'].includes(node.name?.text)).map(node => node.getText(ast)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:20px;background:#eff3ff;font-family:Arial,'Microsoft Yahei',sans-serif}${css}</style></head><body><div id="app"></div><script type="module">
import * as Vue from '/node_modules/.vite/deps/vue.js';import FactCategoryBadge from '/src/components/FactCategoryBadge.vue';
import {factCategoryState,factCategoryDefinition,factCategoryGroups,loadFactCategories} from '/src/factCategories.ts';factCategoryState.items=${JSON.stringify(catalog.categories)};
${helpers}
const rows=[['fulfillment','发货时效','一般当天发，下午6点后的订单次日上午发'],['fulfillment','售后政策','支持7天无理由退货'],['product','商品规格','商品信息以页面为准'],['trade','活动说明','活动期间下单享受优惠价'],['other','经营年限','经营70年'],['identity_location','门店地址','用户确认的门店地址']].map(([category,key,value],i)=>({id:i+1,category,key,value,forbidden_wording:i===2?'未经依据的夸大表达':'',safe_rewrite:i===2?'按已确认的商品信息介绍':'',version_no:1,source_type:i===4?'manual_create':'adoption'}));
const state=Vue.reactive({currentPlan:{name:'样例方案'},formalFacts:rows,creatingFormalFact:false,formalFactCreateGuidance:'',formalFactCreateDraft:{},formalFactCategoryOptions:[{value:'fulfillment',label:'履约事实'},{value:'product',label:'商品事实'}],editingFormalFactId:null,formalFactEditDraft:{},mutatingFormalFactId:null,expandedFactRiskId:null,locatedFactRiskHash:'',factRiskNoticeState:{error:'',loading:false},factRiskAppealDrafts:{},session:{bootstrap:{actor:{role:'customer'}}},factRiskController:{},calls:[]});
window.fixture=state;
const review={claim_sha256:'r5',status:'confirmed',rule_id:'example',evidence:'用户确认内容真实',semantic_decision:'pass',reason:'已确认并备案',severity:'P1'};
const handlers={factCategoryState,factCategoryDefinition,factCategoryGroups,loadFactCategories,factCategoryLabel,factCategoryDescription,formalFactSourceLabel,factRiskTone:item=>item.id===5?'confirmed':item.id===3?'blocked':'',factRiskToneLabel:item=>item.id===5?'用户已确认':'禁止使用',factRisksForFact:item=>item.id===5?[review]:[],factRiskCanConfirm:()=>false,startFormalFactEdit:item=>{state.calls.push('edit:'+item.id);state.editingFormalFactId=item.id;state.formalFactEditDraft={...item}},cancelFormalFactEdit:()=>{state.editingFormalFactId=null},saveFormalFactEdit:item=>{state.calls.push('save:'+item.id);state.editingFormalFactId=null},deleteFormalFact:item=>state.calls.push('delete:'+item.id),beginCreateFormalFact:()=>{},createFormalFact:()=>{},cancelCreateFormalFact:()=>{},confirmInlineFactRisk:()=>{},appealInlineFactRisk:()=>{}};
Vue.createApp({components:{FactCategoryBadge},setup:()=>({...Vue.toRefs(state),...handlers}),render:new Function('Vue',${JSON.stringify(renderCode)})(Vue)}).mount('#app');</script></body></html>`
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use((req,res,next) => { if (req.url === '/__fact_panel') {res.setHeader('Content-Type','text/html; charset=utf-8');res.end(fixture)} else next() })
await server.listen()
const browser = await chromium.launch({ executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true })
const page = await browser.newPage({ viewport:{width:1774,height:930} }), errors=[]
page.on('pageerror',error=>errors.push(error.message))
try {
  await page.goto(server.resolvedUrls.local[0]+'__fact_panel')
  const rows = page.locator('.strategy-formal-facts-table tbody > tr:not(.strategy-fact-risk-detail-row)')
  await rows.nth(5).waitFor()
  assert.deepEqual(await page.locator('.strategy-formal-facts-table thead th').allTextContents(),['分类','事实名称','事实内容（可以说）','不要这样说','改成这样说','操作'])
  assert.equal(await page.locator('.strategy-formal-fact-version').count(),0)
  assert.equal(await rows.nth(0).locator('td').count(),6)
  assert.equal(await page.locator('.fact-category-badge svg').count(),6)
  const colors=await page.locator('.fact-category-badge').evaluateAll(els=>els.map(el=>getComputedStyle(el).color))
  assert.notEqual(colors[0],colors[2]);assert.notEqual(colors[2],colors[3]);assert.notEqual(colors[3],colors[4])
  for(let i=0;i<6;i++) {
    const actions=rows.nth(i).locator('.strategy-formal-fact-actions button'), a=await actions.nth(0).boundingBox(), b=await actions.nth(1).boundingBox()
    assert.ok(Math.abs(a.y-b.y)<2,'actions must be on one line')
    assert.equal(await actions.locator('svg').count(),2)
    assert.ok((await rows.nth(i).boundingBox()).height<104,'normal rows should be compact')
  }
  assert.equal(await page.locator('.strategy-formal-fact-wording').count(),12)
  assert.equal(await rows.nth(2).locator('.strategy-formal-fact-boundary').textContent(),'未经依据的夸大表达')
  const confirmedColor=await rows.nth(4).locator('td').first().evaluate(el=>getComputedStyle(el).backgroundColor)
  await rows.nth(4).hover()
  assert.equal(await rows.nth(4).locator('td').first().evaluate(el=>getComputedStyle(el).backgroundColor),confirmedColor,'hover must retain risk color')
  await rows.nth(4).getByRole('button',{name:'用户已确认',exact:true}).click()
  await page.locator('.strategy-fact-risk-detail-row').waitFor()
  assert.equal(await page.locator('.strategy-fact-risk-detail-row td').getAttribute('colspan'),'6')
  assert.match(await page.locator('.strategy-fact-risk-detail-row').textContent(),/用户确认内容真实/)
  await rows.nth(0).getByRole('button',{name:'修改',exact:true}).click()
  assert.equal(await rows.nth(0).locator('td').count(),6)
  assert.equal(await rows.nth(0).locator('select optgroup').count(),7)
  assert.equal(await rows.nth(0).locator('select option').count(),42)
  assert.equal(await rows.nth(0).locator('textarea').count(),3)
  await rows.nth(0).getByRole('button',{name:'取消',exact:true}).click()
  await rows.nth(1).getByRole('button',{name:'删除',exact:true}).click()
  assert.deepEqual(await page.evaluate(()=>window.fixture.calls),['edit:1','delete:2'])
  await rows.nth(4).getByRole('button',{name:'用户已确认',exact:true}).click()
  const out=new URL('../../deploy/validation/fact-panel/',import.meta.url);await mkdir(out,{recursive:true})
  await page.screenshot({path:fileURLToPath(new URL('desktop.png',out)),fullPage:true})
  await page.setViewportSize({width:390,height:850})
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'mobile overflow must stay inside table')
  assert.ok(await page.locator('.strategy-formal-facts-table-wrap').evaluate(el=>el.scrollWidth>el.clientWidth))
  assert.deepEqual(errors,[])
  console.log('PASS: six-column fact section without visible versions; category colors/icons; compact rows; horizontal actions; wording blocks; edit/delete bindings; preserved risk detail/colors; mobile table scroll')
} finally {await browser.close();await server.close()}
