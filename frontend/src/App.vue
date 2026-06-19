<template>
  <div class="app">

    <!-- Clear backdrop — closes identity dropdown on outside click -->
    <div v-if="showUserPicker" class="backdrop-clear" @click="showUserPicker = false" />

    <!-- Identity dropdown (fixed below header) -->
    <Transition name="fade-fast">
      <div v-if="showUserPicker" class="user-dropdown">
        <button
          v-for="p in sortedPeople"
          :key="p.id"
          class="user-option"
          :class="{ 'user-option--active': p.id === myPersonId }"
          @click="selectIdentity(p.id)"
        >{{ p.name }}</button>
        <button v-if="myPersonId" class="user-option user-option--clear" @click="clearIdentity">
          Clear identity
        </button>
      </div>
    </Transition>

    <!-- Header -->
    <header class="hdr">
      <div class="hdr-row">
        <div class="hdr-brand">🎲 Board Game Picker</div>
        <div class="hdr-right">
          <button class="btn-id-pill" @click.stop="showUserPicker = !showUserPicker">
            {{ myPerson?.name ?? 'Pick me' }}
          </button>
          <button class="btn-gear" @click="showSettings = true">⚙️</button>
        </div>
      </div>
      <div class="tab-bar">
        <button
          class="tab"
          :class="{ 'tab--active': activeTab === 'night' }"
          @click="activeTab = 'night'"
        >🎯 Game Night</button>
        <button
          class="tab"
          :class="{ 'tab--active': activeTab === 'suggestions' }"
          @click="activeTab = 'suggestions'"
        >
          💡 Suggestions
          <span v-if="sortedSuggestions.length" class="tab-badge">{{ sortedSuggestions.length }}</span>
        </button>
      </div>
    </header>

    <!-- Page content -->
    <div class="content">

      <div v-if="error" class="err-bar">
        {{ error }}
        <button class="err-close" @click="error = ''">×</button>
      </div>

      <div v-if="loading && !state" class="loading-wrap">
        <div class="spinner" />
        <span>Loading…</span>
      </div>

      <template v-else>

        <!-- ── GAME NIGHT TAB ───────────────────────────────────────────── -->
        <template v-if="activeTab === 'night'">

          <div v-if="!state || sortedPeople.length === 0" class="empty-wrap">
            <span class="empty-icon">🎲</span>
            <p>No players yet. Tap ⚙️ to add some!</p>
          </div>

          <template v-else>

            <!-- Pre-pick card -->
            <div v-if="!state.pendingPick" class="picker-card picker-card--pre">
              <div class="pc-label">🎯 IT'S YOUR TURN TO PICK!</div>
              <div class="pc-name">{{ currentPicker?.name ?? '—' }}</div>
              <input
                v-model="gameName"
                class="pc-input"
                placeholder="Enter a board game…"
                maxlength="80"
                @keydown.enter.prevent="submitPick"
              />
              <div class="pc-btns">
                <button class="btn-pick" :disabled="!gameName.trim() || busy" @click="submitPick">
                  ✅ Pick this game
                </button>
                <button class="btn-skip" :disabled="busy || queueLength <= 1" @click="submitSkip">
                  ⏩ Skip my turn
                </button>
              </div>
            </div>

            <!-- Pending card -->
            <div v-else class="picker-card picker-card--pending">
              <div class="pc-label pc-label--green">🕐 PENDING PICK</div>
              <div class="pc-name-sm">{{ currentPicker?.name ?? '—' }}</div>
              <div class="pc-game-display">→ {{ gameName || state.pendingPick.gameName }}</div>
              <input
                v-model="gameName"
                class="pc-input pc-input--green"
                placeholder="Edit game name…"
                maxlength="80"
              />
              <button class="btn-done" :disabled="busy" @click="onDoneClick">
                🏁 Done — End of night
              </button>
            </div>

            <!-- Next session strip -->
            <div class="session-strip">
              <div class="ss-left">
                <span class="ss-icon">📅</span>
                <div>
                  <div class="ss-label">NEXT SESSION</div>
                  <div class="ss-date">{{ state.nextSession ? formatSessionDate(state.nextSession) : '—' }}</div>
                </div>
              </div>
              <div class="ss-right">
                <span class="ss-going">{{ attendingCount }}</span>/{{ queueLength }}<span class="ss-going-label"> going</span>
              </div>
            </div>

            <!-- Queue -->
            <div class="section">
              <div class="section-hdr">QUEUE</div>
              <div class="queue-list">
                <div
                  v-for="(person, i) in sortedPeople"
                  :key="person.id"
                  class="q-row"
                  :class="{ 'q-row--first': i === 0 }"
                >
                  <div class="q-pos">
                    <span v-if="i === 0">👑</span>
                    <span v-else class="q-num">{{ i + 1 }}</span>
                  </div>
                  <div class="q-name" :class="{ 'q-name--bold': i === 0 }">{{ person.name }}</div>
                  <button
                    class="btn-att"
                    :class="{
                      'btn-att--yes': person.attending === 'yes',
                      'btn-att--no': person.attending === 'no',
                    }"
                    @click="toggleAttendance(person.id)"
                  >{{ attLabel(person.attending) }}</button>
                </div>
              </div>
            </div>

            <!-- Recent picks -->
            <div v-if="recentHistory.length" class="section">
              <div class="section-hdr">RECENT PICKS</div>
              <div class="history-list">
                <div v-for="(pick, i) in recentHistory.slice(0, 5)" :key="i" class="h-row">
                  <span class="h-person">{{ nameById(pick.personId) }}</span>
                  <span v-if="!pick.skipped" class="h-game">{{ pick.gameName }}</span>
                  <span v-else class="h-game h-game--skip">skipped</span>
                  <span class="h-date">{{ formatDate(pick.pickedAt) }}</span>
                </div>
              </div>
            </div>

          </template>
        </template>

        <!-- ── SUGGESTIONS TAB ─────────────────────────────────────────── -->
        <template v-else-if="activeTab === 'suggestions'">

          <div class="sugg-add">
            <input
              v-model="suggestGameName"
              class="sugg-input"
              placeholder="Suggest a game…"
              maxlength="80"
              @keydown.enter.prevent="onSuggest"
            />
            <button class="btn-sugg-add" :disabled="busy" @click="onSuggest">Add</button>
          </div>

          <div v-if="!sortedSuggestions.length" class="sugg-empty">
            <div class="sugg-empty-icon">💡</div>
            <p>No suggestions yet. Add one above!</p>
          </div>

          <div v-else class="sugg-list">
            <div v-for="s in sortedSuggestions" :key="s.id" class="sugg-row">
              <div class="sugg-info">
                <span class="sugg-game">{{ s.gameName }}</span>
                <span class="sugg-by"> ({{ nameById(s.suggestedBy) }})</span>
              </div>
              <span class="sugg-score" :class="scoreClass(netScore(s))">{{ formatScore(netScore(s)) }}</span>
              <div class="vote-pill" :class="votePillClass(myVote(s))">
                <button
                  class="vp-seg vp-up"
                  :class="{ 'vp-up--on': myVote(s) === 'up' }"
                  @click="onVote(s, 'up')"
                >👍</button>
                <button
                  class="vp-seg vp-down"
                  :class="{ 'vp-down--on': myVote(s) === 'down' }"
                  @click="onVote(s, 'down')"
                >👎</button>
                <button class="vp-seg vp-x" @click="removeSuggestion(s.id)">×</button>
              </div>
            </div>
          </div>

        </template>

      </template>
    </div>

    <div class="content-spacer" />

    <!-- Settings sheet backdrop -->
    <Transition name="fade-bg">
      <div v-if="showSettings" class="sheet-bg" @click="showSettings = false" />
    </Transition>

    <!-- Settings bottom sheet -->
    <Transition name="slide-up">
      <div v-if="showSettings" class="sheet">
        <div class="sheet-handle" />

        <div class="sheet-section-label">MANAGE PLAYERS</div>
        <div class="sheet-input-row">
          <input
            v-model="newName"
            class="sheet-input"
            placeholder="Player name…"
            maxlength="40"
            @keydown.enter.prevent="onAddPerson"
          />
          <button class="btn-sheet-add" :disabled="busy" @click="onAddPerson">+ Add</button>
        </div>
        <div v-if="sortedPeople.length" class="sheet-hint">Tap × to remove a player.</div>

        <div class="sheet-players">
          <div v-for="p in sortedPeople" :key="p.id" class="sheet-player-row">
            <span class="sheet-player-name">{{ p.name }}</span>
            <button class="btn-sheet-remove" @click="removePerson(p.id)">×</button>
          </div>
        </div>

        <div class="sheet-divider" />

        <div class="sheet-date-hdr">
          <span>📅</span>
          <span>Next session date</span>
        </div>
        <div class="sheet-input-row">
          <input
            type="date"
            v-model="sessionDateInput"
            class="sheet-input sheet-date-input"
          />
          <button class="btn-sheet-add" :disabled="busy" @click="updateSessionDate">Save</button>
        </div>

        <div class="sheet-divider" />

        <button class="btn-reset" :disabled="busy" @click="onResetData">
          🗑️ Reset picks &amp; attendance
        </button>
        <p class="sheet-reset-hint">Clears picks, suggestions, and attendance. Queue order is kept.</p>
      </div>
    </Transition>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import {
  useGameNight,
  createApiFetcher,
  netScore,
  type Suggestion,
  type VoteDirection,
  type Person,
} from './composables/useGameNight'

const API = import.meta.env.VITE_API_URL ?? ''

const {
  state,
  loading,
  busy,
  error,
  gameName,
  sessionDateInput,
  sortedPeople,
  currentPicker,
  queueLength,
  recentHistory,
  attendingCount,
  sortedSuggestions,
  addPerson,
  removePerson,
  submitPick,
  submitSkip,
  confirmDone,
  toggleAttendance,
  resetData,
  updateSessionDate,
  addSuggestion,
  removeSuggestion,
  voteOnSuggestion,
} = useGameNight(createApiFetcher(API))

// ── UI state ──────────────────────────────────────────────────────────────────

const showUserPicker = ref(false)
const showSettings = ref(false)
const activeTab = ref<'night' | 'suggestions'>('night')
const newName = ref('')
const suggestGameName = ref('')

// ── Identity ──────────────────────────────────────────────────────────────────

const IDENTITY_KEY = 'bgpicker:who'
const myPersonId = ref(localStorage.getItem(IDENTITY_KEY) ?? '')
const myPerson = computed(() => sortedPeople.value.find(p => p.id === myPersonId.value) ?? null)

function selectIdentity(id: string) {
  myPersonId.value = id
  localStorage.setItem(IDENTITY_KEY, id)
  showUserPicker.value = false
}

function clearIdentity() {
  myPersonId.value = ''
  localStorage.removeItem(IDENTITY_KEY)
  showUserPicker.value = false
}

// ── Pending pick sync ─────────────────────────────────────────────────────────

// Pre-fill gameName when a pending pick first appears; clear it when it's gone.
// Skips overwrites mid-edit: if old name was already set, the user is editing.
watch(
  () => state.value?.pendingPick?.gameName,
  (name, oldName) => {
    if (!name) {
      gameName.value = ''
    } else if (!oldName) {
      gameName.value = name
    }
  },
)

// ── Helpers ───────────────────────────────────────────────────────────────────

function nameById(id: string): string {
  return state.value?.people.find(p => p.id === id)?.name ?? 'Unknown'
}

function formatSessionDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    weekday: 'short', day: 'numeric', month: 'short', year: 'numeric',
  })
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function myVote(s: Suggestion): VoteDirection {
  if (!myPersonId.value) return ''
  return s.votes[myPersonId.value] ?? ''
}

function attLabel(att: Person['attending']): string {
  if (att === 'yes') return '✓ Going'
  if (att === 'no') return '✗ Not going'
  return 'Going?'
}

function scoreClass(score: number): string {
  if (score > 0) return 'sugg-score--pos'
  if (score < 0) return 'sugg-score--neg'
  return ''
}

function formatScore(score: number): string {
  return score > 0 ? `+${score}` : String(score)
}

function votePillClass(vote: VoteDirection): string {
  if (vote === 'up') return 'vote-pill--up'
  if (vote === 'down') return 'vote-pill--down'
  return ''
}

// ── Handlers ──────────────────────────────────────────────────────────────────

async function onAddPerson() {
  if (await addPerson(newName.value)) newName.value = ''
}

async function onDoneClick() {
  const pendingName = state.value?.pendingPick?.gameName
  if (gameName.value.trim() && gameName.value.trim() !== pendingName) {
    await submitPick()
    if (error.value) return
  }
  await confirmDone()
}

function onResetData() {
  if (!confirm('Clear picks, suggestions, and attendance? Queue order is kept.')) return
  void resetData()
}

async function onSuggest() {
  if (!myPersonId.value) { showUserPicker.value = true; return }
  if (await addSuggestion(myPersonId.value, suggestGameName.value)) {
    suggestGameName.value = ''
  }
}

function onVote(s: Suggestion, direction: VoteDirection) {
  if (!myPersonId.value) { showUserPicker.value = true; return }
  const newDir: VoteDirection = myVote(s) === direction ? '' : direction
  void voteOnSuggestion(s.id, myPersonId.value, newDir)
}
</script>

<style scoped>
/* ── Transitions ─────────────────────────────────────────────────────────── */
.fade-fast-enter-active,
.fade-fast-leave-active { transition: opacity 0.15s ease; }
.fade-fast-enter-from,
.fade-fast-leave-to { opacity: 0; }

.fade-bg-enter-active,
.fade-bg-leave-active { transition: opacity 0.2s ease; }
.fade-bg-enter-from,
.fade-bg-leave-to { opacity: 0; }

.slide-up-enter-active { transition: transform 0.25s ease-out; }
.slide-up-leave-active { transition: transform 0.25s ease-in; }
.slide-up-enter-from,
.slide-up-leave-to { transform: translateY(100%); }

/* ── App layout ──────────────────────────────────────────────────────────── */
.app {
  max-width: 540px;
  margin: 0 auto;
  position: relative;
  min-height: 100vh;
}
.content {
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.content-spacer { height: 48px; }

/* ── Backdrop (closes identity dropdown) ─────────────────────────────────── */
.backdrop-clear {
  position: fixed;
  inset: 0;
  z-index: 19;
}

/* ── Identity dropdown ───────────────────────────────────────────────────── */
.user-dropdown {
  position: fixed;
  top: 66px;
  right: 16px;
  z-index: 20;
  width: 200px;
  background: #1c1c2e;
  border: 1px solid #3d3490;
  border-radius: 12px;
  overflow: hidden;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
}
.user-option {
  display: block;
  width: 100%;
  text-align: left;
  padding: 11px 16px;
  font-size: 14px;
  font-weight: 500;
  color: #c8c8e8;
  background: none;
  border: none;
  border-bottom: 1px solid #252540;
  transition: background 0.12s, color 0.12s;
}
.user-option:last-child { border-bottom: none; }
.user-option:hover { background: #261d6a; color: #e8e8f4; }
.user-option--active { color: #c4b5fd; font-weight: 600; }
.user-option--clear { color: #7a7a9a; font-size: 13px; }

/* ── Header ──────────────────────────────────────────────────────────────── */
.hdr {
  position: sticky;
  top: 0;
  z-index: 10;
  background: #12121e;
  border-bottom: 1px solid #1c1c30;
}
.hdr-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px 8px;
}
.hdr-brand {
  font-size: 16px;
  font-weight: 700;
  color: #e8e8f4;
  letter-spacing: -0.01em;
}
.hdr-right {
  display: flex;
  align-items: center;
  gap: 8px;
}
.btn-id-pill {
  background: #261d6a;
  border: 1px solid #6254c8;
  border-radius: 999px;
  color: #c4b5fd;
  font-size: 12px;
  font-weight: 600;
  padding: 4px 12px;
  white-space: nowrap;
  transition: background 0.12s;
}
.btn-id-pill:hover { background: #3d3490; }
.btn-gear {
  background: #1c1c2e;
  border: 1px solid #2a2a48;
  border-radius: 8px;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  color: #7a7a9a;
  transition: background 0.12s, color 0.12s;
}
.btn-gear:hover { background: #252540; color: #c4b5fd; }

/* ── Tab bar ─────────────────────────────────────────────────────────────── */
.tab-bar {
  display: flex;
  background: #14142a;
  padding: 0 12px;
  border-top: 1px solid #1c1c30;
}
.tab {
  flex: 1;
  padding: 9px 4px;
  font-size: 13px;
  font-weight: 600;
  color: #5a5a7a;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  background: none;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  transition: color 0.12s, border-color 0.12s;
}
.tab:hover { color: #8b8ba8; }
.tab--active { color: #a78bfa; border-bottom-color: #6254c8; }
.tab-badge {
  background: #6254c8;
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 999px;
  line-height: 1.5;
}

/* ── Error bar ───────────────────────────────────────────────────────────── */
.err-bar {
  background: rgba(248, 113, 113, 0.1);
  border: 1px solid #f87171;
  border-radius: 8px;
  color: #f87171;
  padding: 10px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 14px;
}
.err-close {
  background: none;
  color: #f87171;
  font-size: 18px;
  padding: 0 4px;
  line-height: 1;
}

/* ── Loading ─────────────────────────────────────────────────────────────── */
.loading-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 48px 16px;
  color: #5a5a7a;
  font-size: 14px;
}
.spinner {
  width: 28px;
  height: 28px;
  border: 2.5px solid #252540;
  border-top-color: #6254c8;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* ── Empty state ─────────────────────────────────────────────────────────── */
.empty-wrap {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 48px 16px;
  text-align: center;
  color: #5a5a7a;
  font-size: 14px;
}
.empty-icon { font-size: 40px; }

/* ── Picker card ─────────────────────────────────────────────────────────── */
.picker-card {
  border-radius: 16px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.picker-card--pre {
  background: #181530;
  border: 1.5px solid #5a4fba;
}
.picker-card--pending {
  background: #0d1f14;
  border: 1.5px solid #22c55e;
}
.pc-label {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #a78bfa;
}
.pc-label--green { color: #4ade80; }
.pc-name {
  font-size: 32px;
  font-weight: 800;
  color: #e8e8f4;
  letter-spacing: -0.02em;
  line-height: 1.1;
}
.pc-name-sm {
  font-size: 18px;
  font-weight: 700;
  color: #e8e8f4;
}
.pc-game-display {
  font-size: 22px;
  font-weight: 700;
  color: #4ade80;
  letter-spacing: -0.01em;
}
.pc-input {
  background: #0d0d1e;
  border: 1px solid #2a2456;
  border-radius: 10px;
  color: #e8e8f4;
  padding: 12px 14px;
  font-size: 16px;
  font-family: inherit;
  width: 100%;
  transition: border-color 0.15s;
}
.pc-input:focus { border-color: #6254c8; }
.pc-input::placeholder { color: #3a3a58; }
.pc-input--green {
  background: #0d2018;
  border-color: #154025;
  color: #4ade80;
}
.pc-input--green:focus { border-color: #22c55e; }
.pc-input--green::placeholder { color: #1e3828; }
.pc-btns {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.btn-pick {
  padding: 12px;
  background: #3d3490;
  border: 1px solid #6254c8;
  border-radius: 10px;
  color: #e8e8f4;
  font-size: 14px;
  font-weight: 600;
  transition: background 0.12s;
}
.btn-pick:not(:disabled):hover { background: #4a3faa; }
.btn-pick:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-skip {
  padding: 12px;
  background: #141428;
  border: 1px solid #252544;
  border-radius: 10px;
  color: #7a7a9a;
  font-size: 14px;
  font-weight: 600;
  transition: background 0.12s;
}
.btn-skip:not(:disabled):hover { background: #1c1c38; }
.btn-skip:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-done {
  padding: 12px;
  background: #166834;
  border: 1px solid #22c55e;
  border-radius: 10px;
  color: #dcfce7;
  font-size: 14px;
  font-weight: 600;
  width: 100%;
  transition: background 0.12s;
}
.btn-done:not(:disabled):hover { background: #1a7a3e; }
.btn-done:disabled { opacity: 0.4; cursor: not-allowed; }

/* ── Next session strip ──────────────────────────────────────────────────── */
.session-strip {
  background: #1c1c2e;
  border: 1px solid #252540;
  border-radius: 10px;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.ss-left { display: flex; align-items: center; gap: 10px; }
.ss-icon { font-size: 18px; flex-shrink: 0; }
.ss-label {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #5a5a7a;
}
.ss-date { font-size: 14px; font-weight: 600; color: #c8c8e8; }
.ss-right { font-size: 13px; color: #7a7a9a; flex-shrink: 0; }
.ss-going { color: #4ade80; font-weight: 700; font-size: 16px; }
.ss-going-label { margin-left: 2px; }

/* ── Section ─────────────────────────────────────────────────────────────── */
.section { display: flex; flex-direction: column; gap: 6px; }
.section-hdr {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #5a5a7a;
}

/* ── Queue ───────────────────────────────────────────────────────────────── */
.queue-list { display: flex; flex-direction: column; gap: 4px; }
.q-row {
  display: flex;
  align-items: center;
  gap: 12px;
  background: #1c1c2e;
  border: 1px solid #252540;
  border-radius: 10px;
  padding: 12px 14px;
}
.q-row--first { border-color: #3d3490; }
.q-pos {
  width: 22px;
  text-align: center;
  flex-shrink: 0;
  font-size: 14px;
}
.q-num { color: #5a5a7a; font-size: 13px; font-weight: 600; }
.q-name { flex: 1; font-size: 15px; font-weight: 500; color: #c8c8e8; min-width: 0; }
.q-name--bold { font-weight: 700; color: #e8e8f4; }
.btn-att {
  background: #1c1c30;
  border: 1px solid #3a3a58;
  border-radius: 999px;
  color: #5a5a7a;
  font-size: 12px;
  font-weight: 600;
  padding: 4px 10px;
  white-space: nowrap;
  flex-shrink: 0;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.btn-att:hover { border-color: #22c55e; color: #4ade80; }
.btn-att--yes { background: #0a2010; border-color: #22c55e; color: #4ade80; }
.btn-att--no { background: #2a0a0a; border-color: #f87171; color: #f87171; }

/* ── History ─────────────────────────────────────────────────────────────── */
.history-list { display: flex; flex-direction: column; gap: 3px; }
.h-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: #1c1c2e;
  border: 1px solid #252540;
  border-radius: 8px;
  font-size: 13px;
}
.h-person { font-weight: 600; color: #c8c8e8; min-width: 72px; flex-shrink: 0; }
.h-game { flex: 1; color: #7a7a9a; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.h-game--skip { font-style: italic; }
.h-date { color: #5a5a7a; font-size: 12px; flex-shrink: 0; }

/* ── Suggestions tab ─────────────────────────────────────────────────────── */
.sugg-add { display: flex; gap: 8px; }
.sugg-input {
  flex: 1;
  background: #1c1c2e;
  border: 1px solid #252540;
  border-radius: 10px;
  color: #e8e8f4;
  padding: 11px 14px;
  font-size: 14px;
  transition: border-color 0.15s;
}
.sugg-input:focus { border-color: #6254c8; }
.sugg-input::placeholder { color: #3a3a58; }
.btn-sugg-add {
  background: #3d3490;
  border: 1px solid #6254c8;
  border-radius: 10px;
  color: #e8e8f4;
  font-size: 14px;
  font-weight: 600;
  padding: 0 18px;
  white-space: nowrap;
  transition: background 0.12s;
}
.btn-sugg-add:not(:disabled):hover { background: #4a3faa; }
.btn-sugg-add:disabled { opacity: 0.4; cursor: not-allowed; }

.sugg-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 40px 16px;
  text-align: center;
  color: #3e3e5a;
  font-size: 14px;
}
.sugg-empty-icon { font-size: 36px; }

.sugg-list { display: flex; flex-direction: column; gap: 4px; }
.sugg-row {
  display: flex;
  align-items: center;
  gap: 10px;
  background: #1c1c2e;
  border: 1px solid #252540;
  border-radius: 10px;
  padding: 12px 14px;
}
.sugg-info { flex: 1; min-width: 0; }
.sugg-game { font-size: 14px; font-weight: 600; color: #e8e8f4; }
.sugg-by { font-size: 12px; color: #7a7a9a; }
.sugg-score {
  font-size: 13px;
  font-weight: 700;
  min-width: 28px;
  text-align: center;
  flex-shrink: 0;
  color: #5a5a7a;
}
.sugg-score--pos { color: #4ade80; }
.sugg-score--neg { color: #f87171; }

/* ── Vote pill (3-segment capsule) ───────────────────────────────────────── */
.vote-pill {
  display: flex;
  border: 1.5px solid #3d3490;
  border-radius: 999px;
  overflow: hidden;
  flex-shrink: 0;
  transition: border-color 0.12s;
}
.vote-pill--up { border-color: #22c55e; }
.vote-pill--down { border-color: #f87171; }
.vp-seg {
  background: none;
  border: none;
  font-size: 13px;
  padding: 5px 9px;
  color: #5a5a7a;
  transition: background 0.12s, color 0.12s;
  line-height: 1;
}
.vp-up:hover { background: rgba(74, 222, 128, 0.15); }
.vp-up--on { background: rgba(34, 197, 94, 0.2); color: #4ade80; }
.vp-down:hover { background: rgba(248, 113, 113, 0.15); }
.vp-down--on { background: rgba(248, 113, 113, 0.2); color: #f87171; }
.vp-x {
  border-left: 1px solid #252540;
  width: 28px;
  padding: 5px 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #5a5a7a;
  font-size: 16px;
  line-height: 1;
}
.vp-x:hover { background: rgba(248, 113, 113, 0.15); color: #f87171; }

/* ── Settings bottom sheet ───────────────────────────────────────────────── */
.sheet-bg {
  position: fixed;
  inset: 0;
  z-index: 40;
  background: rgba(0, 0, 0, 0.5);
}
.sheet {
  position: fixed;
  bottom: 0;
  left: 0;
  right: 0;
  max-width: 540px;
  margin: 0 auto;
  z-index: 41;
  background: #1a1a2e;
  border-radius: 20px 20px 0 0;
  padding: 12px 20px 32px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 85vh;
  overflow-y: auto;
}
.sheet-handle {
  width: 32px;
  height: 3px;
  background: #3a3a58;
  border-radius: 2px;
  margin: 0 auto 6px;
  flex-shrink: 0;
}
.sheet-section-label {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #5a5a7a;
}
.sheet-input-row { display: flex; gap: 8px; }
.sheet-input {
  flex: 1;
  background: #12121e;
  border: 1px solid #252540;
  border-radius: 8px;
  color: #e8e8f4;
  padding: 10px 12px;
  font-size: 14px;
  transition: border-color 0.15s;
}
.sheet-input:focus { border-color: #6254c8; }
.sheet-input::placeholder { color: #3a3a58; }
.sheet-date-input { color-scheme: dark; }
.btn-sheet-add {
  background: #3d3490;
  border: 1px solid #6254c8;
  border-radius: 8px;
  color: #e8e8f4;
  font-size: 14px;
  font-weight: 600;
  padding: 0 14px;
  white-space: nowrap;
  transition: background 0.12s;
}
.btn-sheet-add:not(:disabled):hover { background: #4a3faa; }
.btn-sheet-add:disabled { opacity: 0.4; cursor: not-allowed; }
.sheet-hint { font-size: 12px; color: #5a5a7a; }

.sheet-players { display: flex; flex-direction: column; }
.sheet-player-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid #1e1e32;
}
.sheet-player-row:last-child { border-bottom: none; }
.sheet-player-name { font-size: 14px; color: #c8c8e8; font-weight: 500; }
.btn-sheet-remove {
  background: none;
  border: none;
  color: #5a5a7a;
  font-size: 18px;
  padding: 0 4px;
  line-height: 1;
  transition: color 0.12s;
}
.btn-sheet-remove:hover { color: #f87171; }

.sheet-divider { height: 1px; background: #1e1e32; margin: 4px 0; }
.sheet-date-hdr {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: #7a7a9a;
}

.btn-reset {
  width: 100%;
  padding: 12px;
  background: rgba(248, 113, 113, 0.1);
  border: 1px solid #f87171;
  border-radius: 10px;
  color: #f87171;
  font-size: 14px;
  font-weight: 600;
  transition: background 0.12s;
}
.btn-reset:not(:disabled):hover { background: rgba(248, 113, 113, 0.2); }
.btn-reset:disabled { opacity: 0.4; cursor: not-allowed; }
.sheet-reset-hint { font-size: 12px; color: #5a5a7a; text-align: center; }
</style>
