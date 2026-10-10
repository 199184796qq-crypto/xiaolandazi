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
const helpers = ts.transpileModule(ast.statements.filter(node => ts.isFunctionDeclaration(node) && ['factCategoryLabel','factCategoryDescription'].includes(node.name?.text)).map(node => node.getText(ast)).join('\n'), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
const fixture = `<html><head><meta charset="utf-8"><style>*{box-sizing:border-box}body{margin:0;padding:20px;background:#eff3ff;font-family:Arial,'Microsoft Yahei',sans-serif}${css}</style></head><body><div id="app"></div><script type="module">
import * as Vue from '/node_modules/.vite/deps/vue.js';import FactCategoryBadge from '/src/components/FactCategoryBadge.vue';
import {factCategoryState,factCategoryDefinition,factCategoryGroups,loadFactCategories} from '/src/factCategories.ts';factCategoryState.items=${JSON.stringify(catalog.categories)};
${helpers}
const rows=[['fulfillment','发货时效','一般当天发，下午6点后的订单次日上午发'],['fulfillment','售后政策','支持7天无理由退货'],['product','商品规格','商品信息以页面为准'],['trade','活动说明','活动期间下单享受优惠价'],['other','经营年限','经营70年'],['identity_location','门店地址','用户确认的门店地址']].map(([category,key,value],i)=>({id:i+1,category,key,value,forbidden_wording:i===2?'未经依据的夸大表达。'.repeat(30):i===5?'不要承诺未经核实的门店地址':'',safe_rewrite:i===2?'按已确认的商品信息介绍':i===3?'按实际活动条件介绍优惠':'',version_no:1,source_type:i===4?'manual_create':'adoption'}));
const state=Vue.reactive({currentPlan:{name:'样例方案'},formalFacts:rows,creatingFormalFact:false,formalFactCreateGuidance:'',formalFactCreateDraft:{},formalFactCategoryOptions:[{value:'fulfillment',label:'履约事实'},{value:'product',label:'商品事实'}],editingFormalFactId:null,formalFactEditDraft:{},mutatingFormalFactId:null,expandedFactRiskId:null,locatedFactRiskHash:'',factRiskNoticeState:{error:'',loading:false},factRiskAppealDrafts:{},session:{bootstrap:{actor:{role:'customer'}}},factRiskController:{},calls:[]});
window.fixture=state;
const review={claim_sha256:'r5',status:'confirmed',rule_id:'example',evidence:'用户确认内容真实',semantic_decision:'pass',reason:'已确认并备案',severity:'P1'};
const handlers={factCategoryState,factCategoryDefinition,factCategoryGroups,loadFactCategories,factCategoryLabel,factCategoryDescription,factRiskTone:item=>item.id===5?'confirmed':item.id===3?'blocked':'',factRiskToneLabel:item=>item.id===5?'用户已确认':'禁止使用',factRisksForFact:item=>item.id===5?[review]:[],factRiskCanConfirm:()=>false,startFormalFactEdit:item=>{state.calls.push('edit:'+item.id);state.editingFormalFactId=item.id;state.formalFactEditDraft={...item}},cancelFormalFactEdit:()=>{state.editingFormalFactId=null},saveFormalFactEdit:item=>{state.calls.push('save:'+item.id);state.editingFormalFactId=null},deleteFormalFact:item=>state.calls.push('delete:'+item.id),beginCreateFormalFact:()=>{},createFormalFact:()=>{},cancelCreateFormalFact:()=>{},confirmInlineFactRisk:()=>{},appealInlineFactRisk:()=>{}};
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
  assert.equal(await page.locator('.strategy-fact-risk-detail-row').count(),0,'wording must be collapsed initially')
  assert.equal(await rows.nth(2).locator('td[colspan="2"]').count(),1)
  await rows.nth(2).getByRole('button',{name:'有表达提醒',exact:true}).click()
  const detail=page.locator('#formal-fact-detail-3')
  await detail.waitFor()
  assert.equal(await detail.locator('td').getAttribute('colspan'),'6')
  assert.equal(await detail.locator('.strategy-formal-fact-wording-card').count(),3)
  assert.equal(await detail.locator('.is-forbidden .strategy-formal-fact-wording-content p').textContent(),await page.evaluate(()=>window.fixture.formalFacts[2].forbidden_wording),'full long wording retained')
  assert.equal(await detail.locator('.is-rewrite .strategy-formal-fact-wording-content p').textContent(),'按已确认的商品信息介绍')
  const arrow=rows.nth(2).locator('.strategy-formal-fact-expand');await arrow.focus();await arrow.press('Space');assert.equal(await detail.count(),0);await arrow.press('Enter');await detail.waitFor()
  await rows.nth(3).getByRole('button',{name:'有参考表达',exact:true}).click()
  assert.equal(await detail.count(),0,'only one row expands at a time')
  assert.match(await page.locator('#formal-fact-detail-4').textContent(),/暂无已保存的表达提醒/)
  assert.match(await page.locator('#formal-fact-detail-4').textContent(),/按实际活动条件介绍优惠/)
  await rows.nth(5).getByRole('button',{name:'有表达提醒',exact:true}).click()
  assert.match(await page.locator('#formal-fact-detail-6').textContent(),/暂无已保存的参考表达/)
  assert.deepEqual(await page.evaluate(()=>window.fixture.calls),[],'expansion must not edit, delete or generate content')
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
  const editRow=rows.nth(0), editActions=editRow.locator('.strategy-formal-fact-edit-actions button')
  assert.equal(await editRow.locator('.strategy-formal-fact-edit-field').count(),3)
  assert.equal(await editRow.locator('.strategy-formal-fact-edit-hint').count(),3)
  const saveBox=await editActions.nth(0).boundingBox(), cancelBox=await editActions.nth(1).boundingBox()
  assert.ok(cancelBox.y>saveBox.y+saveBox.height,'edit actions should stack vertically')
  assert.equal(saveBox.width,cancelBox.width,'edit actions should have fixed equal widths')
  const updatedText='一般是当天发，超过下午6点就第2天上午发'
  await editRow.getByRole('textbox',{name:'事实内容（可以说）',exact:true}).fill(updatedText)
  assert.equal(await editRow.locator('.is-content .strategy-formal-fact-edit-field-head>small').textContent(),updatedText.length+'/4000')
  await editRow.getByRole('textbox',{name:'不要这样说',exact:true}).fill('当天一定送达')
  await editRow.getByRole('textbox',{name:'改成这样说',exact:true}).fill('具体到货时间以物流信息为准')
  assert.equal(await editRow.locator('.is-forbidden .strategy-formal-fact-edit-field-head>small').textContent(),'6/4000')
  await editRow.getByRole('combobox',{name:'事实分类',exact:true}).selectOption('shipping_time')
  assert.match(await editRow.locator('.strategy-formal-fact-edit-name>small').textContent(),/发货/)
  const editOut=new URL('../../deploy/validation/fact-panel/',import.meta.url);await mkdir(editOut,{recursive:true})
  await page.locator('.strategy-formal-facts-table').screenshot({path:fileURLToPath(new URL('edit-desktop.png',editOut))})
  await page.setViewportSize({width:1320,height:930})
  assert.ok(await editRow.locator('textarea').evaluateAll(els=>els.every(el=>el.getBoundingClientRect().width>100)),'all three editors remain usable at table minimum width')
  assert.ok(await editRow.locator('.strategy-formal-fact-edit-field-head').evaluateAll(els=>els.every(el=>el.scrollWidth<=el.clientWidth)),'labels and counters must fit their cards')
  assert.ok(await rows.nth(2).locator('td').last().evaluate(cell=>[...cell.querySelectorAll('button')].every(button=>button.getBoundingClientRect().right<=cell.getBoundingClientRect().right)),'normal row actions should fit alongside their expansion arrow')
  await page.locator('.strategy-formal-facts-table').screenshot({path:fileURLToPath(new URL('edit-compact.png',editOut))})
  await page.setViewportSize({width:1774,height:930})
  await rows.nth(0).getByRole('button',{name:'取消',exact:true}).click()
  assert.equal(await rows.nth(0).locator('.strategy-formal-fact-value').textContent(),'一般当天发，下午6点后的订单次日上午发','cancel must retain original content')
  await rows.nth(1).getByRole('button',{name:'删除',exact:true}).click()
  assert.deepEqual(await page.evaluate(()=>window.fixture.calls),['edit:1','delete:2'])
  await rows.nth(4).getByRole('button',{name:'用户已确认',exact:true}).click()
  const out=new URL('../../deploy/validation/fact-panel/',import.meta.url);await mkdir(out,{recursive:true})
  await page.screenshot({path:fileURLToPath(new URL('desktop.png',out)),fullPage:true})
  await page.setViewportSize({width:390,height:850})
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,'mobile overflow must stay inside table')
  assert.ok(await page.locator('.strategy-formal-facts-table-wrap').evaluate(el=>el.scrollWidth>el.clientWidth))
  assert.deepEqual(errors,[])
  console.log('PASS: compact collapsed wording; full long text/three cards; accordion; edit cards/hints/live counts/category selector; fixed stacked actions; desktop/compact layout; cancel without changes; edit/delete; risk details/colors; mobile table scroll')
} finally {await browser.close();await server.close()}
