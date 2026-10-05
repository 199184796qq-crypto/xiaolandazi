import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

export async function startFixture() {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const fixture = { name: 'product-cards-fixture', configureServer(server) {
    server.middlewares.use(async (req, res, next) => {
      if (req.url !== '/__product-cards.html') return next()
      res.setHeader('Content-Type', 'text/html; charset=utf-8')
      res.end(await server.transformIndexHtml(req.url, `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>商品卡片测试</title><body style="margin:0;padding:28px;background:#edf0ff;font-family:Arial"><main style="max-width:1320px;margin:auto"><div id="app"></div><button id="outside" style="margin-top:24px">编辑完成，移开焦点</button></main><script type="module">
        import {createApp,ref,h} from 'vue'; import Cards from '/src/components/LiveProductLinkCards.vue'; import '/src/style.css';
        import {updateLiveAgentPlanProductLink,deleteLiveAgentPlanProductLink} from '/src/api.ts';
        const items=ref([
          {id:1,tenant_id:14,plan_id:20,link_key:'1号链接',product_name:'四川老油坊熟榨菜籽油',spec:'5L，另有2桶、4桶组合',daily_price:'129.9元',quantity:'1桶/2桶/4桶',audience:'喜欢传统菜籽香的家庭',status:'active',version_no:2},
          {id:2,tenant_id:14,plan_id:20,link_key:'2号链接',product_name:'日常通勤圆领上衣',spec:'S/M/L/XL，米白/黑色',daily_price:'99元',quantity:'1件',audience:'通勤、日常休闲穿着',status:'active',version_no:5}
        ]);
        window.fixture={items,calls:[],failSave:false,failDelete:false,delay:0,confirmation:true};
        window.confirm=()=>window.fixture.confirmation;
        window.fetch=async(url,options={})=>{
          const body=options.body?JSON.parse(options.body):null;
          window.fixture.calls.push({url,method:options.method,body});
          await new Promise(resolve=>setTimeout(resolve,window.fixture.delay));
          if(options.method==='PATCH'){
            if(window.fixture.failSave)return Response.json({error:'保存失败，请重试'},{status:503});
            const item=items.value.find(item=>url.split('?')[0].endsWith('/'+item.id));
            if(body.expected_version_no!==item.version_no)return Response.json({error:'版本冲突'},{status:409});
            return Response.json({...item,...body,version_no:item.version_no+1});
          }
          if(options.method==='DELETE')return window.fixture.failDelete?Response.json({error:'删除失败，请重试'},{status:503}):new Response(null,{status:204});
          throw Error('Unexpected request');
        };
        async function saveField(item,field,value){
          const result=await updateLiveAgentPlanProductLink(item.plan_id,item.id,{expected_version_no:item.version_no,link_key:item.link_key,product_name:item.product_name,spec:item.spec,daily_price:item.daily_price,quantity:item.quantity,audience:item.audience,[field]:value},item.tenant_id);
          items.value=items.value.map(current=>current.id===item.id?result:current);
        }
        async function removeItem(item){await deleteLiveAgentPlanProductLink(item.plan_id,item.id,item.tenant_id);items.value=items.value.filter(current=>current.id!==item.id);}
        createApp({setup:()=>()=>h(Cards,{items:items.value,saveField,removeItem})}).mount('#app');
      </script></body></html>`))
    })
  } }
  const server = await createServer({ root, configFile: false, plugins: [vue(), fixture], server: { host: '127.0.0.1', port: 18605, strictPort: true }, logLevel: 'error' })
  await server.listen()
  return server
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  await startFixture()
  console.log('Product cards fixture: http://127.0.0.1:18605/__product-cards.html')
}
