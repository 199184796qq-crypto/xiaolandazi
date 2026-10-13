import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/liveContentModeOptions.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } })
const { visibleLiveContentModes, canSelectLiveContentMode } = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))
assert.equal(canSelectLiveContentMode(null, 'ai_dynamic'), false)
for (const selected of ['user_audio', 'ai_pregenerated', 'ai_dynamic']) {
  for (const legacyGrant of [false, true]) {
    const access = { content_mode: selected, available_modes: ['user_audio', 'ai_pregenerated', 'ai_dynamic'], dynamic_authorized: legacyGrant }
    assert.deepEqual(visibleLiveContentModes(access), ['user_audio', 'ai_dynamic'])
    assert.equal(canSelectLiveContentMode(access, 'ai_dynamic'), true)
    assert.equal(access.content_mode, selected)
  }
}
console.log('PASS: dynamic mode available for old/new access responses; loading and selected mode preserved')
