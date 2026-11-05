// Actual parent and structured editor; all business requests use local fixtures.
import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,readFile,writeFile} from 'node:fs/promises'
import {createServer} from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const output=process.env.BODY_LIST_TEST_OUTPUT||'E:/直播伴播-local/prompt-body-list-20261105/browser'
await mkdir(output,{recursive:true})
const source=(await readFile(new URL('./test-plan-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
const parent=(await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
let fixture=source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup=parent.split('const setup = String.raw`')[1].split('\n`\nfixture =')[0]
const keys=['persona_md','facts_md','red_lines_md','speech_requirements_md','interaction_requirements_md','rewrite_planning_md']
const labels=['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划']
const sample='【直播目标】\r\n1. 清晰讲解产品卖点、使用方法、适合谁买；\r\n2. 承接公屏互动，引导停留；\r\n3. 自然引导下单，不强制逼单；\r\n4. 维持直播间氛围稳定；\r\n5. 每一轮讲解循环：痛点引入→展示产品→卖点拆解。\r\n\r\n【话术规则】\r\n1. 不说绝对化词：最、第一、100%。\r\n语速：正常\r\n'
fixture=fixture.replace('applySession(',setup+'\nif(!sessionStorage.getItem("rewrite-documents")){for(const k of '+JSON.stringify(keys)+'){if(k==="rewrite_planning_md")window.fixture.documents[4].rewrite_planning_md='+JSON.stringify(sample)+';else window.fixture.documents[4].sections[k]='+JSON.stringify(sample)+'}}\napplySession(')
const server=await createServer({cacheDir:output+'/vite-cache',server:{host:'127.0.0.1',port:0},appType:'custom',plugins:[{name:'body-list-parent',enforce:'pre',transform(code,id){
 if(!id.endsWith('/src/views/LiveStrategyView.vue'))return
 return code.replace('</script>',`window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,hydrating:()=>fullShowWorkspaceHydrating.value,sections:()=>planPromptSections.value,planning:()=>planRewritePlanning.value};</script>`)
}}]})
server.middlewares.use(async(req,res,next)=>{if(req.url!=='/__body-list')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml(req.url,fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[],errors=[],external=[];let page
try{
 const context=await browser.newContext({viewport:{width:1600,height:1100},reducedMotion:'reduce'})
 await context.route('**/*',route=>{if(new URL(route.request().url()).hostname==='127.0.0.1')return route.continue();external.push(route.request().url());return route.abort()})
 page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message))
 const ready=async()=>{await page.waitForFunction(()=>window.fixture?.view?.loaded()===window.fixture.view.plan()&&!window.fixture.view.loading()&&!window.fixture.view.busy()&&!window.fixture.view.hydrating())}
 const raw=()=>page.evaluate(()=>({...JSON.parse(JSON.stringify(window.fixture.view.sections())),rewrite_planning_md:window.fixture.view.planning()}))
 const body=()=>page.getByRole('textbox',{name:'直播目标 / 正文的值',exact:true})
 await page.goto(server.resolvedUrls.local[0]+'__body-list');await page.waitForFunction(()=>window.fixture?.view?.plan()===4)
 await page.getByRole('button',{name:'方案提示词',exact:true}).click();await ready()
 for(let i=0;i<6;i++){
  await page.getByRole('tab',{name:labels[i],exact:true}).click()
  assert.equal((await raw())[keys[i]],sample)
  assert.deepEqual(await page.locator('.prompt-key>span').allTextContents(),['语速'])
  assert.equal(await page.locator('.body-field').count(),2)
  assert.equal(await page.locator('.body-field .prompt-key-column').count(),0)
  assert.ok((await body().inputValue()).includes('5. 每一轮讲解循环：痛点引入'))
  await body().fill((await body().inputValue())+'\n6. 补充说明：保留编号和冒号😀')
  await page.getByRole('textbox',{name:'话术规则 / 语速的值',exact:true}).fill('略快')
  const before=(await raw())[keys[i]]
  await page.getByRole('button',{name:'编辑模型',exact:true}).click()
  assert.equal(await page.locator('.prompt-raw').inputValue(),before.replace(/\r\n|\r/g,'\n'))
  await page.getByRole('button',{name:'结构化显示',exact:true}).click()
  assert.deepEqual(await page.locator('.prompt-key>span').allTextContents(),['语速'])
  assert.ok((await body().inputValue()).includes('6. 补充说明：保留编号和冒号😀'))
  assert.equal((await raw())[keys[i]],before)
  checks.push(labels[i]+'列表带冒号仍为无键正文、整行显示；标准KV可编辑，双显示往返保持原文')
 }
 const edited=await raw()
 assert.equal(await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='PUT'&&c.path.endsWith('/prompt')).length),0)
 await page.locator('#plan-save-button').click();await ready();await page.getByText('方案提示词已随方案保存，三份元提示词已更新',{exact:true}).waitFor()
 await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await page.getByRole('button',{name:'方案提示词',exact:true}).click();await ready()
 for(const key of keys)assert.equal((await raw())[key],edited[key])
 checks.push('六页编辑后顶部统一保存，刷新原文、编号和内容保持')
 for(const width of [1600,777,390,320]){
  await page.setViewportSize({width,height:1100})
  const metrics=await page.locator('.body-field').first().evaluate(e=>{const t=e.querySelector('textarea').getBoundingClientRect(),r=e.getBoundingClientRect();return {scroll:e.scrollWidth,client:e.clientWidth,ratio:t.width/r.width}})
  assert.ok(metrics.scroll<=metrics.client+1&&metrics.ratio>.85)
  await page.locator('.structured-prompt').screenshot({path:output+'/body-list-'+width+'.png'})
 }
 checks.push('四宽度无空白键列或横向溢出，正文框占整行')
 await page.getByRole('button',{name:'删除模式',exact:true}).click();await page.getByRole('button',{name:'删除直播目标 / 正文',exact:true}).click()
 assert.ok(!(await raw()).persona_md.includes('每一轮讲解循环'))
 assert.ok((await raw()).persona_md.includes('语速：略快'))
 await page.getByRole('button',{name:'完成删除',exact:true}).click()
 checks.push('无键正文可用角标删除整段，保留其它节点和标准键值')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(output+'/checks.json',JSON.stringify({passed:true,groups:checks.length,checks,browser_errors:errors,external_requests:external},null,2));console.log(JSON.stringify({passed:true,groups:checks.length}))
}catch(e){if(page){await page.screenshot({path:output+'/failure.png',fullPage:true}).catch(()=>{});await writeFile(output+'/failure.txt',e.stack||String(e))}throw e}finally{await browser.close();await server.close()}
