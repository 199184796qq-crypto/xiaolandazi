const base = process.env.ROOM_BRAIN_BASE || 'http://127.0.0.1:5176/core';
const room = Number(process.env.ROOM_BRAIN_ROOM || '1001');

async function scenario(name) {
  const response = await fetch(base + '/internal/v1/dev/rooms/' + room + '/brain/scenario', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({Scenario: name}),
  });
  const body = await response.json();
  if (!response.ok) throw new Error(name + ' HTTP ' + response.status + ': ' + JSON.stringify(body));
  return body;
}

function assert(condition, message) {
  if (!condition) throw new Error(message);
}

const cold = await scenario('cold');
assert(cold.Intelligence.Heat === 'COLD', 'cold heat=' + cold.Intelligence.Heat);
assert(cold.Intelligence.PreferOneToOneQNA === true, 'cold should prefer one-to-one');

const warm = await scenario('warm');
assert(['WARM', 'BUSY'].includes(warm.Intelligence.Heat), 'warm heat=' + warm.Intelligence.Heat);
assert(warm.Intelligence.QuestionCount30s >= 1, 'warm should detect question');

const hot = await scenario('hot');
assert(hot.Intelligence.Heat === 'OVERHEATED', 'hot heat=' + hot.Intelligence.Heat);
assert(hot.Intelligence.PreferAggregateQNA === true, 'hot should aggregate questions');
assert(hot.Director.Progress === 'HOLD_FOR_QNA', 'hot director progress=' + hot.Director.Progress);
const topics = hot.Intelligence.TopTopics.map((x) => x.Topic);
assert(topics.includes('PRICE'), 'hot missing PRICE bucket');
assert(topics.includes('SHIPPING'), 'hot missing SHIPPING bucket');

const complaint = await scenario('complaint');
assert(complaint.Intelligence.NegativeFeedback30s > 0, 'complaint negative not detected');
assert(complaint.Director.Humanization.Enabled === false, 'complaint must suppress humanization');
assert(complaint.Director.Atmosphere === 'CALM_AND_FOCUS', 'complaint atmosphere=' + complaint.Director.Atmosphere);

console.log(
  'PASS room brain scenarios: ' +
  'cold=' + cold.Intelligence.Heat +
  ', warm=' + warm.Intelligence.Heat +
  ', hot=' + hot.Intelligence.Heat +
  ', hotTopics=' + topics.slice(0, 3).join('/') +
  ', complaintHumanization=' + complaint.Director.Humanization.Enabled
);
