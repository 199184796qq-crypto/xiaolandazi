import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {readFile, mkdir, writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import {createServer} from 'vite'
const {chromium}=createRequire(new URL('../../collector-worker/package.json',import.meta.url))('playwright-core')
const source=await readFile(new URL('../src/views/RoomDetailView.vue',import.meta.url),'utf8')
const computed=source.slice(source.indexOf('const publicSpeechCaptionRows = computed('),source.indexOf('const speechWaveBars ='))
const template=source.slice(source.indexOf('<div class="speech-mainline-live-caption" v-if="publicSpeechCaptionRows.length">'),source.indexOf('<p v-else class="speech-runtime-unified-mainline">'))
assert.ok(computed.includes('speech_feed'));assert.ok(template.includes('row.text'))
const fixture=`<script setup lang="ts">
import {computed,ref,nextTick} from 'vue'
const roomAudioEngine=ref(null),mainlineCaptionRows=ref([])
type RoomAudioSpeechFeedItem={tone:string}
${computed}
window.caption={async set(feed){roomAudioEngine.value={speech_feed:feed};await nextTick()},async fallback(rows){mainlineCaptionRows.value=rows;roomAudioEngine.value=null;await nextTick()},rows:()=>publicSpeechCaptionRows.value}
</script><template>${template}</template>`
const id=fileURLToPath(new URL('../src/__caption-fixture.vue',import.meta.url)).replaceAll('\\','/')
const server=await createServer({server:{host:'127.0.0.1',port:0},plugins:[{name:'caption-fixture',resolveId(p){if(p==='/__caption-fixture.vue')return id},load(p){if(p===id)return fixture},configureServer(dev){dev.middlewares.use(async(req,res,next)=>{if(req.url!=='/__caption-test')return next();res.setHeader('Content-Type','text/html;charset=utf-8');res.end(await dev.transformIndexHtml(req.url,'<!doctype html><meta charset="utf-8"><link rel="icon" href="data:,"><div id="app"></div><script type="module">import {createApp} from "vue";import App from "/__caption-fixture.vue";createApp(App).mount("#app")</script>'))})}}]})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
const errors=[],warnings=[],checks=[]
try{
 const page=await browser.newPage();page.setDefaultTimeout(10000);page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='warn')warnings.push(m.text())})
 await page.goto(server.resolvedUrls.local[0]+'__caption-test');await page.waitForFunction(()=>!!window.caption)
 const item=(id,text,tone='normal')=>({segment_id:id,text,tone})
 const set=async(feed)=>{await page.evaluate(feed=>window.caption.set(feed),feed);const expected=await page.evaluate(()=>window.caption.rows().map(r=>({text:r.text,role:r.role})));const actual=await page.locator('.speech-mainline-caption-row').evaluateAll(nodes=>nodes.map(n=>({text:n.textContent,role:['previous','current','next'].find(r=>n.classList.contains('role-'+r))})));assert.deepEqual(actual,expected,'字幕DOM必须与本次三行快照完全一致，无旧红字节点')}
 await set({previous:item('cut','主线切点','cut'),current:item('interrupt-001','谢谢老粉丝的点赞。','interrupt'),next:item('resume','接着说商品','resume')})
 await set({previous:item('interrupt-001','谢谢老粉丝的点赞。','interrupt'),current:item('resume','接着说商品','resume'),next:item('main-2','介绍套餐')})
 await set({previous:item('resume','接着说商品'),current:item('main-2','介绍套餐'),next:item('main-3','介绍菜品')})
 assert.equal(await page.getByText('谢谢老粉丝的点赞。',{exact:true}).count(),0);checks.push('正常插播、恢复主线、推进下一句后旧互动消失')
 // Repeated/recycled descriptor IDs must never determine the identity of a display slot.
 await set({previous:item('interrupt-001','旧互动1','interrupt'),current:item('interrupt-002','旧互动2','interrupt'),next:item('interrupt-003','旧互动3','interrupt')})
 await set({previous:item('interrupt-002','已结束互动','interrupt'),current:item('interrupt-001','当前互动','interrupt'),next:item('interrupt-001','下一互动','interrupt')})
 await set({previous:item('main-1','商品上一句'),current:item('main-2','商品当前句'),next:item('main-3','商品下一句')})
 assert.equal(await page.locator('.transition-interrupt').count(),0);checks.push('重复/复用片段编号后回归主线无孤立红字')
 for(let n=0;n<150;n++){
  const tone=n%3===0?'interrupt':'normal'
  await set({previous:item('reused-'+n%2,'上句'+n,tone),current:item('reused-'+(n+1)%2,'当前'+n,tone),next:item('reused-'+n%2,'下句'+n,tone)})
 }
 checks.push('150轮重复编号与互动/主线交替，DOM严格匹配最新三行')
 await set({});assert.equal(await page.locator('.speech-mainline-caption-row').count(),0)
 await set({current:item('main-1','新场第一句')});assert.equal(await page.locator('.speech-mainline-caption-row').count(),1);checks.push('停止清空与新场只有当前一句，不继承上一场节点')
 for(let n=0;n<8;n++){
  await page.evaluate(async n=>window.caption.fallback(['previous','current','next'].map(role=>({segment:{segment_id:'recycled',text:role+n},role,transition:''}))),n)
  assert.deepEqual(await page.locator('.speech-mainline-caption-row').allTextContents(),['previous'+n,'current'+n,'next'+n])
 }
 await set({current:item('main-1','Core恢复后的当前句')});assert.deepEqual(await page.locator('.speech-mainline-caption-row').allTextContents(),['Core恢复后的当前句']);checks.push('主线后备字幕重复编号与Core字幕恢复同样不留旧节点')
 assert.deepEqual(errors,[]);assert.deepEqual(warnings,[])
 const output=process.env.CAPTION_TEST_OUTPUT
 if(output){await mkdir(output,{recursive:true});await writeFile(output+'/checks.json',JSON.stringify({checks,errors,warnings},null,2))}
 console.log(JSON.stringify({checks,errors,warnings},null,2))
}catch(error){console.error({errors,warnings});throw error}finally{await browser.close();await server.close()}
