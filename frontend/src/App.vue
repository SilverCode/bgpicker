<template>
  <div class="app">
    <!-- Header -->
    <header class="header">
      <div class="header-inner">
        <div class="header-title">
          <span class="header-icon">🎲</span>
          <h1>Board Game Picker</h1>
        </div>
        <button class="btn-icon" @click="showManage = !showManage" title="Manage players">
          ⚙️
        </button>
      </div>
    </header>

    <main class="main">

      <!-- Error banner -->
      <div v-if="error" class="error-banner">
        {{ error }}
        <button @click="error = ''" class="error-close">✕</button>
      </div>

      <!-- Loading -->
      <div v-if="loading && !state" class="loading-wrap">
        <div class="spinner"></div>
        <p>Loading…</p>
      </div>

      <template v-else>

        <!-- Current Picker Card -->
        <section v-if="currentPicker && !state?.pendingPick" class="current-card">
          <div class="current-label">🎯 It's your turn to pick!</div>
          <div class="current-name">{{ currentPicker.name }}</div>

          <!-- Game input -->
          <div class="pick-form">
            <div class="input-row">
              <input
                v-model="gameName"
                type="text"
                placeholder="Enter a board game…"
                class="game-input"
                @keydown.enter="submitPick"
                maxlength="80"
                ref="gameInputRef"
              />
            </div>
            <div class="pick-actions">
              <button class="btn btn-primary" :disabled="!gameName.trim() || busy" @click="submitPick">
                ✅ Pick this game
              </button>
              <button class="btn btn-secondary" :disabled="busy || queueLength <= 1" @click="submitSkip">
                ⏭️ Skip my turn
              </button>
            </div>
            <p v-if="queueLength <= 1" class="hint">Add more people to enable skipping.</p>
          </div>
        </section>

        <!-- Picker owns the pending pick: show edit + done controls -->
        <section v-else-if="currentPicker && state?.pendingPick?.personId === currentPicker.id" class="current-card current-card--picked">
          <div class="current-label">🎯 Game chosen!</div>
          <div class="current-name">{{ currentPicker.name }}</div>
          <div class="pick-done">
            <div class="pick-chosen">
              🎮 <strong>{{ state!.pendingPick!.gameName }}</strong>
              <button class="btn-edit" @click="editPick" title="Change game">✏️ Edit</button>
            </div>
            <p class="pick-done-text">Tap below when the night is over to pass the turn.</p>
            <button class="btn btn-success" :disabled="busy" @click="confirmDone">
              🏁 Done — end of the night
            </button>
          </div>
        </section>

        <!-- No people state -->
        <section v-else-if="sortedPeople.length === 0" class="empty-state">
          <div class="empty-icon">🎲</div>
          <h2>No players yet</h2>
          <p>Tap ⚙️ above to add some people!</p>
        </section>

        <!-- Next session strip -->
        <section class="session-strip" v-if="state?.nextSession">
          <div class="session-info">
            <span class="session-icon">📅</span>
            <div class="session-text">
              <span class="session-label">Next session</span>
              <span class="session-date">{{ formatSessionDate(state.nextSession) }}</span>
            </div>
          </div>
          <div class="session-count" v-if="sortedPeople.length > 0">
            <span class="count-num">{{ attendingCount }}</span>
            <span class="count-denom">/{{ sortedPeople.length }}</span>
            <span class="count-label">going</span>
          </div>
        </section>

        <!-- Queue -->
        <section class="queue-section" v-if="sortedPeople.length > 0">
          <h2 class="section-title">
            Queue
            <span v-if="showManage" class="section-hint">drag ⠿ to reorder</span>
          </h2>
          <draggable
            v-model="dragList"
            item-key="id"
            tag="ul"
            class="queue-list"
            handle=".drag-handle"
            :disabled="!showManage"
            ghost-class="queue-item--ghost"
            chosen-class="queue-item--chosen"
            animation="180"
            @start="dragging = true"
            @end="onDragEnd"
          >
            <template #item="{ element: person, index }">
              <li
                class="queue-item"
                :class="{ 'queue-item--current': index === 0 }"
              >
                <span
                  class="drag-handle"
                  :class="{ 'drag-handle--active': showManage }"
                  title="Drag to reorder"
                >⠿</span>
                <div class="queue-position">
                  <span v-if="index === 0" class="position-crown">👑</span>
                  <span v-else class="position-num">{{ index + 1 }}</span>
                </div>
                <div class="queue-name">{{ person.name }}</div>
                <button
                  class="btn-attend"
                  :class="{ 'btn-attend--yes': person.attending }"
                  @click="toggleAttendance(person.id)"
                  :title="person.attending ? 'Mark as not going' : 'Mark as going'"
                >{{ person.attending ? '✓ Going' : 'Going?' }}</button>
                <button
                  v-if="showManage"
                  class="btn-remove"
                  @click="removePerson(person.id)"
                  title="Remove player"
                >✕</button>
              </li>
            </template>
          </draggable>
        </section>

        <!-- History -->
        <section class="history-section" v-if="recentHistory.length > 0">
          <h2 class="section-title">Recent picks</h2>
          <ul class="history-list">
            <li v-for="(pick, i) in recentHistory" :key="i" class="history-item">
              <span class="history-person">{{ nameById(pick.personId) }}</span>
              <span v-if="!pick.skipped" class="history-game">{{ pick.gameName }}</span>
              <span v-else class="history-skipped">skipped</span>
              <span class="history-time">{{ formatDate(pick.pickedAt) }}</span>
            </li>
          </ul>
        </section>

        <!-- Manage panel: add players + session date -->
        <section v-if="showManage" class="manage-section">
          <h2 class="section-title">Manage Players</h2>
          <form class="add-form" @submit.prevent="addPerson">
            <input
              v-model="newName"
              type="text"
              placeholder="Player name…"
              class="add-input"
              maxlength="40"
            />
            <button type="submit" class="btn btn-accent" :disabled="!newName.trim() || busy">
              + Add
            </button>
          </form>
          <p class="hint" v-if="sortedPeople.length > 0">Tap ✕ next to a name in the queue to remove them.</p>

          <div class="session-edit">
            <label class="session-edit-label">📅 Next session date</label>
            <div class="session-edit-row">
              <input
                type="date"
                v-model="sessionDateInput"
                class="date-input"
              />
              <button class="btn btn-accent" :disabled="busy" @click="updateSessionDate">
                Save
              </button>
            </div>
          </div>
        </section>

      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import draggable from 'vuedraggable'

interface Person {
  id: string
  name: string
  position: number
  attending: boolean
}

interface Pick {
  personId: string
  gameName: string
  pickedAt: string
  skipped: boolean
}

interface PendingPick {
  personId: string
  gameName: string
  setAt: string
}

interface State {
  people: Person[]
  history: Pick[]
  pendingPick?: PendingPick
  nextSession?: string // ISO date string
}

const API = import.meta.env.VITE_API_URL ?? ''

const state = ref<State | null>(null)
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const showManage = ref(false)
const newName = ref('')
const gameName = ref('')
const dragging = ref(false)
const dragList = ref<Person[]>([])
const gameInputRef = ref<HTMLInputElement | null>(null)
const editing = ref(false) // true while the user is editing a pending pick
const sessionDateInput = ref('')

let pollTimer: ReturnType<typeof setInterval> | null = null

// ── computed ──────────────────────────────────────────────────────────────────

const sortedPeople = computed<Person[]>(() => {
  if (!state.value) return []
  return [...state.value.people].sort((a, b) => a.position - b.position)
})

// Keep dragList in sync with server state, but not while a drag is in progress
watch(sortedPeople, (val) => {
  if (!dragging.value) dragList.value = [...val]
}, { immediate: true })

const currentPicker = computed<Person | null>(() => sortedPeople.value[0] ?? null)

const queueLength = computed(() => sortedPeople.value.length)

const recentHistory = computed<Pick[]>(() => {
  if (!state.value) return []
  return [...state.value.history].reverse().slice(0, 8)
})

const attendingCount = computed(() =>
  sortedPeople.value.filter(p => p.attending).length
)

// Keep sessionDateInput in sync with server state
watch(() => state.value?.nextSession, (iso) => {
  if (iso) sessionDateInput.value = iso.slice(0, 10) // "YYYY-MM-DD"
}, { immediate: true })

// ── helpers ───────────────────────────────────────────────────────────────────

function nameById(id: string): string {
  return state.value?.people.find(p => p.id === id)?.name ?? 'Unknown'
}

function formatSessionDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

async function apiFetch(method: string, path: string, body?: unknown): Promise<State> {
  const res = await fetch(`${API}${path}`, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await res.json()
  if (!res.ok) throw new Error(data.error ?? 'Request failed')
  return data
}

// ── actions ───────────────────────────────────────────────────────────────────

async function loadState() {
  loading.value = true
  try {
    const s = await apiFetch('GET', '/api/state')
    // Don't let a background poll restore the pending pick while the user
    // is actively editing their choice — it would snap the input away.
    if (editing.value) s.pendingPick = undefined
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

async function addPerson() {
  if (!newName.value.trim()) return
  busy.value = true
  try {
    await apiFetch('POST', '/api/people', { name: newName.value.trim() })
    newName.value = ''
    await loadState()
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function removePerson(id: string) {
  busy.value = true
  try {
    const s = await apiFetch('DELETE', `/api/people/${id}`)
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function submitPick() {
  const name = gameName.value.trim()
  if (!name || !currentPicker.value) return
  busy.value = true
  try {
    // Immediately persists the pending pick — all devices will see it on next poll
    const s = await apiFetch('POST', `/api/people/${currentPicker.value.id}/pick`, { gameName: name })
    state.value = s
    gameName.value = ''
    editing.value = false
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

function editPick() {
  // Pre-fill the input with the current pending pick name
  if (state.value?.pendingPick) {
    gameName.value = state.value.pendingPick.gameName
  }
  // Hide the confirmation card locally; the editing flag prevents any
  // background poll from restoring pendingPick until the user re-submits
  editing.value = true
  if (state.value) state.value = { ...state.value, pendingPick: undefined }
  nextTick(() => gameInputRef.value?.focus())
}

async function submitSkip() {
  if (!currentPicker.value) return
  busy.value = true
  try {
    const s = await apiFetch('POST', `/api/people/${currentPicker.value.id}/skip`)
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

// Finalise the pending pick: record history + rotate queue
async function confirmDone() {
  if (!currentPicker.value) return
  busy.value = true
  try {
    const s = await apiFetch('POST', `/api/people/${currentPicker.value.id}/done`)
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function toggleAttendance(id: string) {
  busy.value = true
  try {
    const s = await apiFetch('POST', `/api/people/${id}/attend`)
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function updateSessionDate() {
  if (!sessionDateInput.value) return
  busy.value = true
  try {
    const s = await apiFetch('PUT', '/api/session', { date: sessionDateInput.value })
    state.value = s
  } catch (e: any) {
    error.value = e.message
  } finally {
    busy.value = false
  }
}

async function onDragEnd() {
  dragging.value = false
  busy.value = true
  try {
    const s = await apiFetch('PUT', '/api/people/reorder', { ids: dragList.value.map(p => p.id) })
    state.value = s
  } catch (e: any) {
    error.value = e.message
    // Roll back to last known good order
    dragList.value = [...sortedPeople.value]
  } finally {
    busy.value = false
  }
}

// ── lifecycle ─────────────────────────────────────────────────────────────────

onMounted(() => {
  loadState()
  // Poll every 10s so all devices stay in sync
  pollTimer = setInterval(loadState, 10_000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
/* ── Layout ──────────────────────────────────────────────────────────────── */
.app {
  max-width: 540px;
  margin: 0 auto;
  padding-bottom: 3rem;
}

/* ── Header ──────────────────────────────────────────────────────────────── */
.header {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--bg);
  border-bottom: 1px solid var(--border);
}
.header-inner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.9rem 1rem;
}
.header-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.header-icon { font-size: 1.4rem; }
h1 {
  font-size: 1.1rem;
  font-weight: 700;
  letter-spacing: -0.02em;
}
.btn-icon {
  background: var(--surface2);
  border-radius: var(--radius-sm);
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.1rem;
}
.btn-icon:hover { background: var(--border); }

/* ── Main ────────────────────────────────────────────────────────────────── */
.main {
  padding: 1rem;
  display: flex;
  flex-direction: column;
  gap: 1.2rem;
}

/* ── Error ───────────────────────────────────────────────────────────────── */
.error-banner {
  background: var(--danger-dim);
  border: 1px solid var(--danger);
  border-radius: var(--radius-sm);
  color: var(--danger);
  padding: 0.75rem 1rem;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 0.9rem;
}
.error-close {
  background: none;
  color: var(--danger);
  font-size: 0.9rem;
  padding: 0 4px;
}

/* ── Loading ─────────────────────────────────────────────────────────────── */
.loading-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 3rem;
  color: var(--text-muted);
}
.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* ── Current Picker Card ─────────────────────────────────────────────────── */
.current-card {
  background: linear-gradient(135deg, var(--surface) 0%, var(--surface2) 100%);
  border: 1px solid var(--accent);
  border-radius: var(--radius);
  padding: 1.4rem;
  box-shadow: 0 0 0 1px var(--accent-dim), var(--shadow);
}
.current-card--picked {
  border-color: var(--success);
  box-shadow: 0 0 0 1px var(--success-dim), var(--shadow);
}
.current-label {
  font-size: 0.8rem;
  font-weight: 600;
  color: var(--accent-light);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 0.35rem;
}
.current-name {
  font-size: 1.8rem;
  font-weight: 800;
  letter-spacing: -0.03em;
  margin-bottom: 1.2rem;
}

/* ── Pick form ───────────────────────────────────────────────────────────── */
.pick-form { display: flex; flex-direction: column; gap: 0.75rem; }
.input-row { display: flex; gap: 0.5rem; }
.game-input {
  flex: 1;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  padding: 0.65rem 0.9rem;
  font-size: 1rem;
  transition: border-color 0.15s;
}
.game-input:focus { border-color: var(--accent); }
.game-input::placeholder { color: var(--text-muted); }

.pick-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.5rem;
}

/* ── Pick Done ───────────────────────────────────────────────────────────── */
.pick-done { display: flex; flex-direction: column; gap: 0.75rem; }
.pick-chosen {
  background: var(--success-dim);
  border: 1px solid var(--success);
  border-radius: var(--radius-sm);
  padding: 0.75rem 1rem;
  font-size: 1.1rem;
  color: var(--success);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}
.btn-edit {
  background: none;
  color: var(--text-muted);
  font-size: 0.8rem;
  padding: 3px 8px;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  white-space: nowrap;
  flex-shrink: 0;
}
.btn-edit:hover {
  border-color: var(--accent);
  color: var(--accent-light);
}
.pick-done-text { color: var(--text-muted); font-size: 0.9rem; }

/* ── Buttons ─────────────────────────────────────────────────────────────── */
.btn {
  padding: 0.65rem 1rem;
  border-radius: var(--radius-sm);
  font-size: 0.9rem;
  font-weight: 600;
}
.btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-primary {
  background: var(--accent);
  color: #fff;
}
.btn-primary:not(:disabled):hover { background: var(--accent-light); }
.btn-secondary {
  background: var(--surface2);
  color: var(--text);
  border: 1px solid var(--border);
}
.btn-secondary:not(:disabled):hover { border-color: var(--accent); color: var(--accent-light); }
.btn-success {
  background: var(--success);
  color: #fff;
}
.btn-success:not(:disabled):hover { opacity: 0.88; }
.btn-accent {
  background: var(--accent);
  color: #fff;
  white-space: nowrap;
}
.btn-accent:not(:disabled):hover { background: var(--accent-light); }

/* ── Empty State ─────────────────────────────────────────────────────────── */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 3rem 1rem;
  text-align: center;
  color: var(--text-muted);
}
.empty-icon { font-size: 3rem; }
.empty-state h2 { color: var(--text); font-size: 1.2rem; font-weight: 700; }

/* ── Section ─────────────────────────────────────────────────────────────── */
.section-title {
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-muted);
  margin-bottom: 0.6rem;
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.section-hint {
  font-size: 0.7rem;
  font-weight: 400;
  color: var(--accent-light);
  text-transform: none;
  letter-spacing: 0;
}

/* ── Queue ───────────────────────────────────────────────────────────────── */
.queue-list { list-style: none; display: flex; flex-direction: column; gap: 0.4rem; }
.queue-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 0.7rem 0.9rem;
}
.queue-item--current {
  border-color: var(--accent);
  background: var(--accent-dim);
}
.queue-position {
  width: 28px;
  text-align: center;
  flex-shrink: 0;
}
.position-crown { font-size: 1.2rem; }
.position-num { color: var(--text-muted); font-size: 0.85rem; font-weight: 600; }
.queue-name { flex: 1; font-weight: 500; min-width: 0; }

/* Attendance toggle */
.btn-attend {
  background: var(--surface2);
  color: var(--text-muted);
  font-size: 0.75rem;
  font-weight: 600;
  padding: 3px 9px;
  border-radius: 999px;
  border: 1px solid var(--border);
  white-space: nowrap;
  flex-shrink: 0;
  transition: all 0.15s;
}
.btn-attend:hover { border-color: var(--success); color: var(--success); }
.btn-attend--yes {
  background: var(--success-dim);
  border-color: var(--success);
  color: var(--success);
}

/* Drag handle */
.drag-handle {
  font-size: 1.1rem;
  color: transparent; /* invisible when not in manage mode */
  cursor: default;
  user-select: none;
  flex-shrink: 0;
  width: 20px;
  text-align: center;
  transition: color 0.15s;
}
.drag-handle--active {
  color: var(--text-muted);
  cursor: grab;
}
.drag-handle--active:hover { color: var(--accent-light); }
.drag-handle--active:active { cursor: grabbing; }

/* Sortable ghost / chosen states */
.queue-item--ghost {
  opacity: 0.35;
  background: var(--accent-dim) !important;
  border-color: var(--accent) !important;
}
.queue-item--chosen {
  box-shadow: 0 4px 16px rgba(124, 106, 247, 0.3);
}

.btn-remove {
  background: none;
  color: var(--text-muted);
  font-size: 0.85rem;
  padding: 2px 6px;
  border-radius: 4px;
}
.btn-remove:hover { background: var(--danger-dim); color: var(--danger); }

/* ── History ─────────────────────────────────────────────────────────────── */
.history-list { list-style: none; display: flex; flex-direction: column; gap: 0.35rem; }
.history-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.88rem;
  padding: 0.5rem 0.75rem;
  background: var(--surface);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
}
.history-person { font-weight: 600; min-width: 5rem; }
.history-game { flex: 1; color: var(--text-muted); }
.history-skipped { flex: 1; color: var(--text-muted); font-style: italic; }
.history-time { color: var(--text-muted); font-size: 0.78rem; flex-shrink: 0; }

/* ── Manage ──────────────────────────────────────────────────────────────── */
.manage-section {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 1.2rem;
}
.add-form { display: flex; gap: 0.5rem; margin-bottom: 0.75rem; }
.add-input {
  flex: 1;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  padding: 0.65rem 0.9rem;
  font-size: 1rem;
  transition: border-color 0.15s;
}
.add-input:focus { border-color: var(--accent); }
.add-input::placeholder { color: var(--text-muted); }
.hint { color: var(--text-muted); font-size: 0.82rem; }

/* ── Session strip ───────────────────────────────────────────────────────── */
.session-strip {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 0.65rem 0.9rem;
  gap: 0.75rem;
}
.session-info { display: flex; align-items: center; gap: 0.6rem; }
.session-icon { font-size: 1.1rem; flex-shrink: 0; }
.session-text { display: flex; flex-direction: column; gap: 0.05rem; }
.session-label { font-size: 0.7rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-muted); }
.session-date { font-size: 0.9rem; font-weight: 600; }
.session-count { display: flex; align-items: baseline; gap: 0.15rem; flex-shrink: 0; }
.count-num { font-size: 1.1rem; font-weight: 800; color: var(--success); }
.count-denom { font-size: 0.85rem; color: var(--text-muted); }
.count-label { font-size: 0.75rem; color: var(--text-muted); margin-left: 0.2rem; }

/* ── Session date editor (manage panel) ──────────────────────────────────── */
.session-edit { margin-top: 1rem; padding-top: 1rem; border-top: 1px solid var(--border); }
.session-edit-label { display: block; font-size: 0.8rem; font-weight: 600; color: var(--text-muted); margin-bottom: 0.5rem; }
.session-edit-row { display: flex; gap: 0.5rem; }
.date-input {
  flex: 1;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  padding: 0.65rem 0.9rem;
  font-size: 0.95rem;
  transition: border-color 0.15s;
  color-scheme: dark;
}
.date-input:focus { border-color: var(--accent); outline: none; }

</style>
