// Freeze a frontend-only package. The main application must match the pinned live bundle.
import assert from 'node:assert/strict'
import { readFileSync, mkdirSync, copyFileSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { resolve, join } from 'node:path'
const id = process.argv[2]
assert.match(id || '', /^[a-zA-Z0-9][a-zA-Z0-9._-]+$/)
const root = resolve(import.meta.dirname, '..')
const dist = join(root, 'web-console/dist')
const base = join(root, 'artifacts/model-picker-deploy/base-index-B4uJztIB.js')
const html = readFileSync(join(dist, 'index.html'), 'utf8')
const main = html.match(/src="\/assets\/(index-[A-Za-z0-9_-]+\.js)"/)[1]
const normalize = text => text.replace(/([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)/g, '$1-HASH.$2')
assert.equal(normalize(readFileSync(base, 'utf8')), normalize(readFileSync(join(dist, 'assets', main), 'utf8')), 'Main app changed beyond asset references; do not deploy')
const stage = join(root, 'artifacts', id)
mkdirSync(stage)
mkdirSync(join(stage, 'assets'))
copyFileSync(join(dist, 'index.html'), join(stage, 'index.html'))
const refs = text => [...text.matchAll(/(?:assets\/|\.\/)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))/g)].map(m => m[1])
const active = new Set(refs(html)), pending = [...active]
while (pending.length) {
  const file = pending.pop()
  if (!file.endsWith('.js')) continue
  for (const next of refs(readFileSync(join(dist, 'assets', file), 'utf8'))) {
    if (!active.has(next)) { active.add(next); pending.push(next) }
  }
}
const files = [...active].sort()
for (const file of files) {
  copyFileSync(join(dist, 'assets', file), join(stage, 'assets', file))
}
const sources = ['web-console/src/views/SpeechModelsView.vue', 'web-console/src/components/ModelPickerDialog.vue', 'web-console/scripts/model-picker-fixture.mjs', 'web-console/scripts/test-model-picker.mjs']
const digest = file => createHash('sha256').update(readFileSync(file)).digest('hex')
writeFileSync(join(stage, 'manifest.json'), JSON.stringify({ id, main, files, scope: 'model-picker-desktop-only', source_files: sources.map(path => ({ path, sha256: digest(join(root, path)) })) }, null, 2))
const archive = join(root, 'artifacts', id + '.tar.gz')
execFileSync('tar', ['-czf', archive, '-C', stage, 'index.html', 'manifest.json', 'assets'])
console.log(JSON.stringify({ id, archive, archive_sha256: digest(archive), main, files: files.length, normalized_main_matches_live: true }))
