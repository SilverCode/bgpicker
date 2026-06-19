<template>
  <div class="app">

    <!-- Identity overlay — shown until the user picks who they are -->
    <div v-if="showIdentityPicker" class="identity-overlay">
      <div class="identity-card">
        <div class="identity-icon">👋</div>
        <h2 class="identity-title">Who are you?</h2>
        <p class="identity-sub">Your suggestions and votes will be remembered on this device.</p>
        <ul class="identity-list">
          <li v-for="p in sortedPeople" :key="p.id">
            <button class="btn-identity" @click="selectIdentity(p.id)">{{ p.name }}</button>
          </li>
        </ul>
      </div>
    </div>

    <!-- Header -->
    <header class="header">
      <div class="header-inner">
        <div class="header-title">
          <span class="header-icon">🎲</span>
          <h1>Board Game Picker</h1>
        </div>
        <div class="header-actions">
          <button
            v-if="myPerson"
            class="btn-who"
            @click="clearIdentity"
            :title="'Playing as ' + myPerson.name + ' — tap to change'"
          >{{ myPerson.name }}</button>
          <button class="btn-icon" @click="showManage = !showManage" title="Manage players">⚙️</button>
        </div>
      </div>
      <div class="tab-bar">
        <button
          class="tab-btn"
          :class="{ 'tab-btn--active': activeTab === 'night' }"
          @click="activeTab = 'night'"
        >🎲 Game Night</button>
        <button
          class="tab-btn"
          :class="{ 'tab-btn--active': activeTab === 'suggestions' }"
          @click="activeTab = 'suggestions'"
        >
          💡 Suggestions
          <span v-if="sortedSuggestions.length > 0" class="tab-badge">{{ sortedSuggestions.length }}</span>
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

        <!-- ── Game Night tab ─────────────────────────────────────────────── -->
        <template v-if="activeTab === 'night'">

          <!-- Current Picker Card -->
          <section v-if="currentPicker && !pendingPick" class="current-card">
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
          <section v-else-if="currentPicker && pendingPick?.personId === currentPicker.id" class="current-card current-card--picked">
            <div class="current-label">🎯 Game chosen!</div>
            <div class="current-name">{{ currentPicker.name }}</div>
            <div class="pick-done">
              <div class="pick-chosen">
                🎮 <strong>{{ pendingPick!.gameName }}</strong>
                <button class="btn-edit" @click="onEditPick" title="Change game">✏️ Edit</button>
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
                    :class="{ 'btn-attend--yes': person.attending === 'yes', 'btn-attend--no': person.attending === 'no' }"
                    @click="toggleAttendance(person.id)"
                    :title="person.attending === 'yes' ? 'Going — click to mark as not going' : person.attending === 'no' ? 'Not going — click to clear' : 'Click to mark as going'"
                  >{{ person.attending === 'yes' ? '✓ Going' : person.attending === 'no' ? '✗ Not going' : 'Going?' }}</button>
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
            <form class="add-form" @submit.prevent="onAddPerson">
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

            <div class="reset-section">
              <button class="btn btn-danger" :disabled="busy" @click="onResetData">
                🗑️ Reset picks &amp; attendance
              </button>
              <p class="hint">Clears picks, suggestions, and attendance. Queue order is kept.</p>
            </div>
          </section>

        </template>

        <!-- ── Suggestions tab ───────────────────────────────────────────── -->
        <template v-if="activeTab === 'suggestions'">

          <form v-if="myPerson" class="suggest-form" @submit.prevent="onSuggest">
            <input
              v-model="suggestGameName"
              type="text"
              placeholder="Suggest a game…"
              class="suggest-input"
              maxlength="80"
            />
            <button type="submit" class="btn btn-accent" :disabled="!suggestGameName.trim() || busy">
              Add
            </button>
          </form>
          <p v-else-if="sortedPeople.length > 0" class="hint hint--center">
            Tap your name in the top right to suggest and vote.
          </p>

          <ul class="suggestion-list" v-if="sortedSuggestions.length > 0">
            <li v-for="s in sortedSuggestions" :key="s.id" class="suggestion-item">
              <div class="suggestion-info">
                <span class="suggestion-game">{{ s.gameName }}</span>
                <span class="suggestion-by"> ({{ nameById(s.suggestedBy) }})</span>
              </div>
              <span
                class="suggestion-score"
                :class="{ 'score--pos': netScore(s) > 0, 'score--neg': netScore(s) < 0 }"
              >{{ netScore(s) > 0 ? '+' : '' }}{{ netScore(s) }}</span>
              <div class="suggestion-vote-btns" v-if="myPerson">
                <button
                  class="btn-vote"
                  :class="{ 'btn-vote--up': myVote(s) === 'up' }"
                  @click="onVote(s, 'up')"
                  title="Thumbs up"
                >👍</button>
                <button
                  class="btn-vote"
                  :class="{ 'btn-vote--down': myVote(s) === 'down' }"
                  @click="onVote(s, 'down')"
                  title="Thumbs down"
                >👎</button>
              </div>
              <button class="btn-remove" @click="removeSuggestion(s.id)" title="Remove suggestion">✕</button>
            </li>
          </ul>
          <p v-else class="hint hint--center">No suggestions yet — be the first!</p>

        </template>

      </template>
    </main>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick } from 'vue'
import draggable from 'vuedraggable'
import {
  useGameNight,
  createApiFetcher,
  netScore,
  type Suggestion,
  type VoteDirection,
} from './composables/useGameNight'

const API = import.meta.env.VITE_API_URL ?? ''

const {
  state,
  loading,
  busy,
  error,
  gameName,
  dragging,
  dragList,
  sessionDateInput,
  sortedPeople,
  currentPicker,
  queueLength,
  recentHistory,
  attendingCount,
  pendingPick,
  sortedSuggestions,
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
  addSuggestion,
  removeSuggestion,
  voteOnSuggestion,
} = useGameNight(createApiFetcher(API))

// ── Presentation-only state ───────────────────────────────────────────────────

const showManage = ref(false)
const newName = ref('')
const gameInputRef = ref<HTMLInputElement | null>(null)
const activeTab = ref<'night' | 'suggestions'>('night')
const suggestGameName = ref('')

// ── Identity — who is using this device ──────────────────────────────────────

const IDENTITY_KEY = 'bgpicker:who'

const myPersonId = ref(localStorage.getItem(IDENTITY_KEY) ?? '')

// myPerson is null when: no identity set, or the stored person was removed.
const myPerson = computed(() => sortedPeople.value.find(p => p.id === myPersonId.value) ?? null)

// Show the identity picker when people exist but no valid identity is set.
const showIdentityPicker = computed(
  () => !loading.value && sortedPeople.value.length > 0 && !myPerson.value,
)

function selectIdentity(id: string) {
  myPersonId.value = id
  localStorage.setItem(IDENTITY_KEY, id)
}

function clearIdentity() {
  myPersonId.value = ''
  localStorage.removeItem(IDENTITY_KEY)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

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

function myVote(s: Suggestion): VoteDirection {
  if (!myPersonId.value) return ''
  return s.votes[myPersonId.value] ?? ''
}

// ── Handlers ──────────────────────────────────────────────────────────────────

async function onAddPerson() {
  if (await addPerson(newName.value)) newName.value = ''
}

function onEditPick() {
  editPick()
  nextTick(() => gameInputRef.value?.focus())
}

function onResetData() {
  if (!confirm('Clear recent picks, suggestions, and attendance? Queue order is kept.')) return
  void resetData()
}

async function onSuggest() {
  if (!myPersonId.value) return
  if (await addSuggestion(myPersonId.value, suggestGameName.value)) {
    suggestGameName.value = ''
  }
}

function onVote(s: Suggestion, direction: VoteDirection) {
  if (!myPersonId.value) return
  // Clicking the active direction retracts; clicking the other switches sides.
  const newDir: VoteDirection = myVote(s) === direction ? '' : direction
  void voteOnSuggestion(s.id, myPersonId.value, newDir)
}
</script>

<style scoped>
/* ── Layout ──────────────────────────────────────────────────────────────── */
.app {
  max-width: 540px;
  margin: 0 auto;
  padding-bottom: 3rem;
}

/* ── Identity overlay ────────────────────────────────────────────────────── */
.identity-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 1.5rem;
}
.identity-card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  padding: 2rem 1.5rem;
  width: 100%;
  max-width: 360px;
  text-align: center;
}
.identity-icon { font-size: 2.5rem; margin-bottom: 0.5rem; }
.identity-title { font-size: 1.3rem; font-weight: 800; margin-bottom: 0.4rem; }
.identity-sub { font-size: 0.85rem; color: var(--text-muted); margin-bottom: 1.25rem; }
.identity-list { list-style: none; display: flex; flex-direction: column; gap: 0.5rem; }
.btn-identity {
  width: 100%;
  padding: 0.8rem 1rem;
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 1rem;
  font-weight: 600;
  color: var(--text);
  transition: all 0.12s;
}
.btn-identity:hover {
  border-color: var(--accent);
  color: var(--accent-light);
  background: var(--accent-dim);
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
  padding: 0.9rem 1rem 0.6rem;
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
.header-actions { display: flex; align-items: center; gap: 0.5rem; }
.btn-who {
  background: var(--accent-dim);
  border: 1px solid var(--accent);
  border-radius: 999px;
  color: var(--accent-light);
  font-size: 0.8rem;
  font-weight: 600;
  padding: 3px 10px;
  white-space: nowrap;
}
.btn-who:hover { background: var(--accent); color: #fff; }
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

/* ── Tab bar ─────────────────────────────────────────────────────────────── */
.tab-bar {
  display: flex;
  padding: 0 1rem 0;
  gap: 0;
}
.tab-btn {
  flex: 1;
  padding: 0.55rem 0.5rem;
  font-size: 0.85rem;
  font-weight: 600;
  color: var(--text-muted);
  border-bottom: 2px solid transparent;
  border-radius: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.35rem;
  transition: all 0.12s;
}
.tab-btn:hover { color: var(--text); }
.tab-btn--active {
  color: var(--accent-light);
  border-bottom-color: var(--accent);
}
.tab-badge {
  background: var(--accent);
  color: #fff;
  font-size: 0.7rem;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 999px;
  line-height: 1.4;
}

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
.btn-danger {
  background: var(--danger-dim);
  color: var(--danger);
  border: 1px solid var(--danger);
  width: 100%;
}
.btn-danger:not(:disabled):hover { background: var(--danger); color: #fff; }

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
.btn-attend--no {
  background: var(--danger-dim);
  border-color: var(--danger);
  color: var(--danger);
}
.btn-attend--no:hover { border-color: var(--danger); color: var(--danger); }

/* Drag handle */
.drag-handle {
  font-size: 1.1rem;
  color: transparent;
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
.hint--center { text-align: center; padding: 2rem 0; }

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

/* ── Reset section (manage panel) ───────────────────────────────────────── */
.reset-section { margin-top: 1rem; padding-top: 1rem; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 0.4rem; }

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

/* ── Suggestions tab ─────────────────────────────────────────────────────── */
.suggest-form {
  display: flex;
  gap: 0.5rem;
}
.suggest-input {
  flex: 1;
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text);
  padding: 0.65rem 0.9rem;
  font-size: 0.9rem;
  transition: border-color 0.15s;
}
.suggest-input:focus { border-color: var(--accent); outline: none; }
.suggest-input::placeholder { color: var(--text-muted); }

.suggestion-list { list-style: none; display: flex; flex-direction: column; gap: 0.4rem; }
.suggestion-item {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 0.65rem 0.9rem;
}
.suggestion-info { flex: 1; min-width: 0; }
.suggestion-game { font-weight: 600; font-size: 0.95rem; }
.suggestion-by { font-size: 0.82rem; color: var(--text-muted); }
.suggestion-score {
  font-size: 0.85rem;
  font-weight: 700;
  min-width: 2.2rem;
  text-align: center;
  flex-shrink: 0;
  color: var(--text-muted);
}
.score--pos { color: var(--success); }
.score--neg { color: var(--danger); }
.suggestion-vote-btns { display: flex; gap: 0.25rem; flex-shrink: 0; }
.btn-vote {
  background: var(--surface2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-size: 0.9rem;
  padding: 3px 8px;
  line-height: 1.5;
  transition: all 0.12s;
}
.btn-vote:hover { border-color: var(--accent); }
.btn-vote--up { background: var(--success-dim); border-color: var(--success); }
.btn-vote--down { background: var(--danger-dim); border-color: var(--danger); }
</style>
