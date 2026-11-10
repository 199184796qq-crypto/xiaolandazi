// Actual strategy parent and local API fixtures; no external business calls.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const output=process.env.DELETE_MODE_TEST_OUTPUT||'E:/直播伴播-local/prompt-delete-mode-20261105/browser'
await mkdir(output,{recursive:true})
const source=(await readFile(new URL('./test-plan-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
const parent=(await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
let fixture=source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup=parent.split('const setup = String.raw`')[1].split('\n`\nfixture =')[0]
const keys=['persona_md','facts_md','red_lines_md','speech_requirements_md','interaction_requirements_md','rewrite_planning_md']
const labels=['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划']
const samples=Object.fromEntries(keys.map((k,i)=>[k,'【节点'+i+'】\n模型正文😀\n键一：值一'+i+'\n键二：值二'+i+'\n\n【保留'+i+'】\n保留键：保留内容'+i+'\n']))
fixture=fixture.replace('applySession(',setup+'\nif(!sessionStorage.getItem("rewrite-documents")){const s='+JSON.stringify(samples)+';for(const [k,v] of Object.entries(s)){if(k==="rewrite_planning_md")window.fixture.documents[4].rewrite_planning_md=v;else window.fixture.documents[4].sections[k]=v}}\napplySession(')
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom',plugins:[{name:'delete-mode-parent',enforce:'pre',transform(code,id){
 if(!id.endsWith('/src/views/LiveStrategyView.vue'))return
 return code.replace('</script>',`window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,hydrating:()=>fullShowWorkspaceHydrating.value,sections:()=>planPromptSections.value,planning:()=>planRewritePlanning.value};</script>`)
}}]})
server.middlewares.use(async(req,res,next)=>{if(req.url!=='/__delete-mode')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml(req.url,fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[],errors=[],external=[];let page
try{
 const context=await browser.newContext({viewport:{width:1440,height:1100},reducedMotion:'reduce'})
 await context.route('**/*',route=>{const u=new URL(route.request().url());if(u.hostname==='127.0.0.1')return route.continue();external.push(u.hostname);return route.abort()})
 page=await context.newPage();page.setDefaultTimeout(15000);page.on('pageerror',e=>errors.push(e.message))
 const ready=async()=>{await page.waitForFunction(()=>window.fixture?.view?.loaded()===window.fixture?.view?.plan()&&!window.fixture.view.loading()&&!window.fixture.view.busy()&&!window.fixture.view.hydrating());await page.waitForFunction(()=>document.querySelector('.plan-prompt-editor')?.getAttribute('aria-busy')==='false')}
 const open=async()=>{await page.getByRole('button',{name:'方案提示词',exact:true}).click();await ready()}
 const tab=i=>page.getByRole('tab',{name:labels[i],exact:true})
 const draft=()=>page.evaluate(()=>({...JSON.parse(JSON.stringify(window.fixture.view.sections())),rewrite_planning_md:window.fixture.view.planning()}))
 const requests=()=>page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET').length)
 const enter=async()=>{const b=page.getByRole('button',{name:'删除模式',exact:true});if(await b.count())await b.click()}
 const exit=async()=>{const b=page.getByRole('button',{name:'完成删除',exact:true});if(await b.count())await b.click()}
 const badge=(name)=>page.getByRole('button',{name,exact:true})
 const select=async i=>{await tab(i).click();await ready()}
 const save=async()=>{await page.locator('#plan-save-button').click();await ready();await page.getByText('方案提示词已随方案保存，三份元提示词已更新',{exact:true}).waitFor()}
 await page.goto(server.resolvedUrls.local[0]+'__delete-mode');await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
 assert.deepEqual(await page.locator('.structure-toolbar button').allTextContents(),['编辑模型','＋ 添加节点','删除模式'])
 assert.equal(await page.locator('.delete-badge,.delete-node,.delete-field,.node-delete-confirm,.field-delete-confirm').count(),0)
 assert.equal(await page.getByRole('button',{name:/^删除(节点|键值|正文)$/,exact:true}).count(),0)
 checks.push('默认无删除角标，删除模式位于添加节点后，旧文字删除按钮与二次确认移除')

 const original=await draft(),calls=await requests();await enter()
 assert.equal(await page.getByRole('button',{name:'完成删除',exact:true}).getAttribute('aria-pressed'),'true')
 assert.equal(await page.locator('.node-delete-badge').count(),2);assert.equal(await page.locator('.field-delete-badge').count(),4)
 await page.emulateMedia({reducedMotion:'no-preference'})
 const style=await page.locator('.delete-badge').first().evaluate(el=>({color:getComputedStyle(el).backgroundColor,animation:getComputedStyle(el).animationName,shape:getComputedStyle(el).borderRadius,text:el.textContent}))
 assert.equal(style.color,'rgb(225, 70, 95)');assert.match(style.animation,/^prompt-delete-wiggle(?:-|$)/);assert.equal(style.shape,'50%');assert.equal(style.text,'×')
 await page.emulateMedia({reducedMotion:'reduce'})
 assert.deepEqual(await draft(),original);assert.equal(await requests(),calls)
 checks.push('开启后节点/键值/正文全部有红色圆形抖动角标，切换模式不改字不发请求')

 const expected={...original}
 for(let i=0;i<6;i++){
  await select(i);await enter()
  await badge('删除节点'+i+' / 键一键值').click();expected[keys[i]]=samples[keys[i]].replace('键一：值一'+i+'\n','')
  assert.equal((await draft())[keys[i]],expected[keys[i]])
  await badge('删除节点'+i+' / 正文').click();expected[keys[i]]=expected[keys[i]].replace('模型正文😀\n','')
  assert.equal((await draft())[keys[i]],expected[keys[i]])
  await badge('删除节点'+i+'节点').click();expected[keys[i]]='【保留'+i+'】\n保留键：保留内容'+i+'\n'
  assert.equal((await draft())[keys[i]],expected[keys[i]])
  assert.equal(await page.getByRole('textbox',{name:'保留'+i+' / 保留键的值',exact:true}).inputValue(),'保留内容'+i)
  assert.equal(await page.getByRole('dialog').count(),0)
 }
 assert.deepEqual(await draft(),expected);assert.equal(await requests(),calls)
 checks.push('六页角标点击即删除对应键名与内容、正文或整节点，其他内容逐字保留，无自动保存')

 await select(0);assert.equal(await page.locator('.delete-badge').count(),0);await enter();await exit()
 assert.equal(await page.locator('.delete-badge').count(),0);assert.deepEqual(await draft(),expected)
 await enter();await page.getByRole('button',{name:'编辑模型',exact:true}).click()
 assert.equal(await page.locator('.delete-badge,.delete-mode-toggle,.add-node').count(),0)
 assert.equal(await page.getByRole('textbox',{name:'主播人设完整内容',exact:true}).inputValue(),expected.persona_md)
 await page.getByRole('button',{name:'结构化显示',exact:true}).click();assert.equal(await page.locator('.delete-badge').count(),0)
 checks.push('完成删除、切页和切换整段显示均关闭模式；双显示内容同步不丢失')

 await page.getByRole('button',{name:'＋ 添加节点',exact:true}).click()
 await page.getByRole('textbox',{name:'新增节点内容',exact:true}).fill('自建节点\n自建键：可删除')
 await page.getByRole('textbox',{name:'新增节点内容',exact:true}).blur();await enter()
 await badge('删除自建节点 / 自建键键值').click();await badge('删除自建节点节点').click()
 assert.deepEqual(await draft(),expected)
 await save();assert.equal(await page.locator('.delete-badge').count(),0)
 await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open();assert.deepEqual(await draft(),expected)
 checks.push('自建节点和键值也使用相同角标，顶部统一保存刷新保留删除结果，模式默认关闭')

 await enter();await page.locator('.field-delete-badge').first().evaluate(el=>window.fixture.oldBadge=el)
 await select(1);const staleBefore=await draft()
 await page.evaluate(()=>window.fixture.oldBadge.dispatchEvent(new MouseEvent('click',{bubbles:true})))
 assert.deepEqual(await draft(),staleBefore)
 await select(0);await enter();await page.locator('.field-delete-badge').first().evaluate(el=>window.fixture.oldBadge=el)
 await page.getByRole('button',{name:'编辑模型',exact:true}).click()
 await page.evaluate(()=>window.fixture.oldBadge.dispatchEvent(new MouseEvent('click',{bubbles:true})))
 assert.deepEqual(await draft(),staleBefore)
 await page.getByRole('button',{name:'结构化显示',exact:true}).click()
 checks.push('切页或移除角标后旧DOM点击被拒绝，不能误删新页面或文本模式内容')

 await enter();await page.evaluate(()=>window.fixture.holdVersion=true);const beforeSave=await draft()
 await page.locator('#plan-save-button').click();await page.waitForFunction(()=>!!window.fixture.releaseVersion)
 assert.equal(await page.getByRole('button',{name:'删除模式',exact:true}).isDisabled(),true);assert.equal(await page.locator('.delete-badge').count(),0)
 await page.evaluate(()=>{window.fixture.releaseVersion();window.fixture.releaseVersion=null});await ready();assert.deepEqual(await draft(),beforeSave)
 await enter();const current=(await draft()).persona_md;await page.evaluate(text=>{window.fixture.templates.persona_md=text;window.fixture.holdRender=true},current)
 await page.getByRole('button',{name:'模板选择',exact:true}).click();await page.waitForFunction(()=>!!window.fixture.releaseRender)
 assert.equal(await page.locator('.delete-badge').count(),0);assert.equal(await page.getByRole('button',{name:'删除模式',exact:true}).isDisabled(),true)
 await page.evaluate(()=>{window.fixture.releaseRender();window.fixture.releaseRender=null});await page.getByRole('radio',{name:'测试模板',exact:true}).check();await page.getByRole('button',{name:'确认使用',exact:true}).click();await ready();assert.equal(await page.locator('.delete-badge').count(),0);assert.equal((await draft()).persona_md,current)
 checks.push('保存/选择模板期间关闭并禁用删除模式，同文选用结束后不恢复角标')

 await enter();await page.locator('.field-delete-badge').first().evaluate(el=>window.fixture.oldBadge=el)
 await page.evaluate(()=>window.fixture.view.setPlan(5));await ready();const plan5=await draft()
 await page.evaluate(()=>window.fixture.oldBadge.dispatchEvent(new MouseEvent('click',{bubbles:true})))
 assert.deepEqual(await draft(),plan5);assert.equal(await page.locator('.delete-badge').count(),0)
 await page.evaluate(()=>window.fixture.view.setPlan(4));await ready()
 checks.push('切方案退出删除模式，旧方案角标不能删除新方案的同名或同编号内容')

 await enter()
 for(const width of [1440,777,390,320]){
  await page.setViewportSize({width,height:1100});await page.waitForTimeout(60)
  const size=await page.locator('.structured-prompt').evaluate(el=>({scroll:el.scrollWidth,client:el.clientWidth}));assert.ok(size.scroll<=size.client+1,JSON.stringify(size))
  const bounds=await page.locator('.delete-badge').evaluateAll(els=>els.map(el=>{const b=el.getBoundingClientRect(),p=el.parentElement.getBoundingClientRect();const other=el.parentElement.querySelector('.prompt-value,.node-actions');const a=other?.getBoundingClientRect();return {contained:b.left>=p.left&&b.right<=p.right+1&&b.top>=p.top-1&&b.bottom<=p.bottom+1,clear:!a||b.left>=a.right-1,width:b.width}}))
  assert.ok(bounds.every(b=>b.contained&&b.clear&&b.width>=30),JSON.stringify(bounds))
  await page.locator('.plan-prompt-editor').screenshot({path:output+'/'+width+'-delete.png'})
 }
 checks.push('四种宽度角标均位于节点/键值右上角，不遮挡正文或添加按钮，无横向溢出')

 await page.emulateMedia({reducedMotion:'reduce'})
 assert.equal(await page.locator('.delete-badge').first().evaluate(el=>getComputedStyle(el).animationName),'none')
 await page.emulateMedia({reducedMotion:'no-preference'})
 const last=await draft();await page.locator('.field-delete-badge').first().focus();await page.keyboard.press('Enter')
 assert.notEqual((await draft()).persona_md,last.persona_md)
 checks.push('尊重系统减少动态设置；删除角标可通过键盘聚焦及回车操作')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(output+'/checks.json',JSON.stringify({passed:true,groups:checks.length,checks,browser_errors:errors,external_requests:external},null,2))
 console.log(JSON.stringify({passed:true,groups:checks.length,checks}))
}catch(error){if(page){await page.screenshot({path:output+'/failure.png',fullPage:true}).catch(()=>{});await writeFile(output+'/failure.txt',error.stack||String(error))}throw error}finally{await browser.close();await server.close()}
