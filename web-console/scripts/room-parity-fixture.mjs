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
        import {createApp} from 'vue'; import {createRouter,createMemoryHistory} from 'vue-router';
        import View from '/src/views/RoomDetailView.vue'; import {applySession} from '/src/session.ts';
        import {coreRuntime} from '/src/coreRuntime.ts'; import '/src/style.css';
        const role=new URLSearchParams(location.search).get('role')||'customer';
        window.fixture={calls:[],queue:[],caption:true,granted:role!=='ungranted'};
        const room={id:15,tenant_id:14,name:'菜籽油',platform:'douyin',external_room_id:'529757346891',status:'offline',monitor_enabled:false,online_count:64,cooperation_status:'cooperating'};
        const now=new Date().toISOString();
        window.fetch=async(url,options={})=>{
          const path=new URL(url,location.origin).pathname;window.fixture.calls.push({path,method:options.method||'GET'});
          if(options.method&&options.method!=='GET')throw Error('Unexpected mutation '+path);
          if(path==='/api/v1/liveops/support-authorizations')return Response.json({items:window.fixture.granted?[{staff_user_id:7,room_id:15,tenant_id:14,capability:'l3_policy',status:'active'}]:[]});
          if(path==='/api/v1/rooms/15')return Response.json(room);
          if(path.endsWith('/events')&&!path.includes('/runtime/'))return Response.json({items:[{id:1,room_id:15,event_type:'chat',nickname:'测试老乡',content:'这个油怎么保存',occurred_at:now}]});
          if(path.endsWith('/runtime/events'))return Response.json([]);
          if(path.endsWith('/runtime'))return Response.json({room_id:15,room_live:false,agent_state:'stopped',agent_mode:'anchor',quota_remaining_seconds:7200});
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
        createApp(View).use(router).mount('#app');
      </script></body></html>`))
    })
  } }
  const server = await createServer({root,configFile:false,plugins:[vue(),plugin],server:{host:'127.0.0.1',port:18601,strictPort:true},logLevel:'error'})
  await server.listen()
  return server
}
