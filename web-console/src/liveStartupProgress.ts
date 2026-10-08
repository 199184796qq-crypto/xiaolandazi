import type { LiveRuntimeSnapshot } from './types'

export const startupMilestones = {
  opening_audio: { percent: 4, title: '正在开启声音通道…', detail: '确认声音可以正常播放，请稍候' },
  connecting: { percent: 8, title: '正在连接直播服务…', detail: '连接完成后会开始准备主播声音' },
  waiting_startup: { percent: 10, title: '直播服务已连接，正在接收准备状态…', detail: '正在等待第一段口播内容' },
  reading_plan: { percent: 12, title: '正在读取你的直播方案…', detail: '准备商品信息、讲解安排和主播声音' },
  connecting_service: { percent: 25, title: '正在连接声音准备服务…', detail: '连接完成后开始准备第一段口播内容' },
  preparing_content: { percent: 40, title: '正在准备第一段口播内容…', detail: '根据你的商品和直播方案组织讲解' },
  checking_content: { percent: 55, title: '已收到口播内容，正在核对…', detail: '核对商品信息和表达是否适合本场直播' },
  generating_voice: { percent: 65, title: '正在生成主播声音…', detail: '把口播内容转换为你选择的主播声音' },
  voice_ready: { percent: 80, title: '首段声音已准备好…', detail: '马上发送到直播间，不再等待后续段落' },
  sending_audio: { percent: 90, title: '正在发送声音到直播间…', detail: '声音接收后会交给已连接的小蓝盒子播放' },
  waiting_playback: { percent: 96, title: '声音已发送，等待开始播放…', detail: '第一段声音开始播放后，这个提示会自动收起' },
} as const
export type StartupStage = keyof typeof startupMilestones
export function advanceStartupStage(current: StartupStage, snapshot: LiveRuntimeSnapshot): StartupStage {
  const progress = snapshot.startup
  if (!progress || progress.session_id !== snapshot.session?.id || snapshot.agent_state !== 'working') return current
  if (!Object.hasOwn(startupMilestones, progress.stage)) return current
  const stage = progress.stage as StartupStage
  if (progress.percent !== startupMilestones[stage].percent || progress.percent < startupMilestones[current].percent) return current
  return stage
}
