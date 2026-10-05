import assert from 'node:assert/strict';
import { readFileSync, statSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, relative, isAbsolute } from 'node:path';

const root = resolve(process.argv[2] || '');
assert(process.argv[2], 'absolute firmware pack directory required');
assert(isAbsolute(process.argv[2]));
const read = (file) => {
  const path = resolve(root, file), subpath = relative(root, path);
  assert(!subpath.startsWith('..') && !isAbsolute(subpath), 'manifest path escaped pack');
  return readFileSync(path);
};
const manifest = JSON.parse(read('firmware-manifest.json'));
assert.equal(manifest.chip, 'esp32s3');
assert.equal(manifest.wake_word, '小蓝小蓝');
assert.equal(manifest.assistant_voice, 'Cherry');
assert.equal(manifest.physical_device_verified, false);
assert.equal(manifest.device_beans_charged, 0);
assert.deepEqual(manifest.parts.map((p) => p.address), ['0x0', '0x8000', '0xd000', '0x20000', '0x800000']);
const hash = (data) => createHash('sha256').update(data).digest('hex');
for (const file of [...manifest.parts, manifest.merged]) {
  const data = read(file.file);
  assert.equal(data.length, file.bytes);
  assert.equal(hash(data), file.sha256);
}
const merged = read(manifest.merged.file);
for (const part of manifest.parts) {
  const offset = Number(part.address);
  assert.deepEqual(merged.subarray(offset, offset + part.bytes), read(part.file));
}
assert(merged.subarray(0x9000, 0xd000).every((b) => b === 0xff));
assert.equal(manifest.merged.overwrites_saved_wifi_and_settings, true);
const assets = read('parts/generated_assets.bin');
const count = assets.readUInt32LE(0), entries = new Map();
assert.equal(assets.readUInt32LE(8) + 12, assets.length);
assert(assets.length < 0x800000);
for (let i = 0; i < count; i++) {
  const table = 12 + i * 44, name = assets.subarray(table, table + 32).toString().split('\0')[0];
  const length = assets.readUInt32LE(table + 32), offset = 12 + 44 * count + assets.readUInt32LE(table + 36);
  assert.equal(assets.readUInt16LE(offset), 0x5a5a);
  entries.set(name, assets.subarray(offset + 2, offset + 2 + length));
}
const index = JSON.parse(entries.get('index.json'));
assert.equal(index.multinet_model.language, 'cn');
assert.deepEqual(index.multinet_model.commands, [{ command: 'xiao lan xiao lan', text: '小蓝小蓝', action: 'wake' }]);
const prompts = [...entries.keys()].filter((name) => name.startsWith('feedback_'));
assert.equal(prompts.length, 12);
for (const prompt of prompts) assert.equal(entries.get(prompt).subarray(0, 4).toString(), 'OggS');
assert(statSync(resolve(root, '烧录说明.md')).isFile());
console.log('firmware pack: PASS (6 SHA256, 5 regions, NVS notice, double wake, 12 Ogg assets)');
console.log(`merged SHA256: ${manifest.merged.sha256}`);
