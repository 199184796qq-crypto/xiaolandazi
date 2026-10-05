import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

// Exercise the actual workspace guards without calling paid AI or a server.
const component = readFileSync(new URL('../src/views/LiveStrategyView.vue', import.meta.url), 'utf8')
const script = component.match(/<script setup[^>]*>([\s\S]*?)<\/script>/)?.[1]
assert.ok(script)
const tree = ts.createSourceFile('workspace.ts', script, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TS)
const names = ['startFullShowVariantEdit', 'saveFullShowVariantEdit', 'generateFullShowVariantVoice', 'regenerateFullShowVariant', 'adoptFullShowVoiceCandidate', 'handleLiveVoiceChanged']
const functions = names.map((name) => {
  const node = tree.statements.find((item) => ts.isFunctionDeclaration(item) && item.name?.text === name)
  assert.ok(node, name + ' must exist')
  return 'export ' + node.getText(tree)
})
const identityDeclaration = tree.statements.flatMap((node) => ts.isVariableStatement(node) ? [...node.declarationList.declarations] : [])
  .find((node) => node.name.getText(tree) === 'fullShowVoiceIdentityConsistent')
assert.ok(identityDeclaration)
const fixture = `
export const isUserAudioContentMode = { value: true }
const contentModeReady = { value: true }
export const fullShowNotice = { value: '' }
export const fullShowWorkspaceVoiceIdentity = { value: { voice_id: 'recording-clone' } }
export const fullShowSavedVersion = { value: { id: 7 } }
const fullShowVersionError = { value: '' }
const fullShowResult = { value: { variants: [{ variant_key: 'legacy-recording-key' }] } }
const fullShowFormalVariantKeys = { value: ['legacy-recording-key'] }
export const recording = { audio_url: '/api/v1/live/media-assets/42/content', audio_asset_id: 42, voice_identity_key: 'recorded-speaker', timeline: [{ text: '原始声音', start_ms: 0, end_ms: 1000 }] }
const fullShowVoiceState = () => recording
const currentVoiceIdentityKey = { value: 'new-interactive-voice' }
const computed = (fn) => ({ get value() { return fn() } })
export let voiceConfigReloads = 0
const refreshAgentVersions = async () => { voiceConfigReloads++ }
export const ${identityDeclaration.getText(tree)}
${functions.join('\n')}
`
const { outputText } = ts.transpileModule(fixture, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
const workspace = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))
const original = structuredClone(workspace.recording)
assert.equal(workspace.fullShowVoiceIdentityConsistent.value, true, 'recorded speaker may differ from interactive voice without requiring TTS')
for (const name of names.filter((name) => name !== 'handleLiveVoiceChanged')) {
  await workspace[name]({ variant_key: 'legacy-recording-key' })
}
await workspace.handleLiveVoiceChanged()
assert.equal(workspace.voiceConfigReloads, 1)
assert.equal(workspace.fullShowSavedVersion.value, null, 'interactive voice change needs a newly saved version')
assert.equal(workspace.fullShowWorkspaceVoiceIdentity.value, null, 'new interactive identity comes from room voice config')
assert.deepEqual(workspace.recording, original, 'changing interactive voice must preserve recording asset, URL, timeline and recorded identity')
workspace.isUserAudioContentMode.value = false
assert.equal(workspace.fullShowVoiceIdentityConsistent.value, false, 'AI mainline must still match selected voice identity')
console.log('Recorded mainline: no TTS/script regeneration, legacy arbitrary key, independent interactive voice, unchanged original audio passed')
