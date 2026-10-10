import type { LiveAgentPlanVersion, LiveRuntimeSnapshot, SpeechProgramSnapshot } from './types'

export type PlanVersionIdentity = Pick<LiveAgentPlanVersion, 'id' | 'plan_id' | 'version_no'>

export function latestSavedPlanVersion(planID: number, versions: (LiveAgentPlanVersion | null | undefined)[]) {
  return versions.filter((version): version is LiveAgentPlanVersion =>
    !!version && version.plan_id === planID && version.id > 0 && version.version_no > 0 &&
    ['published', 'superseded'].includes(version.lifecycle_status),
  ).sort((a, b) => b.version_no - a.version_no)[0] || null
}

export function livePlanVersionStatus(
  planID: number,
  saved: PlanVersionIdentity | null,
  runtime: LiveRuntimeSnapshot | null,
  program: SpeechProgramSnapshot | null,
) {
  const latest = saved?.plan_id === planID && saved.id > 0 && saved.version_no > 0 ? saved : null
  const active = runtime?.agent_mode === 'anchor' && runtime.agent_state !== 'stopped' &&
    !!runtime.session && ['running', 'paused'].includes(runtime.session.status)
  // Core's actual program must belong to this live session. A previous session's
  // program or a preview is never proof that the current scene loaded a version.
  const confirmed = active && program?.running && Number(program.version_id) > 0 && Number(program.version_no) > 0 &&
    program.task?.session_id === `live-runtime-${runtime?.session?.id}`
  const loaded = confirmed ? { id: program.version_id!, version_no: program.version_no! } : null
  const same = !!latest && !!loaded && latest.id === loaded.id && latest.version_no === loaded.version_no
  return {
    latest, loaded, active,
    state: loaded && latest ? (same ? 'same' : 'different') : active ? 'loading' : 'idle',
    hint: loaded && latest
      ? same ? '版本一致' : '有新版本，下次开播加载'
      : active ? '等待确认本场加载版本' : '下次开播加载最新保存版本',
  }
}
