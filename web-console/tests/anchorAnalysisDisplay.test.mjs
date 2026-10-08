import test, {before,after} from 'node:test'
import assert from 'node:assert/strict'
import {createServer} from 'vite'
import {createSSRApp} from 'vue'
import {renderToString} from 'vue/server-renderer'
let server,component
before(async()=>{server=await createServer({server:{middlewareMode:true},appType:'custom'});component=(await server.ssrLoadModule('/src/components/AnchorStyleAnalysisDisplay.vue')).default})
after(async()=>{await server?.close()})
test('overall result uses collapsible returned sections without raw prompt disclosure',async()=>{
 const html=await renderToString(createSSRApp(component,{text:'【口语风格】\n1. 称呼：同学们\n【情绪】\n1. 基调：温暖',editable:true}))
 assert.equal((html.match(/<details\b/g)||[]).length,2)
 assert.ok(html.includes('删除大项'))
 assert.ok(html.includes('修改分析小项'))
 assert.equal(html.includes('原始元提示词'),false)
 assert.equal(html.includes('· 主播元提示词'),false)
 assert.equal(html.includes('展开 / 收起'),false)
 assert.match(html, /class="section-title-text"[^>]*>.*?<\/span><span class="fold-triangle"[^>]*aria-hidden="true"/)
})
test('sample result is read-only and has no version mutation controls',async()=>{
 const html=await renderToString(createSSRApp(component,{text:'【情绪】\n1. 基调：温暖',title:'样本分析结果'}))
 assert.ok(html.includes('样本分析结果'))
 assert.equal(html.includes('修改分析小项'),false)
 assert.equal(html.includes('删除大项'),false)
 assert.equal(html.includes('原始元提示词'),false)
})
