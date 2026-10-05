import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
export async function startFixture() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const plugin = { name: 'ops-room-fixture', configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (req.url !== '/__ops-rooms.html') return next()
      res.setHeader('Content-Type', 'text/html; charset=utf-8')
      res.end(await server.transformIndexHtml('/__ops-rooms.html', `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>运维直播间列表测试</title><body style="padding:30px"><div id="app-global-breadcrumbs"></div><div id="app-global-switches"></div><div id="app"></div><script type="module">
        import {createApp} from 'vue'; import {createRouter,createMemoryHistory} from 'vue-router';
        import View from '/src/views/RoomsView.vue'; import {applySession,session} from '/src/session.ts';
        import {coreRuntime} from '/src/coreRuntime.ts'; import {feedbackState,resolveConfirm} from '/src/uiFeedback.ts'; import '/src/style.css';
        window.fixture={calls:[],phoneDelay:0,phoneFail:false,phoneEmpty:false,grants:true,feedbackState,resolveConfirm};
        const rooms=[{id:15,tenant_id:14,name:'菜籽油',customer_name:'小蓝客户',external_room_id:'529757346891',platform:'douyin',status:'live',monitor_enabled:true,online_count:64,cooperation_status:'cooperating'},{id:16,tenant_id:15,name:'灰枣直播间',customer_name:'张三',external_room_id:'123456789',platform:'douyin',status:'offline',monitor_enabled:false,online_count:0,cooperation_status:'non_cooperating'}];
        const bootstrap={actor:{user_id:7,username:'operator',display_name:'小张',role:'staff'},tenants:[],environment:'test',staff_access:{is_super_admin:false,permissions:['liveops.view_all','liveops.configure','livepolicy.manage_l2','livepolicy.manage_l3_authorized'],role_codes:[],permission_scopes:{},permission_group_ids:{},group_ids:[],managed_group_ids:[]}};
        window.fetch=async(url,options={})=>{
          window.fixture.calls.push({url,method:options.method||'GET'});
          if(url==='/api/v1/rooms')return Response.json({items:rooms});
          if(url==='/api/v1/liveops/support-authorizations')return Response.json({items:window.fixture.grants?[{staff_user_id:7,room_id:15,tenant_id:14,capability:'l3_policy',status:'active'}]:[]});
          if(url.endsWith('/customer-contact')){
            await new Promise(resolve=>setTimeout(resolve,window.fixture.phoneDelay));
            return window.fixture.phoneFail?Response.json({error:'读取电话失败'},{status:503}):Response.json({tenant_id:url.includes('/15/')?14:15,name:'测试客户',phone:window.fixture.phoneEmpty?'':'13800000000'});
          }
          if(options.method&&options.method!=='GET')throw Error('Unexpected mutation');
          return Response.json({items:[],count:0});
        };
        const router=createRouter({history:createMemoryHistory(),routes:[{path:'/:pathMatch(.*)*',component:View}]});
        await router.push('/rooms');await router.isReady();window.fixture.router=router;
        coreRuntime.phase='online';applySession(bootstrap);
        window.fixture.setCustomer=()=>applySession({...bootstrap,actor:{...bootstrap.actor,role:'customer',tenant_id:14},tenants:[{id:14,name:'小蓝客户'}],staff_access:null});
        window.fixture.revoke=()=>{window.fixture.grants=false;applySession({...bootstrap});};
        createApp(View).use(router).mount('#app');
      </script></body></html>`))
    })
  } }
  const server = await createServer({ root, configFile: false, plugins: [vue(), plugin], server: { host: '127.0.0.1', port: 18599, strictPort: true }, logLevel: 'error' })
  await server.listen()
  return server
}
