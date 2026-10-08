import type { AnchorStyleRollingFeedbackResponse, AnchorStyleSpeechScoreResponse } from './types'

export interface CycleCandidate {
  id: string; attempt: number; text: string; prompt: string
  raw_text?: string; output_text?: string; score_scope?: string; postprocess_ms?: number
  punctuation_edits?: { position: number; before: string; after: string; reason: string }[]
  score: AnchorStyleSpeechScoreResponse['score']; generation_ms: number
  event_ids: string[]; retry_feedback?: string; created_at: string
  cache_id?: string
  p0_report?: { passed: boolean; hits: import('./riskHighlight').RiskHit[] }
}
export interface CycleTask { selling_point: string; angle: string; key: string; target_audience?: string; pain_point?: string }
export interface CycleOpeningHook {
  id: string; type: string; template: string; filled_template: string
  hard_values: Record<string, string>; soft_values: Record<string, string>; source: string
  recent_count: number; check_numbers: boolean; check_forbidden: boolean; required_quantities?: string[]
}
export interface OpeningHookAsset { id: number; tenant_id: number; plan_id: number; library_md: string; source: string; created_at: string }
export interface OpeningHookSettings { asset: OpeningHookAsset | null; mother_md: string; mother_source: string }
export interface CycleConfig {
  workspace_id: string; region: string; agent_id: string; environment_id: string
  credential_profile_id: string; feedback_profile_id: string
  threshold: number; max_retries: number; max_chars: number; point_gap: number
  repair_punctuation?: boolean
  rolling?: boolean; body_segments?: number; combination_gap?: number; max_cycles?: number
  body_min?: number; body_max?: number
  tts?: CycleVoiceConfig
}
export interface CycleVoiceConfig { source: string; voice_id: string; profile_id?: number; model?: string; rate: number; buffer_seconds: number; cache_ttl_seconds?: number }
export interface CycleVoiceAsset { status: string; error?: string; job_id?: string; latency_ms: number; bundle: CycleVoiceBundle; cache_id?: string; cache_hit?: boolean; cache_expires_at?: string }
export interface CycleVoiceResponse { run_id: string; candidate_id: string; voice: CycleVoiceAsset | null; preview_complete?: boolean }
export interface CycleVoiceBundle {
  audio_url: string; audio_asset_id: number; audio_duration_ms: number; text: string; srt: string
  timeline: { segment_id: string; start_ms: number; end_ms: number; text: string; safe_cut: boolean }[]
  safe_points: { id: string; cut_ms: number; sentence_id: string; left_preview: string; next_preview: string }[]
  asset_manifest: Record<string, unknown>
}
export interface CycleSegment {
  phase: string; task: CycleTask; selected: CycleCandidate; attempts: CycleCandidate[]
  reason: string; summary: string; macro_report: AnchorStyleRollingFeedbackResponse['report']
  feedback?: AnchorStyleRollingFeedbackResponse
  cycle_number?: number; opening_version?: string
  opening_hook?: CycleOpeningHook
  voice?: CycleVoiceAsset
}
export interface CycleState {
  version: number; run_id: string; revision: number; plan_id: number; room_id: number
  phase: 'opening' | 'body' | 'closing' | 'ended'; status: string
  cursor: number; queue: CycleTask[]; config: CycleConfig; session_id: string
  task_index?: number; opening_count?: number; cycle_number?: number; body_count?: number
  body_target?: number; pending_price_hook?: { task: CycleTask; promise: string; due_index: number }
  pending_opening_hook?: CycleOpeningHook; recent_opening_hook_ids?: string[]
  opening_hook_library_asset_id?: number
  attempts: CycleCandidate[]; accepted: CycleSegment[]; recent_summaries: string[]
  style_window: string; pending_feedback: string; feedback_pending: boolean
  macro_report: AnchorStyleRollingFeedbackResponse['report']; busy: boolean
  uncertain: boolean; warnings: string[]; error?: string; created_at: string
  p0_max_rewrites?: number
  p0_skipped?: { attempts: CycleCandidate[] }[]
  p0_audit_pending?: CycleCandidate
}
export interface CycleResponse { run: CycleState | null; warning?: string }
export function normalizeCycleState(state: CycleState): CycleState {
  const accepted = Array.isArray(state.accepted) ? state.accepted : []
  const attempts = Array.isArray(state.attempts) ? state.attempts : []
  const queue = Array.isArray(state.queue) ? state.queue : []
  const recentSummaries = Array.isArray(state.recent_summaries) ? state.recent_summaries : []
  const warnings = Array.isArray(state.warnings) ? state.warnings : []
  if (accepted === state.accepted && attempts === state.attempts && queue === state.queue && recentSummaries === state.recent_summaries && warnings === state.warnings) return state
  return { ...state, accepted, attempts, queue, recent_summaries: recentSummaries, warnings }
}
export function cycleSpeech(state: CycleState | null): string {
  return state ? normalizeCycleState(state).accepted.map(segment => segment.selected.output_text || segment.selected.text).join('\n\n') : ''
}

// Display only committed server state. An in-flight request is not a completed
// segment, and the final closing must not make a single-round run look like round 2.
export function cycleProgress(state: CycleState) {
  state = normalizeCycleState(state)
  const closings = state.accepted.filter(segment => segment.phase === 'closing').length
  const round = state.cycle_number || Math.max(1, closings + (state.phase === 'ended' ? 0 : 1))
  const lastClosing = state.accepted.map(segment => segment.phase).lastIndexOf('closing')
  const lastOpening = state.accepted.map(segment => segment.phase).lastIndexOf('opening')
  const currentSegments = state.phase === 'opening' && lastClosing >= 0 ? [] : state.accepted.slice(Math.max(0, lastOpening))
  const task = ['content_blocked', 'compliance_blocked'].includes(state.status) ? state.error || '内容检查未通过，已暂停核对' : state.status === 'spacing_blocked' ? '等待可用卖点×角度：避重间隔不满足' : state.pending_price_hook ? '先兑现塑价钩子：' + state.pending_price_hook.promise : state.phase === 'closing' ? '本轮收口：重报核心信息并自然承接'
    : state.phase === 'ended' ? '本轮任务已完成' : state.queue[state.config.rolling ? state.task_index || 0 : state.cursor]?.key || '等待装载卖点×角度'
  const last = state.accepted.at(-1)
  const covered = new Set(state.accepted.filter(segment => state.queue.some(task => task.key === segment.task.key)).map(segment => segment.task.key)).size
  const done = currentSegments.length
  const bodyTotal = state.config.rolling ? state.body_target || state.config.body_segments || 0 : Math.max(0, state.queue.length - 1)
  const total = state.config.rolling ? bodyTotal + 2 : state.queue.length + 1
  const openingVersion = state.phase === 'opening' ? ((state.opening_count || 0) === 0 ? '完整版' : '接新版')
    : currentSegments.find(segment => segment.phase === 'opening')?.opening_version || (round === 1 ? '完整版' : '接新版')
  return {
    round, task, covered, total, done, openingVersion, percent: Math.min(100, Math.round(done / total * 100)),
    bodyDone: currentSegments.filter(segment => segment.phase === 'body').length,
    bodyTotal,
    openingDone: currentSegments.some(segment => segment.phase === 'opening'),
    closingDone: currentSegments.some(segment => segment.phase === 'closing'),
    nextAttempt: state.attempts.length + 1, maxAttempts: state.config.max_retries + 1,
    lastScore: last?.selected.score.overall_score ?? null,
    lastGenerationSeconds: (last?.selected.generation_ms || 0) / 1000,
    lastTaskGenerationSeconds: (last?.attempts?.reduce((total, candidate) => total + (candidate.generation_ms || 0), 0) || 0) / 1000,
  }
}
// No browser-owned cursor or attempts are restored. Redis is authoritative;
// stop requests take effect between completed attempts; an ended run cannot
// be resurrected by the response of an earlier request.
export async function runCycleLoop(options: {
  state: () => CycleState | null
  stopped: () => boolean
  advance: (run: CycleState) => Promise<CycleResponse>
  receive: (state: CycleState) => void
  beforeAdvance?: (state: CycleState) => Promise<void>
  afterReceive?: (state: CycleState) => Promise<void>
}): Promise<'completed' | 'paused'> {
  let slot = '', slotRequests = 0
  while (!options.stopped()) {
    const initial = options.state()
    if (!initial) return 'paused'
    let state = normalizeCycleState(initial)
    if (state.phase === 'ended') return 'completed'
    if (options.beforeAdvance) {
      await options.beforeAdvance(state)
      if (options.stopped()) return 'paused'
      const refreshed = options.state()
      if (!refreshed) return 'paused'
      state = normalizeCycleState(refreshed)
    }
    if (state.status === 'content_blocked') throw new Error(state.error || '塑价钩子没有兑现，已暂停')
    if (state.status === 'compliance_blocked') throw new Error(state.error || 'P0检查未通过，已暂停')
    if (state.status === 'spacing_blocked') throw new Error(state.error || '卖点池无法满足避重间隔，请扩充池或降低间隔')
    if (state.uncertain || state.busy) throw new Error(state.error || '本场请求未确认完成，请刷新进度核对，不能盲目重发')
    const currentSlot = `${state.cycle_number || 1}:${state.phase}:${state.cursor}:${state.pending_price_hook?.due_index || 0}`
    if (slot !== currentSlot) { slot = currentSlot; slotRequests = 0 }
    if (++slotRequests > state.config.max_retries + (state.p0_max_rewrites || 0) + 2) throw new Error('当前任务未推进，已达到请求保护上限')
    const result = await options.advance(state)
    const current = options.state()
    if (!current || current.run_id !== state.run_id) return 'paused'
    if (!result.run || result.run.run_id !== state.run_id) throw new Error('本场大循环已结束或被替换')
    const received = normalizeCycleState(result.run)
    if (received.revision <= state.revision) throw new Error('本轮没有推进，停止重复请求')
    options.receive(received)
    if (options.afterReceive) await options.afterReceive(received)
  }
  return 'paused'
}
