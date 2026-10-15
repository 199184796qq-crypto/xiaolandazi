import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { createSSRApp } from 'vue'
import { renderToString } from 'vue/server-renderer'
let server, component, avatars
before(async () => {
  server = await createServer({ server: { middlewareMode: true }, appType: 'custom' })
  component = (await server.ssrLoadModule('/src/components/AnchorPresenterManager.vue')).default
  avatars = await server.ssrLoadModule('/src/anchorAvatar.ts')
})
after(async () => { await server?.close() })
const style = { id: 7, name: '小林主播', description: '真实话术样本', avatar_type: 'female', sample_count: 1, training_count: 3 }
const props = { styles: [style], selectedId: 7, busy: false, editingId: null, savingNameId: null, avatarSavingIds: [], nameDraft: '', hasSelection: true, hasPlan: true }
test('presenter manager shows real counts and four per-presenter avatar options', async () => {
  const html = await renderToString(createSSRApp(component, props))
  for (const text of ['主播管理', '创建和管理你的专属主播', '＋新建主播', '小林主播', '1 份样本 · 3 次训练', '设为当前智能体主播', '当前系统的个性插件', '/assets/anchor-avatars/female-v1.png']) assert.ok(html.includes(text), text)
  assert.equal((html.match(/<select\b/g) || []).length, 0)
  assert.ok(html.includes('选择 ▾'))
  assert.deepEqual(avatars.anchorAvatarOptions.map(o => o.label), ['男', '女', '中年男', '中年女'])
  assert.equal(avatars.anchorAvatarURL('unknown'), '/assets/anchor-avatars/female-v1.png')
})
test('empty list and no-plan/saving states retain existing action guards', async () => {
  const empty = await renderToString(createSSRApp(component, { ...props, styles: [], hasSelection: false }))
  assert.ok(empty.includes('还没有保存主播'))
  assert.equal(empty.includes('manager-bind'), false)
  const saving = await renderToString(createSSRApp(component, { ...props, avatarSavingIds: [7], hasPlan: false }))
  assert.match(saving, /<button[^>]*class="avatar-menu-button"[^>]*disabled/)
  assert.ok(saving.includes('先选择智能体方案'))
})

test('bound presenter sorts first with usage check and cancel pill independently of selected card', async () => {
  const html=await renderToString(createSSRApp(component,{...props,styles:[style,{...style,id:8,name:'另一主播'}],selectedId:7,currentId:8}))
  assert.ok(html.indexOf('data-presenter-id="8"')<html.indexOf('data-presenter-id="7"'))
  assert.equal((html.match(/<article\b[^>]*class="[^"]*\bis-current\b[^"]*"/g)||[]).length,1)
  assert.ok(html.includes('取消使用主播“另一主播”'))
  assert.ok(html.includes('当前使用主播“另一主播”，点击查看'))
  assert.ok(html.includes('设为当前智能体主播'),'another selection can replace the bound presenter')
})

test('viewing a card never marks it as in use; current selection cannot be rebound accidentally', async () => {
  const unbound=await renderToString(createSSRApp(component,{...props,currentId:0}))
  assert.equal(unbound.includes('取消使用'),false)
  assert.equal(unbound.includes('presenter-check checked'),false)
  const current=await renderToString(createSSRApp(component,{...props,currentId:7,bindingBusy:true}))
  assert.match(current,/<button[^>]*class="presenter-unbind"[^>]*disabled/)
  assert.equal(current.includes('class="manager-bind"'),false)
})

test('intro is independent, defaults by displayed avatar and truncates at 30 characters', async () => {
 const empty = await renderToString(createSSRApp(component, { ...props, styles: [{...style, description: ''}] }))
 assert.ok(empty.includes('她什么也没介绍'))
 const male = await renderToString(createSSRApp(component, { ...props, styles: [{...style, description: '', avatar_type: 'male'}] }))
 assert.ok(male.includes('他什么也没介绍'))
 const long = await renderToString(createSSRApp(component, { ...props, styles: [{...style, description: '甲'.repeat(40)}] }))
 assert.ok(long.includes('甲'.repeat(30) + '</p>'))
})
