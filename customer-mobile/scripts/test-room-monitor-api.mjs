import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source = readFileSync(new URL('../src/lib/api.ts', import.meta.url), 'utf8');
const javascript = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { setRoomMonitor } = await import('data:text/javascript;base64,' + Buffer.from(javascript).toString('base64'));
const original = globalThis.fetch;
const calls = [];
let response;
globalThis.fetch = async (url, init) => { calls.push({ url, ...init }); return response; };
try {
  for (const enabled of [true, false]) {
    response = Response.json({ id: 15, monitor_enabled: enabled, status: enabled ? 'connecting' : 'stopped' });
    assert.equal((await setRoomMonitor(15, enabled)).monitor_enabled, enabled);
    const call = calls.at(-1);
    assert.equal(call.url, '/api/v1/rooms/15/monitor');
    assert.equal(call.method, 'PATCH');
    assert.equal(call.credentials, 'include');
    assert.deepEqual(JSON.parse(call.body), { enabled });
  }
  for (const body of [{ id: 16, monitor_enabled: true }, { id: 15 }, { id: 15, monitor_enabled: false }]) {
    response = Response.json(body);
    await assert.rejects(setRoomMonitor(15, true), /操作结果未确认/);
  }
  response = Response.json({ error: '当前账号没有直播间连接控制权限' }, { status: 403 });
  await assert.rejects(setRoomMonitor(15, true), /当前账号没有直播间连接控制权限/);
  assert.ok(calls.every(call => !call.url.includes('/runtime/')));
  console.log('Room collection controls: authenticated PATCH true/false, response identity and state, API rejection, no broadcast-runtime calls passed');
} finally {
  globalThis.fetch = original;
}
