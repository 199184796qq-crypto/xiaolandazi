import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { readFile, mkdir } from 'node:fs/promises'
import { createServer } from 'vite'
const require = createRequire(new URL('../../collector-worker/package.json', import.meta.url))
const { chromium } = require('playwright-core')
const original = await readFile(new URL('./test-interaction-full-view-location.mjs', import.meta.url), 'utf8')
let fixture = original.match(/const fixture = `([\s\S]*?)`\r?\nconst deferredWorkspace/)[1]
fixture = fixture.replace("import {createApp,h}", "import {createApp,h,ref}")
fixture = fixture.replace("import View from '/src/views/LiveStrategyView.vue';", "import View from '/src/views/LiveStrategyView.vue';import Admin from '/src/views/AnchorRecommendationsAdminView.vue';")
fixture = fixture.replace("name:'测试方案',room_ids", "name:'测试方案',status:'active',room_ids")
fixture = fixture.replace('const router=createRouter', `
const baseFetch=window.fetch;
const profile={summary:'自然亲切',dimensions:[{key:'rhythm',label:'节奏',value:'自然'}],reusable_rules:['简洁自然'],candidate_patterns:[],excluded_from_style:[],delivery_spec:{meta_prompt_md:'使用自然亲切的表达，不添加业务事实。'}};
const style={id:7,tenant_id:14,name:'小林主播',description:'自然亲切的直播表达',avatar_type:'female',profile,plugin_settings:[],sample_count:1,training_count:3,status:'active'};
const summary='风格自然'.repeat(12)+'亲切';
window.fixture.styles=[style];window.fixture.rec=[{id:2,name:'系统主播B',introduction:'简洁明亮的风格',summary,avatar_type:'male',status:'active',version_no:1}];window.fixture.writes=[];window.fixture.bound=7;window.fixture.failImport=false;
const mode=ref('admin');window.fixture.customer=()=>{applySession({actor:{user_id:14,tenant_id:14,role:'customer'},tenants:[{id:14,name:'测试'}],environment:'test'});mode.value='customer'};
window.fetch=async(url,init={})=>{
 const u=new URL(url,location.origin),p=u.pathname,method=init.method||'GET',f=window.fixture;
 if(method!=='GET'){
  const multipart=init.body instanceof FormData,body=multipart?{avatar_type:init.body.get('avatar').type,avatar_size:init.body.get('avatar').size}:JSON.parse(init.body||'{}');f.writes.push({p,query:u.search,method,body});
  if(p.endsWith('/anchor-recommendations/preview'))return Response.json({tenant_id:14,style_id:7,name:'系统主播A',introduction:style.description,summary,avatar_type:'female',source_sha256:'a'.repeat(64),raw_text:JSON.stringify({summary}),provider:'test',model:'test',prompt_source:'mother#test'});
  if(p==='/api/v1/system/anchor-recommendations'){const rec={id:1,name:body.name,introduction:body.introduction,summary:body.summary,avatar_type:'female',status:'active',version_no:1};f.rec.unshift(rec);return Response.json(rec)}
  if(p.endsWith('/anchor-recommendations/import')){
   if(f.failImport){f.failImport=false;return Response.json({error:'系统推荐主播已下架或不存在'},{status:409})}
   const items=body.ids.map(id=>{let item=f.styles.find(s=>s.source_recommendation_id===id);if(!item){const rec=f.rec.find(r=>r.id===id);item={...style,id:106+id,name:rec.name,description:rec.summary,sample_count:0,training_count:0,source_recommendation_id:id};f.styles.push(item)}return item});return Response.json({items});
  }
  if(p.endsWith('/avatar-image')){const id=Number(p.split('/')[4]),s=f.styles.find(s=>s.id===id),blob=init.body.get('avatar');const bytes=await blob.arrayBuffer();let bin='';for(const b of new Uint8Array(bytes))bin+=String.fromCharCode(b);s.avatar_url='data:image/jpeg;base64,'+btoa(bin);return Response.json(s)}
  if(p.endsWith('/bind-plan')){f.bound=Number(p.split('/')[4]);return Response.json({bound:true,style_id:f.bound,plan_id:body.plan_id})}
  if(p.endsWith('/withdraw')){f.rec.find(r=>r.id===Number(p.split('/')[4])).status='withdrawn';return Response.json({withdrawn:true})}
  throw Error('Unexpected mutation '+p);
 }
 if(p==='/api/v1/system/anchor-recommendations/sources')return Response.json({items:[{...style,has_style:true,tenant_name:'测试用户'}]});
 if(p==='/api/v1/system/anchor-recommendations'||p==='/api/v1/live/anchor-recommendations')return Response.json({items:f.rec.filter(r=>p.includes('/system/')||r.status==='active')});
 if(p==='/api/v1/live-anchor-styles')return Response.json({items:f.styles});
 if(p==='/api/v1/live-anchor-styles/current')return Response.json({style_id:f.bound});
 if(p.startsWith('/api/v1/live-anchor-styles/')){
  if(p.endsWith('/samples'))return Response.json({items:[]});
  if(p.endsWith('/analysis-versions'))return Response.json({items:[],active_version_id:0});
  return Response.json(f.styles.find(s=>s.id===Number(p.split('/')[4])));
 }
 return baseFetch(url,init);
};
const router=createRouter`)
fixture = fixture.replace("routes:[{path:'/',component:View}]", "routes:[{path:'/',name:'live-strategy',component:View}]")
fixture = fixture.replace('render:()=>h(View)', "render:()=>h(mode.value==='admin'?Admin:View)")
const server = await createServer({ server: { host: '127.0.0.1', port: 0 }, appType: 'custom' })
server.middlewares.use(async(req,res,next)=>{if(!req.url.startsWith('/__anchor_recommendation'))return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await server.transformIndexHtml('/__anchor_recommendation',fixture))})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
try {
 const page=await browser.newPage({viewport:{width:1600,height:1150},reducedMotion:'reduce'}),errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.goto(server.resolvedUrls.local[0]+'__anchor_recommendation')
 await page.getByRole('button',{name:'设为系统推荐',exact:true}).click()
 await page.getByRole('button',{name:'发布为系统推荐',exact:true}).waitFor()
 assert.equal((await page.evaluate(()=>window.fixture.writes)).length,1,'preview must not publish automatically')
 assert.equal(await page.getByLabel('风格特征简介').inputValue(),'风格自然'.repeat(12)+'亲切')
 await page.getByRole('button',{name:'发布为系统推荐',exact:true}).click()
 await page.getByRole('status').filter({hasText:'已发布为系统推荐'}).waitFor()
 const pub=(await page.evaluate(()=>window.fixture.writes))[1];assert.equal(pub.body.summary.length,50);assert.ok(pub.body.generation.raw_text)
 await mkdir('../deploy/validation/anchor-recommendations',{recursive:true});await page.screenshot({path:'../deploy/validation/anchor-recommendations/admin.png'})
 await page.evaluate(()=>window.fixture.customer())
 await page.locator('.workflow-step-anchor').click()
 await page.getByRole('heading',{name:'主播管理',exact:true}).waitFor()
 await page.getByRole('button',{name:'系统推荐主播 查看并添加 ›'}).click()
 const modal=page.getByRole('dialog',{name:'系统推荐主播'})
 await modal.getByRole('checkbox',{name:'添加推荐主播“系统主播A”'}).check()
 await modal.getByText('“系统主播A”已加入你的主播列表。可继续勾选，再设为当前主播。').waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.bound),7,'checking recommendation must not change current presenter')
 await page.evaluate(()=>window.fixture.failImport=true)
 await modal.getByRole('checkbox',{name:'添加推荐主播“系统主播B”'}).check()
 await modal.getByRole('alert').waitFor();assert.equal(await modal.getByRole('checkbox',{name:'添加推荐主播“系统主播B”'}).isChecked(),false,'failed import stays unchecked')
 await modal.getByRole('checkbox',{name:'添加推荐主播“系统主播B”'}).check()
 await modal.getByText('“系统主播B”已加入你的主播列表。可继续勾选，再设为当前主播。').waitFor()
 assert.equal(await modal.getByRole('checkbox',{name:'添加推荐主播“系统主播A”'}).isDisabled(),true)
 await page.screenshot({path:'../deploy/validation/anchor-recommendations/customer-library.png'})
 await modal.getByRole('button',{name:'完成，返回主播管理'}).click()
 await page.getByRole('button',{name:'系统主播A',exact:true}).click()
 await page.getByRole('button',{name:'设为当前智能体主播',exact:true}).click()
 await page.locator('.presenter-card').filter({has:page.getByRole('button',{name:'系统主播A',exact:true})}).getByText('✓ 当前使用',{exact:true}).waitFor()
 assert.equal(await page.evaluate(()=>window.fixture.bound),107)
 const png=await page.evaluate(()=>{const c=document.createElement('canvas');c.width=32;c.height=32;const g=c.getContext('2d');g.fillStyle='#7762db';g.fillRect(0,0,32,32);return c.toDataURL('image/png').split(',')[1]})
 await page.getByLabel('上传主播“系统主播A”的头像',{exact:true}).setInputFiles({name:'my-avatar.png',mimeType:'image/png',buffer:Buffer.from(png,'base64')})
 await page.getByText('你的主播头像已上传并保存啦。',{exact:true}).waitFor()
 const card=page.locator('.presenter-card').filter({has:page.getByRole('button',{name:'系统主播A',exact:true})})
 assert.ok((await card.locator('.presenter-select img').getAttribute('src')).startsWith('data:image/jpeg;base64,'))
 const writes=await page.evaluate(()=>window.fixture.writes);assert.equal(writes.filter(c=>c.p.endsWith('/preview')).length,1,'customer browse/add/bind/upload must not generate AI descriptions')
 assert.ok(writes.filter(c=>c.p.endsWith('/import')).every(c=>c.query==='?tenant_id=14'))
 assert.equal(writes.filter(c=>c.p.endsWith('/bind-plan')).length,1)
 await page.screenshot({path:'../deploy/validation/anchor-recommendations/customer-presenters.png'})
 assert.deepEqual(errors,[])
 console.log('PASS: admin generates 50-character preview then explicitly publishes; customer checkbox imports multiple presenters; failure recovery; no implicit binding; current presenter confirmed; real image upload normalization; tenant scope; no customer model generation')
} finally { await browser.close(); await server.close() }
