import { reactive } from 'vue'
import { getCoreRuntimeStatus } from './api'

export type CoreRuntimePhase = 'checking' | 'online' | 'offline' | 'recovering'

export const coreRuntime = reactive({
  phase: 'checking' as CoreRuntimePhase,
  available: false,
  bootId: '',
  activeRooms: 0,
  checkedAt: '',
  lastTransitionAt: '',
  recoverySerial: 0,
})

let pollTimer: number | undefined
let recoveryTimer: number | undefined
let watchKey = ''
let pollBusy = false
let watchGeneration = 0
let unavailableSince: number | undefined
const transientFailureGraceMS = 6000

function observeUnavailable() {
  // A failed status request is not a command to stop a healthy PCM listener.
  // Confirm sustained loss before changing the phase that tears it down.
  unavailableSince ??= Date.now()
  if ((coreRuntime.phase === 'online' || coreRuntime.phase === 'recovering') &&
      Date.now() - unavailableSince < transientFailureGraceMS) return
  clearRecoveryTimer()
  coreRuntime.available = false
  coreRuntime.phase = 'offline'
}

function clearRecoveryTimer() {
  if (recoveryTimer !== undefined) {
    window.clearTimeout(recoveryTimer)
    recoveryTimer = undefined
  }
}

function scheduleRecoveryComplete(bootId: string) {
  clearRecoveryTimer()
  recoveryTimer = window.setTimeout(() => {
    recoveryTimer = undefined
    if (coreRuntime.available && coreRuntime.bootId === bootId) {
      coreRuntime.phase = 'online'
    }
  }, 3200)
}

export async function refreshCoreRuntimeStatus() {
  if (pollBusy) return
  pollBusy = true
  const generation = watchGeneration
  try {
    const next = await getCoreRuntimeStatus()
    if (generation !== watchGeneration) return
    if (!next.available) {
      observeUnavailable()
      return
    }
    unavailableSince = undefined
    const previousPhase = coreRuntime.phase
    const previousBootId = coreRuntime.bootId
    const bootChanged =
      Boolean(previousBootId) &&
      Boolean(next.core_boot_id) &&
      previousBootId !== next.core_boot_id

    coreRuntime.available = next.available
    coreRuntime.bootId = next.core_boot_id || ''
    coreRuntime.activeRooms = next.active_rooms || 0
    coreRuntime.checkedAt = next.checked_at || ''
    coreRuntime.lastTransitionAt = next.last_transition_at || ''

    if (previousPhase === 'offline' || bootChanged) {
      coreRuntime.phase = 'recovering'
      coreRuntime.recoverySerial += 1
      scheduleRecoveryComplete(coreRuntime.bootId)
      return
    }

    if (previousPhase === 'recovering') {
      return
    }
    coreRuntime.phase = 'online'
  } catch {
    if (generation === watchGeneration) observeUnavailable()
  } finally {
    if (generation === watchGeneration) pollBusy = false
  }
}

export function startCoreRuntimeWatch(key: string) {
  const normalized = String(key || '').trim()
  if (!normalized) {
    stopCoreRuntimeWatch()
    return
  }
  if (pollTimer !== undefined && watchKey === normalized) return
  stopCoreRuntimeWatch()
  watchKey = normalized
  coreRuntime.phase = 'checking'
  void refreshCoreRuntimeStatus()
  pollTimer = window.setInterval(() => {
    void refreshCoreRuntimeStatus()
  }, 2000)
}

export function stopCoreRuntimeWatch() {
  watchGeneration += 1
  unavailableSince = undefined
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
  clearRecoveryTimer()
  watchKey = ''
  pollBusy = false
  coreRuntime.phase = 'checking'
  coreRuntime.available = false
  coreRuntime.bootId = ''
  coreRuntime.activeRooms = 0
  coreRuntime.checkedAt = ''
  coreRuntime.lastTransitionAt = ''
}
