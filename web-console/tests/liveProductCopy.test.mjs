import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { readFile } from 'node:fs/promises'
let server,component
before(async()=>{server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});component=(await server.ssrLoadModule('/src/components/LiveProductLinkCards.vue')).default})
after(async()=>{await server?.close()})
test('each formal product card has a separate copy action; source fields are unchanged',async()=>{
 const items=[{id:7,link_key:'1号链接',product_name:'杨鸭子',version_no:3,status:'active',attributes:[],room_roles:['main']},{id:9,link_key:'10号链接',product_name:'半只装',version_no:2,status:'active',attributes:[],room_roles:['traffic']}]
 const snapshot=JSON.stringify(items),noop=async()=>{}
 const html=await renderToString(createSSRApp(component,{items,saveField:noop,saveRoomRoles:noop,removeItem:noop,copyItem:noop,addAttribute:noop,saveAttribute:noop,removeAttribute:noop}))
 assert.ok(html.includes('复制1号链接商品卡'));assert.ok(html.includes('复制10号链接商品卡'))
 assert.equal((html.match(/class="product-copy"/g)||[]).length,2)
 assert.equal(JSON.stringify(items),snapshot)
})
test('benefit copy uses scoped API and inserts after source without rewriting original',async()=>{
 const source=await readFile(new URL('../src/views/LiveStrategyView.vue',import.meta.url),'utf8')
 assert.ok(source.includes('copy-item="duplicateProductLink"'))
 assert.ok(source.includes('duplicateFormalBenefit(item)'))
 assert.ok(source.includes('copyLiveAgentPlanBenefit(plan, item)'))
 assert.ok(source.includes('activeRoom.value?.tenant_id !== tenant'))
 assert.ok(source.includes('cards.splice(index < 0 ? cards.length : index + 1, 0, created)'))
 const api=await readFile(new URL('../src/api.ts',import.meta.url),'utf8')
 for(const endpoint of ["'/product-links/' + item.id + '/copy'","'/benefits/' + item.id + '/copy'"])assert.ok(api.includes(endpoint))
 assert.ok(api.includes('expected_version_no: item.version_no'))
})
