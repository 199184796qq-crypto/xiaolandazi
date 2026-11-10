// Real strategy parent with local transport fixtures; no production writes or paid calls.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { createServer } from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const output=process.env.DUAL_MODE_TEST_OUTPUT||'E:/直播伴播-local/prompt-dual-mode-20261105/browser'
await mkdir(output,{recursive:true})
const source=(await readFile(new URL('./test-plan-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
const parent=(await readFile(new URL('./test-plan-rewrite-prompt-editor.mjs',import.meta.url),'utf8')).replace(/\r\n/g,'\n')
let fixture=source.split('const fixture=String.raw`')[1].split('`\nconst server=')[0]
const setup=parent.split('const setup = String.raw`')[1].split('\n`\nfixture =')[0]
const keys=['persona_md','facts_md','red_lines_md','speech_requirements_md','interaction_requirements_md','rewrite_planning_md']
const labels=['主播人设','事实依据','红线禁止','直播话术要求','互动回答要求','话术规划']
const samples=Object.fromEntries(keys.map((k,i)=>[k,'  前言😀 '+i+'\r\n\r\n【节点'+i+'】\r\n名称：值'+i+'\r\n说明：保留{{占位符}}\r\n\r\n']))
fixture=fixture.replace('applySession(',setup+'\nif(!sessionStorage.getItem("rewrite-documents")){const s='+JSON.stringify(samples)+';for(const [k,v] of Object.entries(s)){if(k==="rewrite_planning_md")window.fixture.documents[4].rewrite_planning_md=v;else window.fixture.documents[4].sections[k]=v}}\napplySession(')
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom',plugins:[{name:'dual-mode-parent',enforce:'pre',transform(code,id){
 if(!id.endsWith('/src/views/LiveStrategyView.vue'))return
 return code.replace('</script>',`window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}}),loaded:()=>planPromptLoadedPlan.value,loading:()=>planPromptLoading.value,busy:()=>savePlanOneClickBusy.value,hydrating:()=>fullShowWorkspaceHydrating.value,sections:()=>planPromptSections.value,planning:()=>planRewritePlanning.value};</script>`)
}}]})
server.middlewares.use(async(req,res,next)=>{if(req.url!=='/__dual-mode')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml(req.url,fixture))})
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
 const raw=i=>page.getByRole('textbox',{name:labels[i]+'完整内容',exact:true})
 const value=(i,key='名称')=>page.getByRole('textbox',{name:'节点'+i+' / '+key+'的值',exact:true})
 const mode=async text=>{const b=page.getByRole('button',{name:text?'编辑模型':'结构化显示',exact:true});if(await b.count())await b.click()}
 const draft=()=>page.evaluate(()=>({...JSON.parse(JSON.stringify(window.fixture.view.sections())),rewrite_planning_md:window.fixture.view.planning()}))
 const requests=()=>page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET').length)
 const select=async i=>{await tab(i).click();await ready()}
 const switchTo=async id=>{await page.evaluate(id=>window.fixture.view.setPlan(id),id);await ready()}
 const save=async()=>{await page.locator('#plan-save-button').click();await ready();await page.getByText('方案提示词已随方案保存，三份元提示词已更新',{exact:true}).waitFor()}
 await page.goto(server.resolvedUrls.local[0]+'__dual-mode');await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
 assert.equal(await page.locator('.plan-prompt-content-heading label,.structure-guide,.structure-legend,.structure-test,.node-title>small,.prompt-key small,.structure-original').count(),0)
 assert.deepEqual(await page.locator('.structure-toolbar button').allTextContents(),['编辑模型','＋ 添加节点','删除模式'])
 checks.push('移除截图冗余标题/说明/图例/锁定小字；编辑模型位于添加节点前')

 const original=await draft(),calls=await requests()
 for(let i=0;i<6;i++){
  await select(i);assert.equal(await page.getByRole('group',{name:labels[i]+'编辑器',exact:true}).count(),1)
  assert.equal(await value(i).inputValue(),'值'+i)
  await mode(true);assert.equal(await page.locator('#plan-prompt-panel textarea').count(),1);assert.equal(await page.locator('.prompt-node').count(),0)
  assert.equal(await raw(i).inputValue(),samples[keys[i]].replace(/\r\n/g,'\n'))
  await mode(false);assert.equal((await draft())[keys[i]],samples[keys[i]])
 }
 assert.deepEqual(await draft(),original);assert.equal(await requests(),calls)
 checks.push('六页均可双向切换，同一原稿逐字保留CRLF/空白/Unicode/占位符；纯切换零请求')

 const expected={...original}
 for(let i=0;i<6;i++){
  await select(i);await mode(true)
  const text='【修改节点'+i+'】\n改名键：整段修改😀 '+i+'\n\n'
  await raw(i).fill(text);expected[keys[i]]=text
  await mode(false)
  assert.equal(await page.getByRole('textbox',{name:'修改节点'+i+' / 改名键的值',exact:true}).inputValue(),'整段修改😀 '+i)
  await page.getByRole('textbox',{name:'修改节点'+i+' / 改名键的值',exact:true}).fill('结构修改 '+i)
  expected[keys[i]]=text.replace('整段修改😀 '+i,'结构修改 '+i)
  await mode(true);assert.equal(await raw(i).inputValue(),expected[keys[i]])
 }
 assert.deepEqual(await draft(),expected);assert.equal(await requests(),calls)
 checks.push('六页整段改节点名/键名后结构化正确重建，结构化改值同步单文本框，互不串页')

 await save();await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
 assert.deepEqual(await draft(),expected)
 for(let i=0;i<6;i++){await select(i);await mode(true);assert.equal(await raw(i).inputValue(),expected[keys[i]])}
 checks.push('顶部统一保存并刷新后，六份原文和话术规划保持修改')

 await select(0);await mode(false)
 await page.getByRole('button',{name:'给修改节点0添加键值',exact:true}).click()
 const add=page.getByRole('textbox',{name:'修改节点0 / 新增键值',exact:true})
 await add.fill('缺少冒号');await mode(true)
 assert.equal(await raw(0).count(),0);assert.equal(await add.inputValue(),'缺少冒号');await page.getByRole('alert').filter({hasText:'新增内容尚未添加'}).waitFor()
 await add.fill('内联键：失焦保留');await mode(true)
 assert.ok((await raw(0).inputValue()).includes('内联键：失焦保留'))
 await mode(false);assert.equal(await page.getByRole('textbox',{name:'修改节点0 / 内联键的值',exact:true}).inputValue(),'失焦保留')
 assert.ok((await draft()).persona_editor_json)
 await mode(true);await raw(0).fill('【新身份】\n名称：文字改写')
 assert.ok(!(await draft()).persona_editor_json)
 checks.push('未解析新增内容阻止切换并保留输入；有效新增先提交；整段修改清除过时来源元数据')

 const imeBefore=await draft()
 await raw(0).evaluate(el=>{el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));el.value='【输入法】\n名称：中文完成';el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true}))})
 assert.deepEqual(await draft(),imeBefore)
 await page.getByRole('button',{name:'结构化显示',exact:true}).click();assert.equal(await raw(0).count(),1)
 await raw(0).evaluate(el=>el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true})))
 assert.equal((await draft()).persona_md,'【输入法】\n名称：中文完成')
 checks.push('整段输入支持中文组合，未完成时不提交/切换，结束后只更新一次')

 const afterIme=await draft()
 await raw(0).evaluate(el=>{window.fixture.oldRaw=el;el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));el.value='迟到文本';el.dispatchEvent(new InputEvent('input',{bubbles:true,isComposing:true}))})
 await select(1)
 await page.evaluate(()=>window.fixture.oldRaw.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true})))
 assert.deepEqual(await draft(),afterIme)
 await select(0);assert.equal(await raw(0).inputValue(),afterIme.persona_md)
 checks.push('切页期间迟到组合事件被丢弃，原页与新页都不被半成品覆盖')

 await save();const beforePlan=await draft()
 await raw(0).evaluate(el=>{window.fixture.oldRaw=el;el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));el.value='旧方案迟到'})
 await switchTo(5);const plan5=await draft()
 await page.evaluate(()=>window.fixture.oldRaw.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true})))
 assert.deepEqual(await draft(),plan5)
 await switchTo(4);assert.equal((await draft()).persona_md,beforePlan.persona_md)
 await mode(true)
 checks.push('切方案销毁六页旧编辑器，旧组合事件不能写入新方案，原草稿恢复')

 const beforeLarge=await draft()
 await raw(0).evaluate(el=>{el.value='字'.repeat(20001);el.dispatchEvent(new InputEvent('input',{bubbles:true}))})
 assert.deepEqual(await draft(),beforeLarge);assert.equal(await raw(0).inputValue(),beforeLarge.persona_md)
 await page.getByRole('alert').filter({hasText:'不能超过20000字'}).waitFor()
 checks.push('整段编辑超过上限恢复已提交原稿，长度保护与结构化一致')

 await mode(false);await page.getByRole('button',{name:'＋ 添加节点',exact:true}).click()
 await page.getByRole('textbox',{name:'新增节点内容',exact:true}).fill('输入法')
 await page.getByRole('textbox',{name:'新增节点内容',exact:true}).blur()
 const current=(await draft()).persona_md
 await page.evaluate(text=>window.fixture.templates.persona_md=text,current)
 await page.evaluate(()=>window.fixture.holdRender=true)
 await page.getByRole('button',{name:'模板选择',exact:true}).click();await page.waitForFunction(()=>!!window.fixture.releaseRender)
 assert.equal(await page.getByRole('button',{name:'编辑模型',exact:true}).isDisabled(),true)
 await page.evaluate(()=>window.fixture.releaseRender());await page.getByRole('radio',{name:'测试模板',exact:true}).check();await page.getByRole('button',{name:'确认使用',exact:true}).click();await ready()
 assert.equal((await draft()).persona_md,current);assert.equal(await page.getByRole('textbox',{name:'新增节点内容',exact:true}).count(),0)
 checks.push('选择模板时禁止模式/内容修改；同文重新选用也清除旧新增草稿')

 for(let i=0;i<6;i++){
  await select(i);await mode(true);await raw(i).fill('无标题\n<自定义>{"原样":true}😀\n  末尾  ')
  await mode(false);await mode(true);assert.equal(await raw(i).inputValue(),'无标题\n<自定义>{"原样":true}😀\n  末尾  ')
  await raw(i).fill('');await mode(false);assert.ok((await page.locator('.prompt-value').evaluateAll(els=>els.map(el=>el.value))).every(v=>v===''));await mode(true);assert.equal(await raw(i).inputValue(),'')
 }
 await save();await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open()
 const empty=await draft();assert.ok(keys.every(k=>empty[k]===''))
 checks.push('普通/未知格式整段原样保存，六页可清空并保存刷新，不恢复旧内容')

 await mode(true);await raw(0).fill('【样例】\n名称：长内容😀'.repeat(15));await mode(false)
 for(const width of [1440,777,390,320]){
  await page.setViewportSize({width,height:1100})
  for(const text of [false,true]){
   await mode(text)
   const size=await page.locator('.structured-prompt').evaluate(el=>({scroll:el.scrollWidth,client:el.clientWidth}))
   assert.ok(size.scroll<=size.client+1,JSON.stringify(size))
   const fonts=await page.locator('.structured-prompt').evaluate(el=>[el,...el.querySelectorAll('*')].filter(e=>e.getClientRects().length&&e.textContent?.trim()).map(e=>parseFloat(getComputedStyle(e).fontSize)))
   assert.ok(fonts.every(n=>n>=16),JSON.stringify(fonts))
   await page.locator('.plan-prompt-editor').screenshot({path:output+'/'+width+'-'+(text?'text':'structured')+'.png'})
  }
 }
 checks.push('四种宽度两种模式无横向溢出，新增修改文字均不小于16px')
 assert.deepEqual(errors,[]);assert.deepEqual(external,[])
 await writeFile(output+'/checks.json',JSON.stringify({passed:true,groups:checks.length,checks,browser_errors:errors,external_requests:external},null,2))
 console.log(JSON.stringify({passed:true,groups:checks.length,checks}))
}catch(error){if(page){await page.screenshot({path:output+'/failure.png',fullPage:true}).catch(()=>{});await writeFile(output+'/failure.txt',error.stack||String(error))}throw error}finally{await browser.close();await server.close()}
