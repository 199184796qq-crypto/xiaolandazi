// Local-only UI fixture. No credentials, provider calls or production writes.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export async function startFixture() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const fixturePlugin = { name: 'model-picker-fixture', configureServer(server) {
  server.middlewares.use(async (req, res, next) => {
    if (req.url !== '/__model-picker.html') return next()
    res.setHeader('Content-Type', 'text/html; charset=utf-8')
    res.end(await server.transformIndexHtml('/__model-picker.html', `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>模型选择测试</title><body><div id="app"></div><script type="module">
      import {createApp} from 'vue';
      import View from '/src/views/SpeechModelsView.vue';
      import '/src/style.css';
      window.fixture={calls:[],delay:0,fail:false,empty:false};
      const profile={id:'fixture-profile',name:'测试模型配置',protocol:'openai_chat',base_url:'https://example.invalid/v1',model:'model-01',enabled:true,timeout_ms:120000,max_tokens:16000,system_prompt:'',has_api_key:true};
      const models=Array.from({length:23},(_,i)=>({id:'model-'+String(i+1).padStart(2,'0'),name:i===11?'Special 模型':'测试模型 '+(i+1)}));
      window.fetch=async(url,options={})=>{
        window.fixture.calls.push({url,method:options.method||'GET'});
        if(url==='/api/v1/system/speech-models'&&!options.method)return Response.json({config:{profiles:[{...profile}],default_id:'',revision:1},key_storage_ready:true});
        if(url==='/api/v1/system/speech-models/models'){
          await new Promise(resolve=>setTimeout(resolve,window.fixture.delay));
          return window.fixture.fail?Response.json({error:'模拟获取失败'},{status:502}):Response.json({models:window.fixture.empty?[]:models,truncated:false});
        }
        throw new Error('Unexpected fixture request');
      };
      createApp(View).mount('#app');
    </script></body></html>`))
  })
  } }
  const server = await createServer({ root, configFile: false, plugins: [vue(), fixturePlugin], server: { host: '127.0.0.1', port: 18598, strictPort: true }, logLevel: 'error' })
  await server.listen()
  return server
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await startFixture()
  console.log('Fixture ready: http://127.0.0.1:18598/__model-picker.html')
}
