import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, type App } from 'vue'
import {
  useGameNight,
  type Fetcher,
  type State,
  type Person,
} from '../useGameNight'

// ── harness ───────────────────────────────────────────────────────────────────

// withSetup mounts a throwaway component so the composable gets a real setup
// context (onMounted/onUnmounted fire). Returns the composable's API plus the
// app handle for unmounting.
function withSetup<T>(fn: () => T): { result: T; app: App } {
  let result: T
  const app = createApp({
    setup() {
      result = fn()
      return () => null
    },
  })
  app.mount(document.createElement('div'))
  return { result: result!, app }
}

interface Call {
  method: string
  path: string
  body?: unknown
}

// fakeFetcher is the test adapter for the network seam: records calls and
// returns the programmed response (or throws the programmed error).
function fakeFetcher(initial: State) {
  const calls: Call[] = []
  let response: unknown = initial
  let failWith: string | null = null
  const fetcher: Fetcher = async (method, path, body) => {
    calls.push({ method, path, body })
    if (failWith) throw new Error(failWith)
    return response
  }
  return {
    fetcher,
    calls,
    lastCall(): Call {
      return calls[calls.length - 1]!
    },
    respondWith(r: unknown) {
      response = r
    },
    failWith(msg: string | null) {
      failWith = msg
    },
  }
}

function person(id: string, position: number, attending: Person['attending'] = ''): Person {
  return { id, name: id, position, attending }
}

function baseState(overrides: Partial<State> = {}): State {
  return {
    people: [person('alice', 0), person('bob', 1), person('charlie', 2)],
    history: [],
    suggestions: [],
    ...overrides,
  }
}

// flush pending microtasks (fetcher promises resolve between ticks)
async function flush() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

// ── loading & polling ─────────────────────────────────────────────────────────

describe('loading and polling', () => {
  it('loads state on mount', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    expect(api.calls).toEqual([{ method: 'GET', path: '/api/state', body: undefined }])
    expect(result.sortedPeople.value.map((p) => p.id)).toEqual(['alice', 'bob', 'charlie'])
    expect(result.currentPicker.value?.id).toBe('alice')
    app.unmount()
  })

  it('polls on the configured interval', async () => {
    const api = fakeFetcher(baseState())
    const { app } = withSetup(() => useGameNight(api.fetcher, { pollIntervalMs: 10_000 }))
    await flush()
    expect(api.calls.length).toBe(1)

    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.calls.length).toBe(2)

    await vi.advanceTimersByTimeAsync(20_000)
    expect(api.calls.length).toBe(4)
    app.unmount()
  })

  it('stops polling on unmount', async () => {
    const api = fakeFetcher(baseState())
    const { app } = withSetup(() => useGameNight(api.fetcher, { pollIntervalMs: 10_000 }))
    await flush()
    app.unmount()

    await vi.advanceTimersByTimeAsync(30_000)
    expect(api.calls.length).toBe(1)
  })

  it('surfaces poll errors via error ref', async () => {
    const api = fakeFetcher(baseState())
    api.failWith('server gone')
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    expect(result.error.value).toBe('server gone')
    expect(result.loading.value).toBe(false)
    app.unmount()
  })
})

// ── pending pick & the editing overlay ───────────────────────────────────────

describe('pending pick editing overlay', () => {
  const pending = { personId: 'alice', gameName: 'Catan', setAt: '2026-06-11T00:00:00Z' }

  it('exposes the server pending pick when not editing', async () => {
    const api = fakeFetcher(baseState({ pendingPick: pending }))
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    expect(result.pendingPick.value?.gameName).toBe('Catan')
    app.unmount()
  })

  it('editPick prefills gameName and hides the pending pick', async () => {
    const api = fakeFetcher(baseState({ pendingPick: pending }))
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.editPick()
    expect(result.gameName.value).toBe('Catan')
    expect(result.pendingPick.value).toBeUndefined()
    // server state itself is untouched
    expect(result.state.value?.pendingPick?.gameName).toBe('Catan')
    app.unmount()
  })

  it('a poll while editing refreshes state but keeps the pick hidden', async () => {
    const api = fakeFetcher(baseState({ pendingPick: pending }))
    const { result, app } = withSetup(() => useGameNight(api.fetcher, { pollIntervalMs: 10_000 }))
    await flush()

    result.editPick()
    result.gameName.value = 'Wingspan' // user is mid-edit

    // another device renames a player; poll picks it up
    const fresh = baseState({ pendingPick: pending })
    fresh.people[1] = { ...person('bob', 1), name: 'robert' }
    api.respondWith(fresh)
    await vi.advanceTimersByTimeAsync(10_000)

    // fresh server data arrived…
    expect(result.state.value?.people[1]?.name).toBe('robert')
    // …but the overlay still hides the pending pick and the edit buffer survives
    expect(result.pendingPick.value).toBeUndefined()
    expect(result.gameName.value).toBe('Wingspan')
    app.unmount()
  })

  it('submitPick sends the new name, clears editing, re-exposes the pick', async () => {
    const api = fakeFetcher(baseState({ pendingPick: pending }))
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.editPick()
    result.gameName.value = 'Wingspan'
    api.respondWith(
      baseState({ pendingPick: { ...pending, gameName: 'Wingspan' } }),
    )
    await result.submitPick()

    expect(api.lastCall()).toEqual({
      method: 'POST',
      path: '/api/people/alice/pick',
      body: { gameName: 'Wingspan' },
    })
    expect(result.editing.value).toBe(false)
    expect(result.gameName.value).toBe('')
    expect(result.pendingPick.value?.gameName).toBe('Wingspan')
    app.unmount()
  })

  it('a failed submit keeps the edit buffer so nothing is lost', async () => {
    const api = fakeFetcher(baseState({ pendingPick: pending }))
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.editPick()
    result.gameName.value = 'Wingspan'
    api.failWith('boom')
    await result.submitPick()

    expect(result.error.value).toBe('boom')
    expect(result.gameName.value).toBe('Wingspan')
    expect(result.editing.value).toBe(true)
    app.unmount()
  })
})

// ── actions ───────────────────────────────────────────────────────────────────

describe('actions', () => {
  it('addPerson posts then refreshes, reporting success', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    const ok = await result.addPerson('  dana  ')
    expect(ok).toBe(true)
    expect(api.calls.slice(1)).toEqual([
      { method: 'POST', path: '/api/people', body: { name: 'dana' } },
      { method: 'GET', path: '/api/state', body: undefined },
    ])
    app.unmount()
  })

  it('addPerson with a blank name is a no-op', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    expect(await result.addPerson('   ')).toBe(false)
    expect(api.calls.length).toBe(1)
    app.unmount()
  })

  it('toggleAttendance posts to the attend endpoint and applies the result', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    const next = baseState()
    next.people[1] = person('bob', 1, 'yes')
    api.respondWith(next)
    await result.toggleAttendance('bob')

    expect(api.lastCall().path).toBe('/api/people/bob/attend')
    expect(result.attendingCount.value).toBe(1)
    app.unmount()
  })

  it('attendingCount counts only definite yes', async () => {
    const api = fakeFetcher(
      baseState({
        people: [person('alice', 0, 'yes'), person('bob', 1, 'no'), person('charlie', 2, '')],
      }),
    )
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    expect(result.attendingCount.value).toBe(1)
    app.unmount()
  })

  it('an action failure sets error and clears busy', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    api.failWith('nope')
    await result.confirmDone()

    expect(result.error.value).toBe('nope')
    expect(result.busy.value).toBe(false)
    app.unmount()
  })

  it('resetData clears the edit buffer on success', async () => {
    const api = fakeFetcher(
      baseState({ pendingPick: { personId: 'alice', gameName: 'Catan', setAt: '' } }),
    )
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.editPick()
    api.respondWith(baseState())
    await result.resetData()

    expect(result.editing.value).toBe(false)
    expect(result.gameName.value).toBe('')
    app.unmount()
  })
})

// ── drag reorder ──────────────────────────────────────────────────────────────

describe('drag reorder', () => {
  it('sends the dragged order and applies the server result', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.dragging.value = true
    result.dragList.value = [person('charlie', 2), person('alice', 0), person('bob', 1)]
    api.respondWith(
      baseState({
        people: [person('charlie', 0), person('alice', 1), person('bob', 2)],
      }),
    )
    await result.onDragEnd()

    expect(api.lastCall()).toEqual({
      method: 'PUT',
      path: '/api/people/reorder',
      body: { ids: ['charlie', 'alice', 'bob'] },
    })
    expect(result.sortedPeople.value.map((p) => p.id)).toEqual(['charlie', 'alice', 'bob'])
    app.unmount()
  })

  it('rolls dragList back when reorder fails', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher))
    await flush()

    result.dragging.value = true
    result.dragList.value = [person('charlie', 2), person('alice', 0), person('bob', 1)]
    api.failWith('reorder failed')
    await result.onDragEnd()

    expect(result.error.value).toBe('reorder failed')
    expect(result.dragList.value.map((p) => p.id)).toEqual(['alice', 'bob', 'charlie'])
    app.unmount()
  })

  it('does not clobber dragList from a poll while dragging', async () => {
    const api = fakeFetcher(baseState())
    const { result, app } = withSetup(() => useGameNight(api.fetcher, { pollIntervalMs: 10_000 }))
    await flush()

    result.dragging.value = true
    result.dragList.value = [person('charlie', 2), person('alice', 0), person('bob', 1)]
    await vi.advanceTimersByTimeAsync(10_000)

    expect(result.dragList.value.map((p) => p.id)).toEqual(['charlie', 'alice', 'bob'])
    app.unmount()
  })
})
