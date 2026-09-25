import fs from 'node:fs/promises';
import path from 'node:path';

const root = path.resolve(process.cwd());
const sourceDir = path.join(root, '测试素材', '母带时间轴测试');
const sourceTimelinePath = path.join(sourceDir, 'timeline_v3.json');
const outputWavPath = path.join(sourceDir, 'mainline_same_tts.wav');
const outputTimelinePath = path.join(sourceDir, 'mainline_same_tts_timeline.json');
const outputSrtPath = path.join(sourceDir, 'mainline_same_tts.srt');
const outputManifestPath = path.join(sourceDir, 'mainline_same_tts_manifest.json');
const segmentDir = path.join(sourceDir, 'same_tts_segments');

const base = process.env.XL_PROXY_URL || 'http://127.0.0.1:5176';
const provider = process.env.XL_TTS_PROVIDER || 'qwen_audio';
const model = process.env.XL_TTS_MODEL || 'qwen-audio-3.0-tts-plus';
const voiceID = process.env.XL_TTS_VOICE_ID || 'qwen-audio-3.0-tts-plus-yangduck-d4988b957aff42a89c44ec3ddb34d052';
const rate = Number(process.env.XL_TTS_RATE || '0.9');

if (!Number.isFinite(rate) || rate < 0.5 || rate > 2) {
  throw new Error('XL_TTS_RATE must be between 0.5 and 2.0');
}

function parseWav(buffer) {
  if (buffer.length < 12 || buffer.toString('ascii', 0, 4) !== 'RIFF' || buffer.toString('ascii', 8, 12) !== 'WAVE') {
    throw new Error('not a RIFF/WAVE file');
  }
  let fmt = null;
  let pcm = null;
  let offset = 12;
  while (offset + 8 <= buffer.length) {
    const id = buffer.toString('ascii', offset, offset + 4);
    const size = buffer.readUInt32LE(offset + 4);
    const start = offset + 8;
    let end = start + size;
    if (id === 'data' && end > buffer.length) {
      end = buffer.length;
    } else if (end > buffer.length) {
      break;
    }
    if (id === 'fmt ') {
      if (end - start < 16) throw new Error('invalid WAV fmt chunk');
      fmt = {
        audioFormat: buffer.readUInt16LE(start),
        channels: buffer.readUInt16LE(start + 2),
        sampleRate: buffer.readUInt32LE(start + 4),
        byteRate: buffer.readUInt32LE(start + 8),
        blockAlign: buffer.readUInt16LE(start + 12),
        bitsPerSample: buffer.readUInt16LE(start + 14),
      };
    } else if (id === 'data') {
      pcm = buffer.subarray(start, end);
      break;
    }
    offset = end + (size % 2);
  }
  if (!fmt || !pcm || !pcm.length) throw new Error('WAV missing fmt/data');
  if (fmt.audioFormat !== 1) throw new Error('only PCM WAV is supported, got format=' + fmt.audioFormat);
  return { fmt, pcm };
}

function makePcmWav(fmt, pcm) {
  const header = Buffer.alloc(44);
  header.write('RIFF', 0, 'ascii');
  header.writeUInt32LE(36 + pcm.length, 4);
  header.write('WAVE', 8, 'ascii');
  header.write('fmt ', 12, 'ascii');
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(fmt.audioFormat, 20);
  header.writeUInt16LE(fmt.channels, 22);
  header.writeUInt32LE(fmt.sampleRate, 24);
  header.writeUInt32LE(fmt.byteRate, 28);
  header.writeUInt16LE(fmt.blockAlign, 32);
  header.writeUInt16LE(fmt.bitsPerSample, 34);
  header.write('data', 36, 'ascii');
  header.writeUInt32LE(pcm.length, 40);
  return Buffer.concat([header, pcm]);
}

function sameFmt(a, b) {
  return a.audioFormat === b.audioFormat &&
    a.channels === b.channels &&
    a.sampleRate === b.sampleRate &&
    a.byteRate === b.byteRate &&
    a.blockAlign === b.blockAlign &&
    a.bitsPerSample === b.bitsPerSample;
}

function msFromBytes(bytes, byteRate) {
  return Math.round(bytes * 1000 / byteRate);
}

function srtTime(ms) {
  ms = Math.max(0, Math.round(ms));
  const h = Math.floor(ms / 3600000);
  ms -= h * 3600000;
  const m = Math.floor(ms / 60000);
  ms -= m * 60000;
  const s = Math.floor(ms / 1000);
  const milli = ms - s * 1000;
  return [h, m, s].map(v => String(v).padStart(2, '0')).join(':') + ',' + String(milli).padStart(3, '0');
}

async function synthesize(text) {
  const response = await fetch(base + '/management/internal/v1/dev/runtime/synthesize-text', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      tts_provider: provider,
      tts_model: model,
      voice_id: voiceID,
      tts_rate: rate,
      text,
    }),
  });
  const body = await response.json();
  if (!response.ok) {
    throw new Error('synthesize failed: ' + response.status + ' ' + JSON.stringify(body));
  }
  const audio = await fetch(body.audio_url);
  if (!audio.ok) {
    throw new Error('download audio failed: ' + audio.status);
  }
  return {
    metadata: body,
    wav: Buffer.from(await audio.arrayBuffer()),
  };
}

await fs.mkdir(segmentDir, { recursive: true });
const source = JSON.parse(await fs.readFile(sourceTimelinePath, 'utf8'));
const sentences = Array.isArray(source.sentences) ? source.sentences : [];
if (!sentences.length) throw new Error('source timeline has no sentences');

let referenceFmt = null;
const pcmParts = [];
const generatedSentences = [];
const safePoints = [];
const segmentManifest = [];
let cumulativeBytes = 0;

for (let i = 0; i < sentences.length; i++) {
  const sentence = sentences[i];
  const result = await synthesize(sentence.text);
  const parsed = parseWav(result.wav);
  if (!referenceFmt) {
    referenceFmt = parsed.fmt;
  } else if (!sameFmt(referenceFmt, parsed.fmt)) {
    throw new Error('WAV format changed at ' + sentence.id);
  }

  const segmentName = String(sentence.id || ('S' + String(i + 1).padStart(3, '0'))) + '.wav';
  await fs.writeFile(path.join(segmentDir, segmentName), makePcmWav(parsed.fmt, parsed.pcm));

  const startMS = msFromBytes(cumulativeBytes, referenceFmt.byteRate);
  cumulativeBytes += parsed.pcm.length;
  const endMS = msFromBytes(cumulativeBytes, referenceFmt.byteRate);
  const durationMS = endMS - startMS;
  pcmParts.push(parsed.pcm);

  const generated = {
    ...sentence,
    start_ms: startMS,
    end_ms: endMS,
    duration_ms: durationMS,
    boundary_prediction_ms: endMS,
    cut_after_ms: endMS,
    cut_refined_by_waveform: false,
    safe_score: 96,
    safe_grade: 'A',
    safe_reason: ['same_tts_segment_boundary'],
    play_start_ms: startMS,
    play_end_ms: endMS,
  };
  generatedSentences.push(generated);

  const nextText = i + 1 < sentences.length ? sentences[i + 1].text : '';
  safePoints.push({
    id: 'SP' + String(i + 1).padStart(3, '0'),
    cut_ms: endMS,
    score: 96,
    grade: 'A',
    kind: 'SENTENCE',
    sentence_id: generated.id,
    left_preview: generated.text,
    next_preview: nextText,
    topics: generated.topics || [],
  });

  segmentManifest.push({
    id: generated.id,
    text: generated.text,
    duration_ms: durationMS,
    audio_file: path.relative(root, path.join(segmentDir, segmentName)),
    tts_latency_ms: result.metadata.tts_latency_ms,
  });
  process.stdout.write('generated ' + generated.id + ' ' + durationMS + 'ms\n');
}

const combinedPCM = Buffer.concat(pcmParts);
const combinedWav = makePcmWav(referenceFmt, combinedPCM);
await fs.writeFile(outputWavPath, combinedWav);

const totalDurationMS = msFromBytes(combinedPCM.length, referenceFmt.byteRate);
const timeline = {
  version: 'same-tts-v1',
  source_audio: path.basename(outputWavPath),
  source_timeline: path.basename(sourceTimelinePath),
  synthesis_mode: 'sentence_by_sentence_same_runtime_tts',
  tts_provider: provider,
  tts_model: model,
  voice_id: voiceID,
  tts_rate: rate,
  audio_duration_ms: totalDurationMS,
  sentence_count: generatedSentences.length,
  sentences: generatedSentences,
  safe_points: safePoints,
};
await fs.writeFile(outputTimelinePath, JSON.stringify(timeline, null, 2), 'utf8');

const srt = generatedSentences.map((item, i) => [
  String(i + 1),
  srtTime(item.start_ms) + ' --> ' + srtTime(item.end_ms),
  item.text,
  '',
].join('\n')).join('\n');
await fs.writeFile(outputSrtPath, srt, 'utf8');

const manifest = {
  generated_at: new Date().toISOString(),
  source_timeline: path.relative(root, sourceTimelinePath),
  output_wav: path.relative(root, outputWavPath),
  output_timeline: path.relative(root, outputTimelinePath),
  output_srt: path.relative(root, outputSrtPath),
  tts_provider: provider,
  tts_model: model,
  voice_id: voiceID,
  tts_rate: rate,
  wav_format: referenceFmt,
  duration_ms: totalDurationMS,
  segment_count: segmentManifest.length,
  segments: segmentManifest,
};
await fs.writeFile(outputManifestPath, JSON.stringify(manifest, null, 2), 'utf8');

console.log(JSON.stringify({
  output_wav: outputWavPath,
  output_timeline: outputTimelinePath,
  output_srt: outputSrtPath,
  duration_ms: totalDurationMS,
  voice_id: voiceID,
  rate,
  segments: generatedSentences.length,
}));
