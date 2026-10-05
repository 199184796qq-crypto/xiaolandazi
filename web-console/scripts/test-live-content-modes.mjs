import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/liveContentModeOptions.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } })
const { visibleLiveContentModes, canSelectLiveContentMode } = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))
const basic = ['ai_pregenerated', 'user_audio']
const access = { content_mode: 'ai_pregenerated', available_modes: basic, dynamic_authorized: false, authorization_source: 'default' }

assert.deepEqual(visibleLiveContentModes(null), basic, 'advanced mode must not flash before authorization loads')
assert.equal(canSelectLiveContentMode(null, 'ai_pregenerated'), false, 'selection waits for server access')
assert.deepEqual(visibleLiveContentModes(access), basic)
for (const mode of basic) assert.equal(canSelectLiveContentMode(access, mode), true)
assert.equal(canSelectLiveContentMode(access, 'ai_dynamic'), false)
assert.deepEqual(visibleLiveContentModes({ ...access, available_modes: [...basic, 'ai_dynamic'] }), basic, 'listed mode alone cannot grant advanced access')
assert.deepEqual(visibleLiveContentModes({ ...access, dynamic_authorized: true }), basic, 'grant flag alone cannot advertise unavailable mode')
const authorized = { ...access, available_modes: [...basic, 'ai_dynamic'], dynamic_authorized: true, authorization_source: 'operator' }
assert.deepEqual(visibleLiveContentModes(authorized), [...basic, 'ai_dynamic'])
assert.equal(canSelectLiveContentMode(authorized, 'ai_dynamic'), true)
assert.equal(canSelectLiveContentMode({ ...authorized, dynamic_authorized: false }, 'ai_dynamic'), false, 'revocation removes dynamic selection immediately')
assert.deepEqual(visibleLiveContentModes({ ...authorized, authorization_source: 'membership' }), [...basic, 'ai_dynamic'], 'future membership uses server entitlement without frontend branching')
console.log('Content modes: two defaults, fail-closed loading, advanced grant/revoke and future membership passed')
