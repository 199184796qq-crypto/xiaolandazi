import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export async function startFixture() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const plugin = { name: 'room-number-fixture', configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (req.url !== '/__room-number.html') return next()
      res.setHeader('Content-Type', 'text/html; charset=utf-8')
      res.end(await server.transformIndexHtml('/__room-number.html', `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>直播间房间号测试</title><body style="padding:16px"><div id="app-global-breadcrumbs"></div><div id="app-global-switches"></div><div id="app"></div><script type="module">
        import {createApp} from 'vue'; import {createRouter,createMemoryHistory} from 'vue-router';
        import View from '/src/views/RoomsView.vue'; import {applySession} from '/src/session.ts';
        import {coreRuntime} from '/src/coreRuntime.ts'; import '/src/style.css';
        const rooms=[{id:29,tenant_id:14,name:'鹅',external_room_id:'521958972384',platform:'douyin',status:'stopped',monitor_enabled:false,online_count:0},{id:30,tenant_id:14,name:'正在直播',external_room_id:'999888777',platform:'douyin',status:'live',monitor_enabled:true,online_count:64}];
        window.fixture={calls:[],saveError:'',delay:0,holdList:false,releaseList:null,rooms};
        const bootstrap={actor:{user_id:7,username:'customer',display_name:'客户',role:'customer',tenant_id:14},tenants:[{id:14,name:'客户'}],environment:'test',staff_access:null};
        window.fetch=async(url,options={})=>{
          const method=options.method||'GET';window.fixture.calls.push({url,method,body:options.body?JSON.parse(options.body):null});
          if(url==='/api/v1/rooms'){
            const snapshot=structuredClone(rooms);
            if(window.fixture.holdList)await new Promise(resolve=>{window.fixture.releaseList=resolve;});
            return Response.json({items:snapshot});
          }
          if(method==='PATCH'&&url==='/api/v1/rooms/29/number'){
            await new Promise(resolve=>setTimeout(resolve,window.fixture.delay));
            if(window.fixture.saveError)return Response.json({error:window.fixture.saveError},{status:409});
            const input=JSON.parse(options.body),room=rooms[0];
            if(input.expected_external_room_id!==room.external_room_id)return Response.json({error:'房间号已被修改，请刷新后重试'},{status:409});
            room.external_room_id=input.external_room_id.trim();room.source_url='https://live.douyin.com/'+room.external_room_id;
            return Response.json(structuredClone(room));
          }
          if(method!=='GET')throw Error('Unexpected mutation: '+url);
          return Response.json({items:[],count:0});
        };
        const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:View}]});
        await router.push('/rooms');await router.isReady();window.fixture.router=router;
        window.fixture.setStaff=()=>applySession({...bootstrap,actor:{user_id:9,role:'staff'},staff_access:{permissions:['liveops.view_all'],is_super_admin:false}});
        coreRuntime.phase='online';applySession(bootstrap);createApp(View).use(router).mount('#app');
      </script></body></html>`))
    })
  } }
  const server = await createServer({ root, configFile: false, plugins: [vue(), plugin], server: { host: '127.0.0.1', port: 18625, strictPort: true }, logLevel: 'error' })
  await server.listen()
  return server
}
