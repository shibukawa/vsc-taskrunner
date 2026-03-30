<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{
  authRequired: boolean
  loginPath: string
  authenticated: boolean
  canManageTokens: boolean
  isAdmin: boolean
  currentUser: string
  userName: string
  userEmail: string
  backgroundPaused: boolean
}>()

const emit = defineEmits<{
  logout: []
  'open-token-manager': []
  'open-settings': []
  'update:backgroundPaused': [value: boolean]
}>()

const userMenuOpen = ref(false)
const settingsMenuOpen = ref(false)
const userMenuRef = ref<HTMLElement | null>(null)
const settingsMenuRef = ref<HTMLElement | null>(null)

function userInitial(value?: string): string {
  const raw = (value ?? '').trim()
  if (!raw) {
    return 'U'
  }
  return raw[0].toUpperCase()
}

const userButtonLabel = computed(() => props.userName || props.userEmail || props.currentUser || 'authenticated user')

function toggleUserMenu() {
  userMenuOpen.value = !userMenuOpen.value
  if (userMenuOpen.value) {
    settingsMenuOpen.value = false
  }
}

function toggleSettingsMenu() {
  settingsMenuOpen.value = !settingsMenuOpen.value
  if (settingsMenuOpen.value) {
    userMenuOpen.value = false
  }
}

function closeMenus() {
  userMenuOpen.value = false
  settingsMenuOpen.value = false
}

function handleDocumentClick(event: MouseEvent) {
  const target = event.target as Node | null
  if (target && userMenuRef.value?.contains(target)) {
    return
  }
  if (target && settingsMenuRef.value?.contains(target)) {
    return
  }
  closeMenus()
}

function handleDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    closeMenus()
  }
}

function toggleBackgroundPaused() {
  emit('update:backgroundPaused', !props.backgroundPaused)
}

onMounted(() => {
  document.addEventListener('click', handleDocumentClick)
  document.addEventListener('keydown', handleDocumentKeydown)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleDocumentClick)
  document.removeEventListener('keydown', handleDocumentKeydown)
})
</script>

<template>
  <header class="topbar glass-panel">
    <div class="topbar-brand">
      <strong class="system-name">VSCode Task Runner WebUI</strong>
    </div>

    <div class="topbar-actions">
      <template v-if="authRequired">
        <a :href="loginPath" class="icon-button login-chip" aria-label="Login" title="Login">
          <span aria-hidden="true">⎋</span>
        </a>
      </template>
      <template v-else-if="authenticated">
        <div ref="userMenuRef" class="menu-anchor">
          <button
            class="icon-button user-button"
            type="button"
            :title="userButtonLabel"
            :aria-label="userButtonLabel"
            :aria-expanded="userMenuOpen"
            aria-haspopup="dialog"
            @click.stop="toggleUserMenu"
          >
            <span class="chip-avatar">{{ userInitial(userName || userEmail || currentUser) }}</span>
          </button>

          <div v-if="userMenuOpen" class="topbar-popover" role="dialog" aria-label="User details">
            <div class="menu-card">
              <span class="menu-label">Name</span>
              <strong class="menu-value">{{ userName || 'Not available' }}</strong>
            </div>
            <div class="menu-card">
              <span class="menu-label">Email</span>
              <strong class="menu-value">{{ userEmail || currentUser || 'Not available' }}</strong>
            </div>
            <button v-if="canManageTokens" class="menu-action" type="button" @click="emit('open-token-manager')">API Tokens</button>
            <button class="menu-action danger" type="button" @click="emit('logout')">Logout</button>
          </div>
        </div>
      </template>
      <template v-else>
        <button class="icon-button user-button" type="button" aria-label="NoAuth mode" title="NoAuth mode">
          <span class="chip-avatar">N</span>
        </button>
      </template>
      <div ref="settingsMenuRef" class="menu-anchor">
        <button
          class="icon-button settings-button"
          type="button"
          aria-label="Settings"
          title="Settings"
          :aria-expanded="settingsMenuOpen"
          aria-haspopup="dialog"
          @click.stop="toggleSettingsMenu"
        >
          <span aria-hidden="true">⚙</span>
        </button>

        <div v-if="settingsMenuOpen" class="topbar-popover settings-popover" role="dialog" aria-label="Settings">
          <label class="toggle-row">
            <span class="toggle-copy">
              <strong>背景アニメーション</strong>
              <small>{{ backgroundPaused ? '停止中' : '再生中' }}</small>
            </span>
            <button
              class="toggle-switch"
              type="button"
              role="switch"
              :aria-checked="!backgroundPaused"
              :class="{ active: !backgroundPaused }"
              @click="toggleBackgroundPaused"
            >
              <span class="toggle-knob"></span>
            </button>
          </label>
          <button v-if="isAdmin" class="menu-action" type="button" @click="emit('open-settings')">設定詳細</button>
        </div>
      </div>
    </div>
  </header>
</template>

<style>
.topbar {
  grid-area: header;
  position: relative;
  z-index: 50;
  isolation: isolate;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 6px 12px;
  min-width: 0;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.15), rgba(255, 255, 255, 0.05)),
    rgba(255, 255, 255, 0.08);
  border-radius: var(--panel-radius);
}

.topbar-brand {
  flex: 0 1 auto;
}

.system-name {
  display: block;
  font-size: 1rem;
  line-height: 1.1;
}

.topbar-actions {
  display: flex;
  justify-content: flex-end;
  flex: 0 0 auto;
  gap: 6px;
}

.menu-anchor {
  position: relative;
}

.icon-chip,
.icon-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.26);
  background: rgba(255, 255, 255, 0.16);
  min-height: 40px;
}

.icon-chip {
  padding: 0.35rem 0.45rem;
  color: var(--ink);
  text-decoration: none;
}

.icon-button {
  width: 32px;
  min-height: 32px;
  padding: 0;
}

.chip-avatar {
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.42);
  color: var(--ink);
  font-size: 0.85rem;
  font-weight: 700;
}

.settings-button span {
  font-size: 1rem;
  line-height: 1;
}

.topbar-popover {
  position: absolute;
  top: calc(100% + 10px);
  right: 0;
  min-width: 220px;
  display: grid;
  gap: 10px;
  padding: 14px;
  border-radius: 18px;
  border: 1px solid rgba(255, 255, 255, 0.52);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.9), rgba(242, 247, 248, 0.84)),
    rgba(255, 255, 255, 0.82);
  box-shadow: 0 18px 38px rgba(17, 24, 39, 0.18);
  color: var(--ink);
  z-index: 30;
}

.settings-popover {
  min-width: 260px;
}

.menu-card {
  display: grid;
  gap: 4px;
}

.menu-label {
  font-size: 0.72rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
}

.menu-value {
  font-size: 0.96rem;
  line-height: 1.3;
  word-break: break-word;
}

.menu-action {
  min-height: 38px;
  border: 0;
  border-radius: 12px;
  padding: 0.65rem 0.9rem;
  font: inherit;
  font-weight: 700;
  cursor: pointer;
  color: #fff;
  background: linear-gradient(135deg, #0d7a69, #158f7c);
}

.menu-action.danger {
  background: linear-gradient(135deg, #be4c5b, #913142);
}

.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}

.toggle-copy {
  display: grid;
  gap: 2px;
}

.toggle-copy strong {
  font-size: 0.92rem;
  line-height: 1.2;
}

.toggle-copy small {
  color: var(--muted);
}

.toggle-switch {
  position: relative;
  width: 52px;
  height: 30px;
  padding: 3px;
  border: 0;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.16);
  cursor: pointer;
  transition: background 160ms ease;
}

.toggle-switch.active {
  background: linear-gradient(135deg, #0d7a69, #158f7c);
}

.toggle-knob {
  display: block;
  width: 24px;
  height: 24px;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.18);
  transition: transform 160ms ease;
}

.toggle-switch.active .toggle-knob {
  transform: translateX(22px);
}

@media (max-width: 720px) {
  .topbar {
    padding: 18px;
    border-radius: var(--panel-radius);
    flex-direction: row;
    align-items: center;
  }

  .topbar-popover {
    right: -8px;
    min-width: min(280px, calc(100vw - 32px));
  }
}
</style>
