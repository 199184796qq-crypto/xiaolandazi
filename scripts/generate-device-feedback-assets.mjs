// One-time, bounded TTS asset generation; no customer recordings or business writes.
// Requires explicit --generate; reruns reuse existing WAV files and do not re-bill them.
import { mkdir, readFile, writeFile, access } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { createHash } from 'node:crypto';

const out = resolve(process.argv[2] || '');
if (!process.argv[2] || !process.argv.includes('--generate')) throw new Error('Usage: node generate-device-feedback-assets.mjs ABS_OUTPUT --generate');
const key = process.env.DASHSCOPE_API_KEY;
if (!key) throw new Error('DASHSCOPE_API_KEY is not configured');
const prompts = {
  feedback_greeting_female: '靓女，有什么吩咐？',
  feedback_greeting_male: '帅哥，有什么吩咐？',
  feedback_greeting_child: '小伙伴，有什么吩咐？',
  feedback_greeting_neutral: '我在，有什么吩咐？',
  feedback_accepted_female: '靓女稍等，马上就干。',
  feedback_accepted_male: '帅哥稍等，马上就干。',
  feedback_accepted_child: '小伙伴稍等，马上就干。',
  feedback_accepted_neutral: '好的，稍等，马上就干。',
  feedback_completed: '已经调整好了。',
  feedback_failed: '这次没执行成功，请再试一次。',
  feedback_capture_completed: '已经拍好并上传了。',
  feedback_info: '结果已经显示在屏幕上了。',
};
await mkdir(out, { recursive: true });
const manifest = { model: 'qwen3-tts-flash', voice: 'Cherry', language: 'Chinese', prompts: [] };
for (const [name, text] of Object.entries(prompts)) {
  const file = join(out, name + '.wav');
  let bytes;
  try { await access(file); bytes = await readFile(file); }
  catch {
    const response = await fetch('https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation', {
      method: 'POST', headers: { Authorization: 'Bearer ' + key, 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: manifest.model, input: { text, voice: manifest.voice, language_type: 'Chinese' } }),
      signal: AbortSignal.timeout(60000),
    });
    if (!response.ok) throw new Error(`TTS HTTP ${response.status}; stopped at ${name}`);
    const result = await response.json();
    const rawURL = result.output?.audio?.url;
    if (!rawURL) throw new Error(`TTS returned no audio at ${name}; code=${String(result.code || 'unknown')}; message=${String(result.message || '').slice(0, 180)}`);
    const url = new URL(rawURL);
    if (!url.hostname.endsWith('.aliyuncs.com') || !['http:', 'https:'].includes(url.protocol)) throw new Error(`Unexpected TTS audio host at ${name}`);
    // DashScope may return an HTTP OSS URL; always download over TLS.
    url.protocol = 'https:';
    const audio = await fetch(url, { signal: AbortSignal.timeout(30000) });
    if (!audio.ok) throw new Error(`Audio download HTTP ${audio.status}; stopped at ${name}`);
    bytes = Buffer.from(await audio.arrayBuffer());
    if (bytes.length < 44 || bytes.length > 2 * 1024 * 1024 || bytes.subarray(0, 4).toString() !== 'RIFF' || bytes.subarray(8, 12).toString() !== 'WAVE') throw new Error(`Invalid WAV at ${name}`);
    await writeFile(file, bytes);
  }
  manifest.prompts.push({ name, text, file: name + '.wav', bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') });
  console.log(`${name}: ${bytes.length} bytes`);
}
await writeFile(join(out, 'manifest.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(`Prepared ${manifest.prompts.length} Cherry prompts; no business data changed.`);
