import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export async function startFixture() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const plugin = { name: 'room-parity-fixture', configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (!req.url?.startsWith('/__room-parity.html')) return next()
      res.setHeader('Content-Type', 'text/html; charset=utf-8')
      res.end(await server.transformIndexHtml('/__room-parity.html', `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><body><div id="app-global-breadcrumbs"></div><div id="app-global-switches"></div><div id="app"></div><script type="module">
        import {createApp,h,Fragment} from 'vue'; import {createRouter,createMemoryHistory} from 'vue-router';
        import View from '/src/views/RoomDetailView.vue'; import GlobalFeedback from '/src/components/GlobalFeedback.vue'; import {applySession} from '/src/session.ts';
        import {coreRuntime} from '/src/coreRuntime.ts'; import '/src/style.css';
        const params=new URLSearchParams(location.search);const role=params.get('role')||'customer';
        window.fixture={calls:[],queue:[],caption:params.get('caption')==='1'||params.get('waiting')!=='1',waiting:params.get('waiting')==='1',granted:role!=='ungranted',session:1,pcm:params.get('pcm')==='1',sources:[],startTest:params.get('start_test')==='1',delayUnlock:params.get('delay_unlock')==='1',startFailed:false,runtimePollLag:false};
        if(window.fixture.pcm){
          window.AudioContext=class {
            constructor(){this.state=window.fixture.delayUnlock?'suspended':'running';this.destination={};this.origin=performance.now();}
            get currentTime(){return (performance.now()-this.origin)/1000;}
            async resume(){if(window.fixture.delayUnlock)await new Promise(resolve=>window.fixture.releaseUnlock=()=>{window.fixture.delayUnlock=false;resolve()});this.state='running';} async suspend(){this.state='suspended';} async close(){this.state='closed';}
            createGain(){return {context:this,gain:{value:1,setValueAtTime(){}},connect(){},disconnect(){}};}
            createBuffer(){const data=new Float32Array(480);return {getChannelData:()=>data};}
            createBufferSource(){const context=this;let timer;const source={buffer:null,onended:null,connect(){},disconnect(){},stop(){clearTimeout(timer);},start(at){window.fixture.sources.push({sound:source.buffer.getChannelData(0).some(v=>v!==0),at});timer=setTimeout(()=>source.onended?.(),Math.max(0,(at-context.currentTime+.02)*1000));}};return source;}
          };
        }
        const room={id:15,tenant_id:14,name:'菜籽油',platform:'douyin',external_room_id:'529757346891',status:'offline',monitor_enabled:false,online_count:64,cooperation_status:'cooperating'};
        const now=new Date().toISOString();
        window.fetch=async(url,options={})=>{
          const path=new URL(url,location.origin).pathname;window.fixture.calls.push({path,method:options.method||'GET'});
          if(path.includes('/v1/receivers/'))return Response.json({});
          if(path.endsWith('/composite.pcm'))return new Response(new ReadableStream({start(controller){window.fixture.pcmController=controller;}}),{headers:{'Content-Type':'application/octet-stream'}});
          if(window.fixture.startTest&&path.endsWith('/runtime/start')){if(window.fixture.startFailed)return Response.json({error:'模拟启动失败'},{status:500});window.fixture.waiting=true;return Response.json({});}
          if(window.fixture.startTest&&path.endsWith('/runtime/stop')){window.fixture.waiting=false;return Response.json({});}
          if(options.method&&options.method!=='GET')throw Error('Unexpected mutation '+path);
          if(path==='/api/v1/liveops/support-authorizations')return Response.json({items:window.fixture.granted?[{staff_user_id:7,room_id:15,tenant_id:14,capability:'l3_policy',status:'active'}]:[]});
          if(path==='/api/v1/rooms/15')return Response.json(room);
          if(path.endsWith('/events')&&!path.includes('/runtime/'))return Response.json({items:[{id:1,room_id:15,event_type:'chat',nickname:'测试老乡',content:'这个油怎么保存',occurred_at:now}]});
          if(path.endsWith('/runtime/events'))return Response.json([]);
          if(path.endsWith('/runtime'))return Response.json({room_id:15,room_live:window.fixture.waiting,agent_state:window.fixture.failure?'stopped':window.fixture.waiting&&!window.fixture.runtimePollLag?'working':'stopped',agent_mode:'anchor',agent_plan_id:window.fixture.startTest?4:0,session:{id:window.fixture.session},failure:window.fixture.failure,startup:window.fixture.startup,quota_remaining_seconds:7200});
          if(path.endsWith('/audio-engine'))return Response.json({room_id:15,phase:'idle',speech_feed:window.fixture.caption?{previous:{segment_id:'prev',text:'上一句主线',tone:'mainline'},current:{segment_id:'current',text:'正在回答保存问题哟',tone:'interrupt'},next:{segment_id:'next',text:'下一句回归主线',tone:'resume'}}:{}});
          if(path.endsWith('/agent-decisions'))return Response.json({queue:window.fixture.queue,notes:[]});
          if(path.endsWith('/brain'))return Response.json({Topics:[],Counters:{},Questions:[]});
          if(path.endsWith('/speech-runtime'))return Response.json({room_id:15,programs:[],tasks:[]});
          if(path.endsWith('/session-stats'))return Response.json({room_id:15,counts:{},online_count:64});
          if(path.endsWith('/capture'))return Response.json({mode:'idle'});
          if(path.endsWith('/speech-analysis'))return Response.json({configured:false});
          return Response.json({items:[],count:0});
        };
        const bootstrap={actor:{user_id:role==='customer'?14:7,username:'tester',display_name:'测试账号',role:role==='customer'?'customer':'staff',...(role==='customer'?{tenant_id:14}:{})},tenants:[{id:14,name:'测试客户'}],environment:'test',staff_access:role==='customer'?null:{is_super_admin:false,permissions:['liveops.view_all','liveops.configure','livepolicy.manage_l2','livepolicy.manage_l3_authorized'],permission_scopes:{},permission_group_ids:{},group_ids:[],managed_group_ids:[]}};
        const router=createRouter({history:createMemoryHistory(),routes:[{path:'/rooms/:id',component:View}]});
        await router.push('/rooms/15');await router.isReady();coreRuntime.phase='online';applySession(bootstrap);
        createApp({render:()=>h(Fragment,[h(View),h(GlobalFeedback)])}).use(router).mount('#app');
      </script></body></html>`))
    })
  } }
  const server = await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:18601,strictPort:true},logLevel:'error'})
  await server.listen()
  return server
}
