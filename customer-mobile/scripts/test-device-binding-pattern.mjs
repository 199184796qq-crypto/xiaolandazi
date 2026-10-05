import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { compile } from 'svelte/compiler';

const mobileSource = readFileSync(new URL('../src/routes/devices/+page.svelte', import.meta.url), 'utf8');
const mobileCompiled = compile(mobileSource, { generate: 'client' }).js.code;
const mobilePattern = mobileCompiled.match(/set_attribute\([^,]+, 'pattern', '([^']+)'\)/)?.[1];

const requireDesktop = createRequire(new URL('../../web-console/package.json', import.meta.url));
const { parse, compileTemplate } = requireDesktop('vue/compiler-sfc');
const desktopSource = readFileSync(new URL('../../web-console/src/views/DeviceBindingView.vue', import.meta.url), 'utf8');
const { descriptor, errors } = parse(desktopSource);
assert.equal(errors.length, 0);
const desktopCompiled = compileTemplate({ source: descriptor.template.content, filename: 'DeviceBindingView.vue', id: 'binding-pattern-regression' });
assert.equal(desktopCompiled.errors.length, 0);
const desktopPattern = desktopCompiled.code.match(/pattern:\s*"([^"]+)"/)?.[1];

// Check compiler output, not source text: Svelte's quoted {6} used to compile as 6.
for (const [platform, pattern] of [['mobile', mobilePattern], ['desktop', desktopPattern]]) {
  assert.equal(pattern, '[0-9]{6}', `${platform}: compiled HTML pattern must preserve the quantifier`);
  const htmlValidation = new RegExp(`^(?:${pattern})$`, 'v');
  for (const valid of ['123456', '000001', '012345', '999999']) assert.equal(htmlValidation.test(valid), true, `${platform}: ${valid}`);
  for (const invalid of ['', '16', '12345', '1234567', '12345a', '１２３４５６', '123 456']) assert.equal(htmlValidation.test(invalid), false, `${platform}: ${invalid}`);
}
for (const source of [mobileSource, desktopSource]) {
  assert.ok(source.includes('/^\\d{6}$/'), 'Submit validation must also require exactly six digits');
  assert.ok(source.includes('type="text"'), 'Leading zeroes must not be lost by numeric coercion');
}
console.log('Mobile + desktop compiled binding-code patterns: six digits, leading zeroes and invalid inputs passed');
