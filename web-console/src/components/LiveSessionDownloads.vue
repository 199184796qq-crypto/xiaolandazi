<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { downloadSessionFile, getSessionVoices, sessionDownloadURL, type SessionVoicePage } from '../liveSessionDownloads'

const props = defineProps<{ roomId: number }>()
const emit = defineEmits<{ close: [] }>()
const panel = ref<HTMLElement | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const data = ref<SessionVoicePage | null>(null)
const from = ref(''), to = ref(''), page = ref(1)
const loading = ref(false), transcriptBusy = ref(false), voiceBusy = ref<number | null>(null)
const error = ref(''), notice = ref('')
let requestController: AbortController | undefined
const downloadController = new AbortController()
let revision = 0
const pages = computed(() => Math.max(1, Math.ceil((data.value?.total || 0) / 50)))
const previousFocus = document.activeElement as HTMLElement | null
const previousOverflow = document.body.style.overflow
const labels: Record<string, string> = { mainline: '主线口播', interaction: '互动回答', bridge: '衔接语' }
const time = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
async function load(nextPage = 1) {
  if ((from.value && !Number.isFinite(Date.parse(from.value))) || (to.value && !Number.isFinite(Date.parse(to.value)))) {
    error.value = '请选择有效的时间'; return
  }
  if (from.value && to.value && new Date(from.value) >= new Date(to.value)) { error.value = '结束时间须晚于开始时间'; return }
  requestController?.abort(); requestController = new AbortController()
  const token = ++revision; loading.value = true; error.value = ''
  try {
    const result = await getSessionVoices(props.roomId, data.value?.session_id, from.value, to.value, nextPage, requestController.signal)
    if (token !== revision || requestController.signal.aborted) return
    data.value = result; page.value = result.page
  } catch (cause) {
    if (token !== revision || requestController.signal.aborted) return
    error.value = cause instanceof Error ? cause.message : '读取声音列表失败'
  } finally { if (token === revision) loading.value = false }
}
async function transcript() {
  if (!data.value || transcriptBusy.value) return
  transcriptBusy.value = true; error.value = ''; notice.value = ''
  try {
    await downloadSessionFile(sessionDownloadURL(props.roomId, data.value.session_id) + '&download=transcript', `本场直播文案_${data.value.session_id}.txt`, downloadController.signal)
    if (!downloadController.signal.aborted) notice.value = '本场全部纯文字文案已下载'
  } catch (cause) { if (!downloadController.signal.aborted) error.value = cause instanceof Error ? cause.message : '文案下载失败' }
  finally { transcriptBusy.value = false }
}
async function voice(id: number) {
  if (!data.value || voiceBusy.value !== null) return
  voiceBusy.value = id; error.value = ''; notice.value = ''
  try {
    await downloadSessionFile(`/api/v1/rooms/${props.roomId}/live-downloads/${id}/audio?session_id=${data.value.session_id}`, `直播声音_${data.value.session_id}_${id}.wav`, downloadController.signal)
    if (!downloadController.signal.aborted) notice.value = '声音文件已下载'
  } catch (cause) {
    if (!downloadController.signal.aborted) {
      error.value = cause instanceof Error ? cause.message : '声音下载失败'
      if ((cause as { status?: number })?.status === 410) {
        const item = data.value?.items.find(item => item.id === id)
        if (item) item.available = false
      }
    }
  }
  finally { voiceBusy.value = null }
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { emit('close'); return }
  if (event.key !== 'Tab' || !panel.value) return
  const elements = [...panel.value.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]')]
  const first = elements[0], last = elements.at(-1)
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
onMounted(async () => { document.body.style.overflow = 'hidden'; await nextTick(); closeButton.value?.focus(); await load() })
onBeforeUnmount(() => { ++revision; requestController?.abort(); downloadController.abort(); document.body.style.overflow = previousOverflow; if (previousFocus?.isConnected) previousFocus.focus() })
</script>

<template>
  <div class="download-mask" @click.self="emit('close')" @keydown="keydown">
    <section ref="panel" class="download-panel" role="dialog" aria-modal="true" aria-labelledby="live-download-title">
      <header class="download-head"><div><h2 id="live-download-title">直播下载</h2><p v-if="data">本场开始于 {{ time(data.started_at) }}</p><p v-else>本场已播放的文案与声音</p></div><button ref="closeButton" class="close" aria-label="关闭直播下载" @click="emit('close')">×</button></header>
      <div class="download-body">
        <section class="transcript-card"><div><h3>本场直播文案</h3><p>下载已播放的主线、互动回答和衔接语，保存为纯文字 TXT。</p><small>下载整场内容，不受下方时间筛选和分页影响。</small></div><button class="primary" :disabled="!data || transcriptBusy" @click="transcript">{{ transcriptBusy ? '下载中…' : '下载全部文案' }}</button></section>
        <section class="voices-section"><div class="voices-heading"><h3>直播声音</h3><span v-if="data">{{ data.total }} 条</span></div>
          <form class="time-filter" @submit.prevent="load(1)"><label>开始时间<input v-model="from" type="datetime-local"></label><label>结束时间<input v-model="to" type="datetime-local"></label><button class="primary" :disabled="loading">{{ loading ? '查询中…' : '查询声音' }}</button><button type="button" :disabled="loading" @click="from = ''; to = ''; load(1)">本场全部</button></form>
          <p class="help">按声音开始播放的时间筛选；点击下载保存该条声音。声音文件到期后不可下载，文案仍保留。</p>
          <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
          <p v-if="loading" class="empty" role="status">正在读取声音列表…</p>
          <div v-else-if="data?.items.length" class="voice-list"><article v-for="item in data.items" :key="item.id" class="voice-item"><div class="voice-copy"><div class="meta"><span>{{ labels[item.kind] || '直播声音' }}</span><time>{{ time(item.played_at) }}</time><small v-if="item.duration_ms">{{ (item.duration_ms / 1000).toFixed(1) }} 秒</small></div><p>{{ item.text }}</p></div><button :disabled="!item.available || voiceBusy !== null" @click="voice(item.id)">{{ !item.available ? '音频已到期' : voiceBusy === item.id ? '下载中…' : '下载声音' }}</button></article></div>
          <p v-else-if="!error" class="empty">{{ from || to ? '这个时间段没有已播放的声音' : '本场还没有已播放的声音记录。功能上线前已结束的场次无法补齐。' }}</p>
          <nav v-if="pages > 1" aria-label="声音列表分页"><button :disabled="page <= 1 || loading" @click="load(page - 1)">上一页</button><span>{{ page }} / {{ pages }}</span><button :disabled="page >= pages || loading" @click="load(page + 1)">下一页</button></nav>
        </section>
      </div>
    </section>
  </div>
</template>

<style scoped>
.download-mask{position:fixed;inset:0;z-index:2100;background:#101c37a6;display:flex;align-items:center;justify-content:center;padding:24px}.download-panel{width:min(920px,100%);max-height:90vh;display:flex;flex-direction:column;background:#fff;border:1px solid #e1e7f4;border-radius:24px;box-shadow:0 24px 80px #07132950;color:#20314d;overflow:hidden}.download-head{display:flex;justify-content:space-between;gap:20px;padding:26px 30px;border-bottom:1px solid #e9edf6}.download-head h2{margin:0;font-size:24px}.download-head p{margin:6px 0 0;color:#8190a8;font-size:13px}.close{font-size:28px;padding:0!important;width:38px;height:38px;flex-shrink:0}.download-body{padding:24px 30px;overflow:auto}.transcript-card{background:#f3f5ff;border:1px solid #e1e5fe;border-radius:18px;padding:22px;display:flex;align-items:center;justify-content:space-between;gap:20px}.transcript-card h3,.voices-heading h3{margin:0;font-size:18px}.transcript-card p{margin:8px 0;color:#637591;font-size:14px}.transcript-card small,.help{color:#8794aa;font-size:12px}.voices-section{margin-top:28px}.voices-heading{display:flex;align-items:center;justify-content:space-between;margin-bottom:16px}.voices-heading span{color:#818da5;font-size:13px}.time-filter{display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap}.time-filter label{display:grid;gap:7px;color:#657591;font-size:13px;flex:1;min-width:190px}.time-filter input{width:100%;min-width:0;box-sizing:border-box;border:1px solid #dbe2ef;background:white;border-radius:10px;padding:10px;color:#20314d;font:inherit}.download-panel button{background:white;border:1px solid #dbe2ef;padding:10px 15px;border-radius:10px;color:#526888;font-size:13px;font-weight:600;cursor:pointer;white-space:nowrap}.download-panel .primary{background:#575de9;border-color:#575de9;color:white}.download-panel button:disabled{opacity:.5;cursor:default}.download-panel button:focus-visible,.time-filter input:focus-visible{outline:2px solid #7377f5;outline-offset:3px}.help{line-height:1.7;margin:12px 0 18px}.voice-list{display:grid;gap:10px}.voice-item{border:1px solid #e5eaf4;border-radius:14px;padding:18px;display:flex;align-items:center;gap:18px}.voice-copy{min-width:0;flex:1}.meta{display:flex;gap:12px;flex-wrap:wrap;font-size:12px;color:#8b98b0}.meta span{color:#6066d8;font-weight:600}.voice-copy p{margin:10px 0 0;line-height:1.65;font-size:14px;white-space:pre-wrap;overflow-wrap:anywhere}.empty{text-align:center;padding:36px 12px;background:#f8faff;border-radius:14px;color:#8895ac}.error,.notice{padding:12px;border-radius:10px;font-size:14px}.error{background:#fff0f0;color:#bf4343}.notice{background:#edf9f3;color:#267b5b}nav{display:flex;justify-content:flex-end;gap:12px;align-items:center;margin-top:16px;font-size:13px}@media(max-width:600px){.download-mask{padding:12px}.download-panel{border-radius:18px;max-height:94vh}.download-head{padding:18px}.download-body{padding:16px}.transcript-card{padding:16px;display:grid;gap:12px}.time-filter{gap:10px}.time-filter label{min-width:100%}.voice-item{display:grid;gap:12px;padding:14px}.voice-item button{justify-self:end}}
</style>
