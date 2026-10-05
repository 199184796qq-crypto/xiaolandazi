import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
const require = createRequire(new URL('../package.json', import.meta.url));
const ts = require('typescript');
const { ref, computed } = require('vue');
const desktop = readFileSync(new URL('../src/views/DeviceBindingView.vue', import.meta.url), 'utf8');
const mobile = readFileSync(new URL('../../customer-mobile/src/routes/devices/+page.svelte', import.meta.url), 'utf8');
for (const source of [desktop, mobile]) {
  for (const value of ['auto', 'female', 'male', 'child', 'neutral']) assert(source.includes(`value="${value}"`));
  assert(source.includes('小伙伴') && source.includes('帅哥') && source.includes('芊悦'));
  assert(source.includes('不识别个人身份'));
  assert(source.includes('saveDeviceAddressing') && source.includes('getDeviceAddressing'));
}
assert(mobile.includes('selected?.id === d.id'), 'stale requests cannot overwrite another selected device');
assert(mobile.includes('!addressingLoaded'), 'failed initial load must not silently save a default');
assert(desktop.includes('isCustomer'), 'only customer device owners see preferences');
assert(mobile.includes('class="device-card" disabled={saving}'), 'cannot switch device while saving');

// Execute the real desktop handlers without rendering: every mutation must hold
// the same lock until its own pending request finishes, including other cards.
const script = desktop.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '');
const compiled = ts.transpileModule(script, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
const mutationAPIs = ['saveDeviceAddressing', 'renameLiveDevice', 'unbindLiveDevice', 'bindLiveDevice', 'claimLiveDevice'];
for (const [start, api] of [['saveAddressing', 'saveDeviceAddressing'], ['rename', 'renameLiveDevice'], ['unbind', 'unbindLiveDevice'], ['saveBinding', 'bindLiveDevice'], ['addDevice', 'claimLiveDevice']]) {
  let resolvePending;
  const pending = new Promise((resolve) => { resolvePending = resolve; });
  const calls = [];
  const a = { id: 1, tenant_id: 1, device_name: 'A' }, b = { id: 2, tenant_id: 1, device_name: 'B' };
  const deps = {
    ref, computed, watch() {}, onMounted() {}, onBeforeUnmount() {},
    session: { bootstrap: { actor: { role: 'customer' } } }, window: { confirm: () => true },
    getLiveDevices: async () => [a, b], getRooms: async () => ({ items: [] }),
    getDeviceAddressing: async () => ({ mode: 'auto' }), getDefaultDeviceName: async () => ({ device_name: 'C' }),
    ...Object.fromEntries(mutationAPIs.map((name) => [name, async () => { calls.push(name); if (name === api) await pending; return { mode: 'female' }; }])),
  };
  const state = new Function(...Object.keys(deps), compiled + '\nreturn { saveAddressing, rename, unbind, saveBinding, addDevice, openAdd, mutationBusy, savingDeviceId, addressingModes, selectedRoomByDevice, bindingCode, deviceName, devices };')(...Object.values(deps));
  state.devices.value = [a, b]; state.addressingModes.value = { 1: 'female', 2: 'male' };
  state.selectedRoomByDevice.value = { 1: 10, 2: 20 }; state.bindingCode.value = '123456'; state.deviceName.value = 'C';
  const started = state[start](a);
  assert.equal(state.mutationBusy.value, true, `${start} acquired the global lock`);
  for (const handler of ['saveAddressing', 'rename', 'unbind', 'saveBinding', 'addDevice', 'openAdd']) await state[handler](b);
  assert.deepEqual(calls, [api], `${start}: no other mutation can start or release its lock`);
  assert.equal(state.mutationBusy.value, true);
  resolvePending(); await started;
  assert.equal(state.mutationBusy.value, false, `${start} released the lock after completion`);
}
console.log('device addressing desktop/mobile source checks: PASS');
console.log('desktop cross-device mutation lock runtime regression: PASS (5 pending operations)');
