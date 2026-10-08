import assert from 'node:assert/strict'
import {createRequire} from 'node:module'
import {mkdir,writeFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import {createServer} from 'vite'
import vue from '@vitejs/plugin-vue'
const require=createRequire(new URL('../../collector-worker/package.json',import.meta.url)),{chromium}=require('playwright-core')
const root=fileURLToPath(new URL('..',import.meta.url)),output=fileURLToPath(new URL('../../work/voice-clone-fix',import.meta.url))
await mkdir(output,{recursive:true})
const plugin={name:'voice-clone-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
  if(!req.url?.startsWith('/__voice-clone.html'))return next()
  res.setHeader('Content-Type','text/html; charset=utf-8')
  res.end(await server.transformIndexHtml('/__voice-clone.html',`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><body><div id="app"></div><script type="module">
    import {createApp} from 'vue';import Voice from '/src/components/LiveVoiceCenter.vue';import '/src/style.css';
    window.fixture={calls:[],profiles:[],fail:true};
    window.fetch=async(url,init={})=>{
      const path=new URL(url,location.origin).pathname;
      if(new URL(url,location.origin).origin!==location.origin)throw Error('Unexpected browser request to supplier');
      window.fixture.calls.push({path,method:init.method||'GET'});
      if(path.endsWith('/media-assets')&&init.method==='POST'){
        window.fixture.uploadedDuration=Number(init.body.get('duration_ms'));
        await new Promise(resolve=>window.fixture.finishUpload=resolve);
        return Response.json({asset:{id:701},content_url:'/api/v1/live/media-assets/701/content'},{status:201});
      }
      if(path.endsWith('/voice-profiles/clone')){
        window.fixture.payload=JSON.parse(init.body);
        await new Promise(resolve=>window.fixture.finishClone=resolve);
        if(window.fixture.fail)return Response.json({error:'生成声音复刻公网样本地址失败'},{status:502});
        const profile={id:41,name:window.fixture.payload.name,clone_status:'ready',config:{target_model:window.fixture.payload.model},created_at:new Date().toISOString()};
        window.fixture.profiles.push(profile);return Response.json(profile,{status:201});
      }
      if(path.endsWith('/official-voices'))return Response.json({items:[]});
      if(path.endsWith('/voice-profiles/quota'))return Response.json({limit:3,used:window.fixture.profiles.length,remaining:3-window.fixture.profiles.length});
      if(path.endsWith('/voice-profiles'))return Response.json(window.fixture.profiles);
      return Response.json([]);
    };
    createApp(Voice,{roomId:26,roomName:'测试房间'}).mount('#app');
  </script></body></html>`))
})}}
const server=await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:18603,strictPort:true},logLevel:'error'})
await server.listen()
const browser=await chromium.launch({executablePath:'C:/Program Files/Google/Chrome/Application/chrome.exe',headless:true})
function wav(seconds){
  const length=Math.round(seconds*16000)*2,buffer=Buffer.alloc(44+length)
  buffer.write('RIFF');buffer.writeUInt32LE(36+length,4);buffer.write('WAVEfmt ',8);buffer.writeUInt32LE(16,16)
  buffer.writeUInt16LE(1,20);buffer.writeUInt16LE(1,22);buffer.writeUInt32LE(16000,24);buffer.writeUInt32LE(32000,28)
  buffer.writeUInt16LE(2,32);buffer.writeUInt16LE(16,34);buffer.write('data',36);buffer.writeUInt32LE(length,40);return buffer
}
try{
  const page=await browser.newPage({viewport:{width:1600,height:1200}}),errors=[]
  page.on('pageerror',e=>errors.push(e.message))
  await page.goto('http://127.0.0.1:18603/__voice-clone.html')
  await page.getByRole('button',{name:'我的声音'}).click()
  await page.locator('.upload-sample-button').click()
  const dialog=page.getByRole('dialog'),generate=dialog.getByRole('button',{name:'确定并生成'}),input=dialog.locator('input[type=file]')
  await dialog.locator('input:not([type=file])').fill('小海')
  assert.match(await dialog.innerText(),/推荐 10–20 秒，最长 60 秒/)
  for(const seconds of [5,9.6,20,60]){
    await input.setInputFiles({name:seconds+'.wav',mimeType:'audio/wav',buffer:wav(seconds)})
    await page.waitForFunction(()=>document.querySelector('.voice-clone-upload small')?.textContent?.includes('时长'))
    await generate.waitFor({state:'visible'});assert.equal(await generate.isEnabled(),true,seconds+'s should be accepted')
  }
  for(const seconds of [4.9,61]){
    await input.setInputFiles({name:seconds+'.wav',mimeType:'audio/wav',buffer:wav(seconds)})
    await dialog.locator('.sample-error').waitFor();assert.equal(await generate.isEnabled(),false)
  }
  await input.setInputFiles({name:'large.wav',mimeType:'audio/wav',buffer:Buffer.alloc(10*1024*1024+1)})
  await dialog.getByText('声音样本不能超过 10MB，请压缩后重新选择。',{exact:true}).waitFor()
  await input.setInputFiles({name:'sample20.wav',mimeType:'audio/wav',buffer:wav(20)})
  await page.waitForFunction(()=>document.querySelector('.voice-clone-upload small')?.textContent?.includes('时长 20.0 秒'))
  await generate.click()
  await dialog.getByRole('status').filter({hasText:'正在上传声音样本'}).waitFor()
  assert.equal(await input.isDisabled(),true)
  assert.match(await dialog.locator('.clone-spinner').evaluate(el=>getComputedStyle(el).animationName),/clone-spin/)
  await page.evaluate(()=>window.fixture.finishUpload())
  await dialog.getByRole('status').filter({hasText:'正在处理样本并生成声音'}).waitFor()
  await page.waitForFunction(()=>!!window.fixture.finishClone)
  await page.evaluate(()=>{window.fixture.finishClone();window.fixture.finishClone=null})
  await dialog.getByRole('alert').filter({hasText:'生成声音复刻公网样本地址失败'}).waitFor()
  assert.equal(await generate.isEnabled(),true,'failed generation should allow retry')
  await page.screenshot({path:output+'/visible-clone-error.png',fullPage:true})
  await page.evaluate(()=>{window.fixture.fail=false;window.fixture.finishUpload=null})
  await generate.click();await page.waitForFunction(()=>!!window.fixture.finishUpload)
  await page.evaluate(()=>window.fixture.finishUpload());await page.waitForFunction(()=>!!window.fixture.finishClone)
  await page.evaluate(()=>window.fixture.finishClone())
  await dialog.waitFor({state:'hidden'});await page.locator('.custom-voice-row').filter({hasText:'小海'}).waitFor()
  const result=await page.evaluate(()=>({calls:window.fixture.calls,duration:window.fixture.uploadedDuration,payload:window.fixture.payload}))
  assert.equal(result.duration,20000);assert.equal(result.payload.sample_asset_id,701)
  assert.deepEqual(errors,[])
  await writeFile(output+'/browser-result.json',JSON.stringify({passed:true,...result},null,2))
  console.log('Voice clone browser regression passed: 5–60s, 10MB, loading, visible failure, retry, save and same-origin requests.')
}finally{await browser.close();await server.close()}
