<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Hls from 'hls.js'
import { getRoom, getRoomEvents } from '../api'
import { session } from '../session'
import type { Room, RoomEvent } from '../types'

const route = useRoute()
const router = useRouter()
const roomId = Number(route.params.id)

const room = ref<Room | null>(null)
const events = ref<RoomEvent[]>([])
const loading = ref(true)
const error = ref('')
const streamState = ref<'connecting' | 'online' | 'offline'>('connecting')
const activeType = ref('all')
const liveVideo = ref<HTMLVideoElement | null>(null)
const videoState = ref<'idle' | 'connecting' | 'live' | 'error'>('idle')
const videoAspectRatio = ref(16 / 9)
const videoOrientation = ref<'portrait' | 'landscape'>('landscape')
let eventSource: EventSource | null = null
let hls: Hls | null = null
let videoRetryTimer: number | undefined


const eventTypes = [
  { key: 'all', label: '全部' },
  { key: 'chat', label: '弹幕' },
  { key: 'member', label: '进房' },
  { key: 'like', label: '点赞' },
  { key: 'follow', label: '关注' },
  { key: 'gift', label: '礼物' },
]

const visibleEvents = computed(() =>
  events.value.filter((event) => event.event_type !== 'room'),
)

const filteredEvents = computed(() => {
  if (activeType.value === 'all') return visibleEvents.value
  return visibleEvents.value.filter((event) => event.event_type === activeType.value)
})

const isAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const livePreviewUrl = computed(() => {
  if (!room.value) return ''
  if (room.value.source_url) return room.value.source_url
  if (!room.value.external_room_id) return ''
  return 'https://live.douyin.com/' + room.value.external_room_id + '?from=web_code_link'
})

function clearVideoRetry() {
  if (videoRetryTimer !== undefined) {
    window.clearTimeout(videoRetryTimer)
    videoRetryTimer = undefined
  }
}

function destroyLivePlayer() {
  clearVideoRetry()

  if (hls) {
    hls.destroy()
    hls = null
  }

  const video = liveVideo.value
  if (video) {
    video.pause()
    video.removeAttribute('src')
    video.load()
  }
}

function scheduleVideoRetry() {
  clearVideoRetry()
  videoRetryTimer = window.setTimeout(() => {
    startLivePlayer()
  }, 3000)
}

async function startLivePlayer() {
  if (!room.value || room.value.status !== 'live') {
    videoState.value = 'idle'
    return
  }

  await nextTick()
  const video = liveVideo.value
  if (!video) return

  clearVideoRetry()
  if (hls) {
    hls.destroy()
    hls = null
  }

  videoState.value = 'connecting'
  videoAspectRatio.value = 16 / 9
  videoOrientation.value = 'landscape'
  const source =
    '/api/v1/rooms/' + roomId + '/live/index.m3u8?t=' + Date.now()

  if (Hls.isSupported()) {
    const instance = new Hls({
      lowLatencyMode: true,
      liveSyncDurationCount: 2,
      liveMaxLatencyDurationCount: 6,
      maxBufferLength: 8,
      backBufferLength: 0,
    })

    hls = instance
    instance.attachMedia(video)

    instance.on(Hls.Events.MEDIA_ATTACHED, () => {
      instance.loadSource(source)
    })

    instance.on(Hls.Events.MANIFEST_PARSED, () => {
      video.play().catch(() => {})
    })

    instance.on(Hls.Events.ERROR, (_event, data) => {
      if (!data.fatal) return
      videoState.value = 'error'
      instance.destroy()
      if (hls === instance) hls = null
      scheduleVideoRetry()
    })
    return
  }

  if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = source
    video.play().catch(() => {})
    return
  }

  videoState.value = 'error'
}

function handleVideoMetadata() {
  const video = liveVideo.value
  if (!video || video.videoWidth <= 0 || video.videoHeight <= 0) {
    return
  }

  videoAspectRatio.value = video.videoWidth / video.videoHeight
  videoOrientation.value =
    video.videoHeight > video.videoWidth ? 'portrait' : 'landscape'
}
function handleVideoPlaying() {
  videoState.value = 'live'
}

function handleVideoError() {
  videoState.value = 'error'
  scheduleVideoRetry()
}
function roomTitle() {
  if (!room.value) return '直播间'
  return room.value.name || '直播间 ' + room.value.external_room_id
}

function tenantName() {
  if (!room.value) return ''
  return session.bootstrap?.tenants.find((tenant) => tenant.id === room.value?.tenant_id)?.name || ''
}

function statusText(status?: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

function eventLabel(type: string) {
  const labels: Record<string, string> = {
    chat: '弹幕',
    member: '进房',
    like: '点赞',
    follow: '关注',
    gift: '礼物',
    room: '房间',
  }
  return labels[type] || type
}

function eventClass(type: string) {
  return 'event-' + type
}

function formatTime(value: string) {
  const date = new Date(value)
  return date.toLocaleTimeString('zh-CN', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function pushEvent(event: RoomEvent) {
  if (events.value.some((item) => item.id === event.id)) return
  events.value.unshift(event)
  if (events.value.length > 500) {
    events.value = events.value.slice(0, 500)
  }
}

async function load() {
  if (!Number.isFinite(roomId) || roomId <= 0) {
    error.value = '无效的直播间'
    loading.value = false
    return
  }

  loading.value = true
  error.value = ''
  try {
    const [roomData, eventData] = await Promise.all([
      getRoom(roomId),
      getRoomEvents(roomId, 200),
    ])
    room.value = roomData
    events.value = [...eventData.items].sort((a, b) => b.id - a.id)
    connectStream()
    await startLivePlayer()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取直播间失败'
  } finally {
    loading.value = false
  }
}

function connectStream() {
  eventSource?.close()
  streamState.value = 'connecting'

  eventSource = new EventSource('/api/v1/rooms/' + roomId + '/stream', {
    withCredentials: true,
  })

  eventSource.onopen = () => {
    streamState.value = 'online'
  }

  eventSource.onmessage = (message) => {
    try {
      const event = JSON.parse(message.data) as RoomEvent
      pushEvent(event)
      if (room.value) {
        room.value.status = 'live'
        room.value.last_event_at = event.occurred_at
      }
      if (videoState.value === 'idle' || videoState.value === 'error') {
        startLivePlayer()
      }
    } catch {
      // Ignore malformed development events.
    }
  }

  eventSource.onerror = () => {
    streamState.value = 'offline'
  }
}

onMounted(load)
onBeforeUnmount(() => {
  eventSource?.close()
  destroyLivePlayer()
})
</script>

<template>
  <div class="room-detail-page">
    <button class="back-button" @click="router.push('/')">← 返回直播间列表</button>

    <div v-if="loading" class="detail-loading">正在读取直播间…</div>
    <div v-else-if="error && !room" class="inline-error">{{ error }}</div>

    <template v-else-if="room">
      <section class="detail-header">
        <div class="detail-title-row">
          <div class="platform-icon large">抖</div>
          <div>
            <div class="detail-title-line">
              <h2>{{ roomTitle() }}</h2>
              <span class="status-pill" :class="'status-' + room.status">
                <i></i>
                {{ statusText(room.status) }}
              </span>
            </div>
            <p>
              抖音 · 房间号 {{ room.external_room_id }}
              <span v-if="isAdmin && tenantName()"> · {{ tenantName() }}</span>
            </p>
          </div>
        </div>

        <div class="stream-indicator" :class="streamState">
          <i></i>
          {{
            streamState === 'online'
              ? '公屏实时连接'
              : streamState === 'connecting'
                ? '正在连接公屏'
                : '实时流重连中'
          }}
        </div>
      </section>

      <div v-if="error" class="inline-error">{{ error }}</div>

      <section class="detail-stat-grid">
        <article>
          <span>在线人数</span>
          <strong>{{ room.online_count.toLocaleString() }}</strong>
        </article>
        <article>
          <span>公屏事件</span>
          <strong>{{ events.length }}</strong>
        </article>
        <article>
          <span>AI 伴播</span>
          <strong class="muted-value">未启用</strong>
        </article>
        <article>
          <span>播放设备</span>
          <strong class="muted-value">未绑定</strong>
        </article>
      </section>

      <section class="detail-layout">
        <div class="public-screen-panel">
          <div class="panel-header">
            <div>
              <span class="section-kicker">REALTIME</span>
              <h3>实时公屏</h3>
            </div>
            <span class="event-count">{{ filteredEvents.length }} 条</span>
          </div>

          <div class="event-tabs">
            <button
              v-for="type in eventTypes"
              :key="type.key"
              :class="{ active: activeType === type.key }"
              @click="activeType = type.key"
            >
              {{ type.label }}
            </button>
          </div>

          <div class="event-list">
            <div v-if="!filteredEvents.length" class="screen-empty">
              <div class="screen-empty-icon">⌁</div>
              <strong>等待直播间事件</strong>
              <span>真实采集器接入后，弹幕、进房、点赞等会实时出现在这里。</span>
            </div>

            <article
              v-for="event in filteredEvents"
              :key="event.id"
              class="event-row"
            >
              <time>{{ formatTime(event.occurred_at) }}</time>
              <span class="event-type" :class="eventClass(event.event_type)">
                {{ eventLabel(event.event_type) }}
              </span>
              <div class="event-body event-inline">
                <span class="event-user">【{{ event.nickname || '直播间用户' }}】</span>
                <span class="event-action">{ {{ event.content || eventLabel(event.event_type) }} }</span>
              </div>
            </article>
          </div>
        </div>

        <aside class="room-side-panel">
          <section class="live-preview-card">
            <div class="live-preview-head">
              <div>
                <span class="section-kicker">LIVE PREVIEW</span>
                <h3>直播画面</h3>
              </div>
              <span class="preview-live-state" :class="{ live: room.status === 'live' }">
                <i></i>
                {{ statusText(room.status) }}
              </span>
            </div>

            <div
              class="live-preview-stage"
              :class="'is-' + videoOrientation"
              :style="{ aspectRatio: String(videoAspectRatio) }"
            >
              <video
                ref="liveVideo"
                class="live-preview-frame"
                autoplay
                playsinline
                controls
                @loadedmetadata="handleVideoMetadata"
                @playing="handleVideoPlaying"
                @error="handleVideoError"
              ></video>

              <div v-if="videoState !== 'live'" class="live-preview-placeholder">
                <strong>
                  {{
                    videoState === 'error'
                      ? '直播画面连接失败，正在重试'
                      : room.status === 'live'
                        ? '正在连接直播视频流'
                        : '直播画面暂不可用'
                  }}
                </strong>
                <span>
                  {{
                    room.status === 'live'
                      ? '正在将抖音直播流转换为浏览器可播放画面'
                      : '房间进入直播状态后会自动显示画面'
                  }}
                </span>
              </div>


            </div>

            <div class="live-preview-actions">
              <a
                class="primary-button"
                :href="livePreviewUrl"
                target="_blank"
                rel="noreferrer"
              >
                打开原直播间 ↗
              </a>
            </div>

            <p class="live-preview-note">
              画面来自实时直播流，按需启动低延迟转码；离开页面后会自动释放视频资源。
            </p>
          </section>
        </aside>
      </section>
    </template>
  </div>
</template>
