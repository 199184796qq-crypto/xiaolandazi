const assert = require('node:assert/strict');

const coreURL = 'http://127.0.0.1:8081';
const audioURL = 'http://127.0.0.1:8082';
const coreToken = process.env.CORE_INTERNAL_TOKEN || 'local-core-dev-token';
const roomID = Date.now();
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

function subscribe(receiverID) {
  const controller = new AbortController();
  const waiters = [];
  const backlog = [];
  let connectedResolve;
  let connectedReject;
  const connected = new Promise((resolve, reject) => {
    connectedResolve = resolve;
    connectedReject = reject;
  });

  function emit(task) {
    const waiter = waiters.shift();
    if (waiter) waiter.resolve(task);
    else backlog.push(task);
  }

  (async () => {
    try {
      const response = await fetch(
        audioURL + '/v1/rooms/' + roomID + '/stream?receiver_id=' + encodeURIComponent(receiverID),
        { signal: controller.signal },
      );
      assert.equal(response.status, 200);
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
        let split;
        while ((split = buffer.indexOf('\n\n')) >= 0) {
          const block = buffer.slice(0, split);
          buffer = buffer.slice(split + 2);
          let event = 'message';
          let data = '';
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) event = line.slice(6).trim();
            if (line.startsWith('data:')) data += line.slice(5).trim();
          }
          if (event === 'connected') connectedResolve(JSON.parse(data));
          if (event === 'task') emit(JSON.parse(data));
        }
      }
    } catch (error) {
      if (!controller.signal.aborted) connectedReject(error);
    }
  })();

  function nextTask(timeoutMS = 30000) {
    if (backlog.length) return Promise.resolve(backlog.shift());
    return new Promise((resolve, reject) => {
      const waiter = { resolve, reject };
      waiters.push(waiter);
      const timer = setTimeout(() => {
        const index = waiters.indexOf(waiter);
        if (index >= 0) waiters.splice(index, 1);
        reject(new Error(receiverID + ' task timeout'));
      }, timeoutMS);
      waiter.resolve = value => {
        clearTimeout(timer);
        resolve(value);
      };
    });
  }

  return { connected, nextTask, close: () => controller.abort() };
}

async function coreProgram(action) {
  const response = await fetch(coreURL + '/internal/v1/dev/audio/program/' + action, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Core-Token': coreToken,
    },
    body: JSON.stringify({ room_id: roomID }),
  });
  const body = await response.json();
  if (!response.ok) throw new Error('core ' + action + ' HTTP ' + response.status + ': ' + JSON.stringify(body));
  return body;
}

async function roomSync() {
  const response = await fetch(audioURL + '/v1/rooms/' + roomID + '/sync', { cache: 'no-store' });
  assert.equal(response.status, 200);
  return response.json();
}

(async () => {
  const a = subscribe('continuous-a');
  await a.connected;

  const started = await coreProgram('start');
  assert.equal(started.running, true);
  assert.ok(started.task);
  const duration = started.task.duration_ms;
  assert.ok(duration > 0);

  const taskA1 = await a.nextTask(5000);
  assert.equal(taskA1.program_id, started.program_id);
  assert.equal(taskA1.sequence, 1);
  assert.equal(taskA1.slot, 'A');

  const lateDelay = Math.max(700, Math.min(2500, Math.floor(duration * 0.30)));
  await delay(lateDelay);

  const syncBeforeB = await roomSync();
  assert.equal(syncBeforeB.running, true);
  assert.equal(syncBeforeB.sequence, 1);
  assert.ok(syncBeforeB.task.start_ms >= Math.max(250, lateDelay - 450));

  const b = subscribe('continuous-b-late');
  await b.connected;
  const taskB1 = await b.nextTask(5000);
  assert.equal(taskB1.speech_task_id, taskA1.speech_task_id);
  assert.equal(taskB1.sequence, 1);
  assert.ok(taskB1.start_ms >= Math.max(250, lateDelay - 550));
  assert.ok(taskB1.start_ms < duration);

  const boundaryTimeout = Math.min(120000, Math.max(5000, duration + 5000));
  const [taskA2, taskB2] = await Promise.all([
    a.nextTask(boundaryTimeout),
    b.nextTask(boundaryTimeout),
  ]);
  assert.equal(taskA2.sequence, 2);
  assert.equal(taskB2.sequence, 2);
  assert.equal(taskA2.speech_task_id, taskB2.speech_task_id);
  assert.equal(taskA2.slot, 'B');
  assert.equal(taskB2.slot, 'B');

  await delay(Math.min(900, Math.max(300, Math.floor(duration * 0.12))));
  const sync2 = await roomSync();
  assert.equal(sync2.sequence, 2);
  assert.ok(sync2.task.start_ms > 0);

  await coreProgram('stop');
  a.close();
  b.close();

  console.log(
    'PASS continuous room audio: late B joined seq=1 at ' +
    taskB1.start_ms + 'ms; A/B both crossed to seq=2 slot=B; wav_duration=' + duration + 'ms',
  );
})().catch(async error => {
  try { await coreProgram('stop'); } catch {}
  console.error(error.stack || error);
  process.exit(1);
});
