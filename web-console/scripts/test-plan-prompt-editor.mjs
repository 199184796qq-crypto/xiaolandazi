// Full strategy page, actual prompt API wrapper and local endpoint fixtures.
import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,writeFile} from 'node:fs/promises'
import {createServer} from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const output=process.env.PLAN_PROMPT_TEST_OUTPUT||'E:/直播伴播-local/plan-prompt-editor-20261030/browser'
await mkdir(output,{recursive:true})
const fixture=String.raw`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="icon" href="data:,"><div id="app"></div><script type="module">
import {createApp,h} from 'vue';import {createRouter,createMemoryHistory} from 'vue-router';import View from '/src/views/LiveStrategyView.vue';import {applySession} from '/src/session.ts';import '/src/style.css';
const plans=[4,5].map(id=>({id,tenant_id:14,name:'测试方案'+id,status:'active',room_count:0,term_count:0}));
window.fixture={calls:[],data:JSON.parse(sessionStorage.getItem('prompt-data')||'null')||{4:'',5:'第二个方案的已保存提示词'},holdRead:false,holdRender:false,holdSave:false,failRender:false,failSave:false,failRead:false,emptyRender:false,largeRender:false,renderCount:0};
window.fetch=async(url,init={})=>{
 const f=window.fixture,u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',body=init.body?JSON.parse(init.body):null,plan=Number(p.match(/live-agent-plans\/(\d+)/)?.[1]||0);f.calls.push({path:p,method,body});
 if(p.endsWith('/prompt/render')&&method==='POST'){
  const text='方案'+plan+'的母版初稿第'+(++f.renderCount)+'版\n商品卡：本地测试商品\n活动福利：以已确认资料为准\n事实依据：仅引用本方案信息。';
  if(f.holdRender){f.holdRender=false;await new Promise(r=>f.releaseRender=r)}
  if(f.failRender){f.failRender=false;return Response.json({error:'母版读取失败，请稍后重试'},{status:500})}
  const rendered=f.emptyRender?'':f.largeRender?'字'.repeat(20001):text;f.emptyRender=false;f.largeRender=false;f.lastRender=rendered;
  return Response.json({prompt_md:rendered,master_sha256:'local-fixture-master'});
 }
 if(p.endsWith('/prompt')&&method==='PUT'){
  if(f.holdSave){f.holdSave=false;await new Promise(r=>f.releaseSave=r)}
  if(f.failSave){f.failSave=false;return Response.json({error:'保存暂时失败，请重试'},{status:500})}
  f.data[plan]=body.prompt_md;sessionStorage.setItem('prompt-data',JSON.stringify(f.data));return Response.json({prompt_md:body.prompt_md});
 }
 if(p.endsWith('/prompt')&&method==='GET'){
  if(f.holdRead){f.holdRead=false;await new Promise(r=>f.releaseRead=r)}
  if(f.failRead){f.failRead=false;return Response.json({error:'读取提示词失败，请重试'},{status:500})}
  return Response.json({prompt_md:f.data[plan]||''});
 }
 if(method==='POST'&&p.endsWith('/fact-gap-check'))return Response.json({items:[],issues:[],gaps:[],checks:[]});
 if(method!=='GET')throw Error('Unexpected mutation '+method+' '+p);
 if(p==='/api/v1/rooms')return Response.json({items:[]});
 if(p.endsWith('/ui-preferences'))return Response.json({});
 if(p==='/api/v1/live-agent-plans')return Response.json({items:plans});
 if(p.endsWith('/voice-rate'))return Response.json({tenant_id:14,plan_id:plan,rate:1});
 if(p.endsWith('/content-mode'))return Response.json({content_mode:'ai_dynamic',available_modes:['user_audio','ai_pregenerated','ai_dynamic'],dynamic_authorized:true});
 if(p.endsWith('/content-pools'))return Response.json({state:'synced',changes:[],products:[],benefits:[],pools:[],set_id:9,saved_set_id:9,job:{}});
 if(p.endsWith('/realtime-options'))return Response.json({buffer_seconds:60,body_min:2,body_max:4,max_chars:300});
 return Response.json({items:[],plugins:[],checks:[],groups:[],catalog:[],profiles:[],version:null,source:null,addressing:[],greeting:'你好'});
};
applySession({actor:{user_id:14,tenant_id:14,role:'customer',username:'test'},tenants:[{id:14,name:'测试'}],environment:'test'});
const router=createRouter({history:createMemoryHistory(),routes:[{path:'/',component:View}]});await router.push('/');await router.isReady();createApp({render:()=>h(View)}).use(router).mount('#app');</script></html>`
const server=await createServer({server:{host:'127.0.0.1',port:0},appType:'custom',plugins:[{name:'plan-prompt-inspect',enforce:'pre',transform(code,id){if(!id.endsWith('/src/views/LiveStrategyView.vue'))return;return code.replace('</script>',`window.fixture.view={plan:()=>currentRoomPlanId.value,setPlan:id=>handlePlanSwitch({target:{value:String(id)}})};</script>`)}}]})
server.middlewares.use(async(req,res,next)=>{if(req.url!=='/__prompt-editor')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml(req.url,fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const checks=[],errors=[],external=[]
try{
const context=await browser.newContext({viewport:{width:1440,height:1100},reducedMotion:'reduce'})
await context.route('**/*',route=>{const u=new URL(route.request().url());if(u.hostname==='127.0.0.1')return route.continue();external.push(u.hostname);return route.abort()})
const page=await context.newPage();page.setDefaultTimeout(25000);page.on('pageerror',e=>errors.push(e.message))
const base=server.resolvedUrls.local[0]+'__prompt-editor'
const open=async()=>{await page.getByRole('button',{name:'方案提示词',exact:true}).click();await page.locator('.plan-prompt-editor').waitFor()}
const editor=page.getByRole('textbox',{name:'提示词内容',exact:true})
const calls=()=>page.evaluate(()=>window.fixture.calls.filter(c=>/\/prompt(?:\/render)?$/.test(c.path)))
const renders=async()=>(await calls()).filter(c=>c.path.endsWith('/render'))
const saves=async()=>(await calls()).filter(c=>c.method==='PUT')
const modal=page.getByRole('dialog',{name:'重新生成方案提示词？',exact:true})
await page.goto(base);await page.waitForFunction(()=>window.fixture?.view?.plan()===4)
await page.evaluate(()=>window.fixture.holdRead=true);await open();await page.waitForFunction(()=>!!window.fixture.releaseRead)
await page.getByText('正在读取当前方案提示词…',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'建立提示词',exact:true}).isDisabled(),true)
await page.evaluate(()=>window.fixture.releaseRead());await editor.waitFor();assert.equal(await editor.inputValue(),'');assert.equal(await page.locator('.plan-prompt-actions button').count(),1);assert.equal((await renders()).length,0)
checks.push('空方案原样为空，仅建立按钮；读取骨架及禁用状态，不自动渲染或写入')

await page.evaluate(()=>window.fixture.holdRender=true);await page.getByRole('button',{name:'建立提示词',exact:true}).click();await page.waitForFunction(()=>!!window.fixture.releaseRender)
const generating=page.getByRole('button',{name:'生成中…',exact:true});assert.equal(await generating.isDisabled(),true);await generating.evaluate(el=>el.click());assert.equal((await renders()).length,1);assert.equal(await editor.inputValue(),'');assert.equal(await editor.getAttribute('readonly'),'');assert.equal((await saves()).length,0)
await page.evaluate(()=>window.fixture.releaseRender());await page.getByText('已按商品卡、活动福利和事实依据生成初稿，检查后保存',{exact:true}).waitFor()
assert.match(await editor.inputValue(),/母版初稿/);assert.deepEqual((await renders())[0].body,{});assert.equal((await saves()).length,0)
checks.push('建立只请求render({})；生成中防重复且保留文本，成功仅填初稿并提示检查保存')

const manual='用户手动修改后的方案提示词\n保留我自己补充的细节。'
await editor.fill(manual);await page.evaluate(()=>{window.fixture.failSave=true;window.fixture.holdSave=true});await page.getByRole('button',{name:'保存方案提示词',exact:true}).click();await page.waitForFunction(()=>!!window.fixture.releaseSave)
assert.equal(await page.getByRole('button',{name:'保存中…',exact:true}).isDisabled(),true);await page.evaluate(()=>window.fixture.releaseSave());await page.getByText('保存未完成',{exact:true}).waitFor();assert.equal(await editor.inputValue(),manual)
await page.getByRole('button',{name:'保存方案提示词',exact:true}).click();await page.getByText('方案提示词已保存',{exact:true}).waitFor();assert.equal(await page.evaluate(()=>window.fixture.data[4]),manual)
await page.reload();await page.waitForFunction(()=>window.fixture?.view?.plan()===4);await open();await editor.waitFor();await page.waitForFunction(()=>document.querySelector('#plan-prompt-content')?.value.includes('用户手动修改'))
assert.equal(await editor.inputValue(),manual);assert.equal((await renders()).length,0)
checks.push('保存失败保留手改文本，重试保存走原PUT；刷新后原GET恢复手改版本')

await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();await modal.getByText(/会覆盖当前内容/).waitFor();assert.equal((await renders()).length,0)
assert.equal(await modal.getByRole('button',{name:'取消',exact:true}).evaluate(el=>el===document.activeElement),true)
await modal.getByRole('button',{name:'取消',exact:true}).click();await modal.waitFor({state:'hidden'});assert.equal(await editor.inputValue(),manual)
await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();await page.keyboard.press('Escape');await modal.waitFor({state:'hidden'});assert.equal((await renders()).length,0)
checks.push('重新生成先确认覆盖；默认焦点在取消，取消和Escape均零render请求且保留内容')

for(const failure of ['failRender','emptyRender','largeRender']){
 await page.evaluate(key=>window.fixture[key]=true,failure);await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();await modal.getByRole('button',{name:'确认重新生成',exact:true}).click();await page.getByText('生成未完成',{exact:true}).waitFor();assert.equal(await editor.inputValue(),manual);assert.equal((await saves()).length,0)
}
checks.push('render的500、空结果及超长结果分开提示，已有内容保持完整，无自动保存')
await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();await modal.getByRole('button',{name:'确认重新生成',exact:true}).click();await page.getByText('已按商品卡、活动福利和事实依据生成初稿，检查后保存',{exact:true}).waitFor()
assert.notEqual(await editor.inputValue(),manual);assert.equal(await page.evaluate(()=>window.fixture.data[4]),manual);assert.equal((await saves()).length,0)
checks.push('确认后按最新render响应覆盖编辑稿，已保存内容要等用户再次保存才更新')

for(const width of [1440,390]){
 await page.setViewportSize({width,height:1100});await page.locator('.plan-prompt-editor').scrollIntoViewIfNeeded()
 const layout=await page.locator('.plan-prompt-editor').evaluate(root=>{const r=root.getBoundingClientRect();return {left:r.left,right:r.right,viewport:innerWidth,overflow:root.scrollWidth>root.clientWidth+1,buttons:[...root.querySelectorAll('.plan-prompt-actions button')].map(b=>{const x=b.getBoundingClientRect(),s=getComputedStyle(b);return {left:x.left,right:x.right,top:x.top,font:Number.parseFloat(s.fontSize),nowrap:s.whiteSpace}}),font:Number.parseFloat(getComputedStyle(root.querySelector('textarea')).fontSize)}})
 assert.ok(layout.left>=0&&layout.right<=width+1&&!layout.overflow,JSON.stringify(layout));assert.ok(layout.buttons.every(b=>b.left>=layout.left&&b.right<=layout.right+1&&b.font>=16&&b.nowrap==='nowrap'));assert.ok(Math.abs(layout.buttons[0].top-layout.buttons[1].top)<2);assert.ok(layout.font>=16)
 await page.locator('.plan-prompt-editor').screenshot({path:output+'/prompt-'+width+'.png'})
 await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();const bounds=await modal.boundingBox();assert.ok(bounds.x>=0&&bounds.x+bounds.width<=width);await modal.screenshot({path:output+'/confirm-'+width+'.png'});await modal.getByRole('button',{name:'取消',exact:true}).click()
 checks.push(width+'px真实父页：按钮同行且文字不换行、不溢出，字号至少16px，确认弹窗可完整操作')
}

await page.evaluate(()=>window.fixture.holdRender=true);await page.getByRole('button',{name:'重新生成',exact:true}).click();await modal.waitFor();await modal.getByRole('button',{name:'确认重新生成',exact:true}).click();await page.waitForFunction(()=>!!window.fixture.releaseRender)
await page.evaluate(()=>window.fixture.view.setPlan(5));await page.waitForFunction(()=>window.fixture.view.plan()===5&&document.querySelector('#plan-prompt-content')?.value==='第二个方案的已保存提示词')
await page.evaluate(()=>window.fixture.releaseRender());await page.waitForTimeout(100);assert.equal(await editor.inputValue(),'第二个方案的已保存提示词');assert.equal((await saves()).length,0)
checks.push('生成期间切换方案，旧响应丢弃，不覆盖其他方案也不写库')

await page.evaluate(()=>{window.fixture.failRead=true;window.fixture.view.setPlan(4)});await page.getByText('读取未完成',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:'保存方案提示词',exact:true}).isDisabled(),true)
await page.getByRole('button',{name:'重新读取',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#plan-prompt-content')?.value.includes('用户手动修改'));assert.equal(await editor.inputValue(),manual)
checks.push('读取错误与生成/保存错误区分；重读仍使用原load函数，不擅自生成空白内容')

const mutations=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method!=='GET'&&!c.path.endsWith('/fact-gap-check')&&!c.path.endsWith('/prompt/render')&&!c.path.endsWith('/prompt')))
assert.deepEqual(mutations,[]);assert.deepEqual(errors,[]);assert.deepEqual(external,[])
await writeFile(output+'/checks.json',JSON.stringify({passed:true,checks,errors,external,scope:'full strategy view and actual API wrappers; local fixtures only, no production save or paid calls'},null,2));console.log(JSON.stringify({passed:true,checks}))
}catch(e){await writeFile(output+'/failure.json',JSON.stringify({error:String(e),stack:e.stack,checks,errors,external},null,2));throw e}finally{await browser.close();await server.close()}
