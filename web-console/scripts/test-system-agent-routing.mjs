import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/systemAgentRouting.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
})
const routing = await import('data:text/javascript;charset=utf-8,' + encodeURIComponent(outputText))

const route = (domain, overrides = {}) =>
  routing.shouldRouteToSystemAgent(domain, {
    hasSystemTask: false,
    systemCapabilityIntent: false,
    clientBoundaryIntent: false,
    ...overrides,
  })

assert.equal(route('live-policy-admin'), false, 'policy workbench must own ordinary rule conversation')
assert.equal(
  route('live-policy-admin', { systemCapabilityIntent: true }),
  false,
  'generic capability matching must not steal L1/L2 rule editing',
)
assert.equal(
  route('live-policy-admin', { hasSystemTask: true }),
  true,
  'explicit cross-system task may leave policy workbench',
)
assert.equal(route('system'), true, 'system domain always uses system agent')
assert.equal(
  route('live-room', { systemCapabilityIntent: true }),
  true,
  'non-policy specialized domains preserve existing system-capability routing',
)
assert.equal(
  route('live-policy-admin', { clientBoundaryIntent: true }),
  true,
  'hard client boundary checks still take priority',
)

console.log('PASS system agent routing: live-policy-admin owns ordinary L1/L2 conversation')
