import { ref, computed, watch, onMounted, onUnmounted } from 'vue'

export interface Person {
  id: string
  name: string
  position: number
  attending: '' | 'yes' | 'no'
}

export interface Pick {
  personId: string
  gameName: string
  pickedAt: string
  skipped: boolean
}

export interface PendingPick {
  personId: string
  gameName: string
  setAt: string
}

export interface State {
  people: Person[]
  history: Pick[]
  pendingPick?: PendingPick
  nextSession?: string // ISO date string
}

// Fetcher is the network seam. The real adapter wraps fetch(); tests supply a
// fake returning canned responses. Most endpoints return State; POST
// /api/people returns the created Person, hence unknown.
export type Fetcher = (method: string, path: string, body?: unknown) => Promise<unknown>

export function createApiFetcher(baseUrl = ''): Fetcher {
  return async (method, path, body) => {
    const res = await fetch(`${baseUrl}${path}`, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : {},
      body: body ? JSON.stringify(body) : undefined,
    })
    const data = await res.json()
    if (!res.ok) throw new Error((data as { error?: string }).error ?? 'Request failed')
    return data
  }
}

export interface GameNightOptions {
  pollIntervalMs?: number
}

// useGameNight owns all coordination between the UI and the server: polling,
// the edit-vs-poll overlay, drag sync with rollback, and the busy/error
// plumbing around every action. `state` always mirrors the last server
// response — local editing never mutates it; the `pendingPick` computed
// applies the overlay instead.
export function useGameNight(fetcher: Fetcher, opts: GameNightOptions = {}) {
  const pollIntervalMs = opts.pollIntervalMs ?? 10_000

  const state = ref<State | null>(null)
  const loading = ref(false)
  const busy = ref(false)
  const error = ref('')
  const editing = ref(false) // true while the user is editing a pending pick
  const gameName = ref('')
  const dragging = ref(false)
  const dragList = ref<Person[]>([])
  const sessionDateInput = ref('')

  let pollTimer: ReturnType<typeof setInterval> | null = null

  // ── computed ──────────────────────────────────────────────────────────────

  const sortedPeople = computed<Person[]>(() => {
    if (!state.value) return []
    return [...state.value.people].sort((a, b) => a.position - b.position)
  })

  const currentPicker = computed<Person | null>(() => sortedPeople.value[0] ?? null)

  const queueLength = computed(() => sortedPeople.value.length)

  const recentHistory = computed<Pick[]>(() => {
    if (!state.value) return []
    return [...state.value.history].reverse().slice(0, 8)
  })

  const attendingCount = computed(
    () => sortedPeople.value.filter((p) => p.attending === 'yes').length,
  )

  // The pending pick as the UI should see it: hidden while the user is
  // editing, so a background poll can never snap the input away.
  const pendingPick = computed<PendingPick | undefined>(() =>
    editing.value ? undefined : (state.value?.pendingPick ?? undefined),
  )

  // Keep dragList in sync with server state, but not while a drag is in progress
  watch(
    sortedPeople,
    (val) => {
      if (!dragging.value) dragList.value = [...val]
    },
    { immediate: true },
  )

  // Keep sessionDateInput in sync with server state
  watch(
    () => state.value?.nextSession,
    (iso) => {
      if (iso) sessionDateInput.value = iso.slice(0, 10) // "YYYY-MM-DD"
    },
    { immediate: true },
  )

  // ── actions ───────────────────────────────────────────────────────────────

  // run wraps an action with the busy/error plumbing. Returns the new State
  // on success, null on failure.
  async function run(fn: () => Promise<unknown>): Promise<State | null> {
    busy.value = true
    try {
      const s = (await fn()) as State
      state.value = s
      return s
    } catch (e) {
      error.value = (e as Error).message
      return null
    } finally {
      busy.value = false
    }
  }

  async function refresh() {
    loading.value = true
    try {
      state.value = (await fetcher('GET', '/api/state')) as State
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function addPerson(name: string): Promise<boolean> {
    if (!name.trim()) return false
    busy.value = true
    try {
      // Returns the created Person, not State — refresh afterwards.
      await fetcher('POST', '/api/people', { name: name.trim() })
      await refresh()
      return true
    } catch (e) {
      error.value = (e as Error).message
      return false
    } finally {
      busy.value = false
    }
  }

  async function removePerson(id: string) {
    await run(() => fetcher('DELETE', `/api/people/${id}`))
  }

  async function submitPick() {
    const name = gameName.value.trim()
    const picker = currentPicker.value
    if (!name || !picker) return
    const s = await run(() => fetcher('POST', `/api/people/${picker.id}/pick`, { gameName: name }))
    if (s) {
      gameName.value = ''
      editing.value = false
    }
  }

  // Start editing the pending pick: pre-fill the input and hide the
  // confirmation card via the pendingPick overlay until re-submitted.
  function editPick() {
    gameName.value = state.value?.pendingPick?.gameName ?? ''
    editing.value = true
  }

  async function submitSkip() {
    const picker = currentPicker.value
    if (!picker) return
    await run(() => fetcher('POST', `/api/people/${picker.id}/skip`))
  }

  // Finalise the pending pick: record history + rotate queue
  async function confirmDone() {
    const picker = currentPicker.value
    if (!picker) return
    await run(() => fetcher('POST', `/api/people/${picker.id}/done`))
  }

  async function toggleAttendance(id: string) {
    await run(() => fetcher('POST', `/api/people/${id}/attend`))
  }

  async function resetData() {
    const s = await run(() => fetcher('POST', '/api/reset'))
    if (s) {
      editing.value = false
      gameName.value = ''
    }
  }

  async function updateSessionDate() {
    if (!sessionDateInput.value) return
    await run(() => fetcher('PUT', '/api/session', { date: sessionDateInput.value }))
  }

  async function onDragEnd() {
    dragging.value = false
    const s = await run(() =>
      fetcher('PUT', '/api/people/reorder', { ids: dragList.value.map((p) => p.id) }),
    )
    if (!s) {
      // Roll back to last known good order
      dragList.value = [...sortedPeople.value]
    }
  }

  // ── lifecycle ─────────────────────────────────────────────────────────────

  onMounted(() => {
    void refresh()
    // Poll so all devices stay in sync
    pollTimer = setInterval(() => void refresh(), pollIntervalMs)
  })

  onUnmounted(() => {
    if (pollTimer) clearInterval(pollTimer)
  })

  return {
    // state
    state,
    loading,
    busy,
    error,
    editing,
    gameName,
    dragging,
    dragList,
    sessionDateInput,
    // computed
    sortedPeople,
    currentPicker,
    queueLength,
    recentHistory,
    attendingCount,
    pendingPick,
    // actions
    refresh,
    addPerson,
    removePerson,
    submitPick,
    editPick,
    submitSkip,
    confirmDone,
    toggleAttendance,
    resetData,
    updateSessionDate,
    onDragEnd,
  }
}
