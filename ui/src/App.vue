<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type {
  Branch, BranchTask, TaskInput, TaskRun, ArtifactItem, RunMeta,
  RunStartResponse, MeResponse, RouteState, SSETaskEvent,
  MetricsSnapshot, APITokenItem, APITokenCreateResponse, SettingsSummary, RuntimeMode,
} from './types'
import { parseAnsiToSegments } from './ansi'
import BackgroundEffect from './components/BackgroundEffect.vue'
import AppHeader from './components/AppHeader.vue'
import MetricsPanel from './components/MetricsPanel.vue'
import TaskSelectionPanel from './components/TaskSelectionPanel.vue'
import ResultPanel from './components/ResultPanel.vue'

const branches = ref<Branch[]>([])
const tasks = ref<BranchTask[]>([])
const runs = ref<RunMeta[]>([])
const artifacts = ref<ArtifactItem[]>([])
const metrics = ref<MetricsSnapshot | null>(null)
const selectedBranch = ref<string | null>(null)
const selectedTask = ref<string | null>(null)
const selectedRunNumber = ref<number | null>(null)
const selectedRunId = ref<string | null>(null)
const selectedChildTask = ref<string | null>(null)
const selectedRunDetail = ref<RunMeta | null>(null)
const taskLogs = ref<Record<string, string>>({})
const inputValues = ref<Record<string, string>>({})
const dialogInputValues = ref<Record<string, string>>({})
const pendingRunBranch = ref<string | null>(null)
const pendingRunTaskLabel = ref<string | null>(null)
const pendingRunTaskDef = ref<BranchTask | null>(null)
const runDialogOpen = ref(false)
const loading = ref(false)
const errorMessage = ref('')
const authRequired = ref(false)
const loginPath = ref('/auth/login')
const authenticated = ref(false)
const currentUser = ref('')
const userName = ref('')
const userEmail = ref('')
const canRun = ref(true)
const isAdmin = ref(false)
const canManageTokens = ref(false)
const apiTokensEnabled = ref(false)
const runtimeMode = ref<RuntimeMode>('always-on')
const routeNotFound = ref(false)
const backgroundPaused = ref(false)
const tokenManagerOpen = ref(false)
const settingsDialogOpen = ref(false)
const tokenItems = ref<APITokenItem[]>([])
const settingsSummary = ref<SettingsSummary | null>(null)
const tokenLabel = ref('')
const tokenTTLDays = ref(30)
const tokenScopeRead = ref(true)
const tokenScopeWrite = ref(true)
const createdTokenValue = ref('')
const createdTokenItemId = ref('')
const createdTokenScopes = ref<string[]>([])
const tokenLoading = ref(false)
const tokenError = ref('')
const settingsLoading = ref(false)
const settingsError = ref('')
const tokenExampleBranch = ref<string | null>(null)
const tokenExampleTask = ref<string | null>(null)
const tokenExampleTasks = ref<BranchTask[]>([])
const copiedToken = ref(false)
const copiedCurlTarget = ref<'me' | 'runs' | 'run-post' | null>(null)
let copiedTokenTimer: number | null = null
let copiedCurlTimer: number | null = null

let source: EventSource | null = null
let metricsSource: EventSource | null = null
let globalRunSource: EventSource | null = null
let isApplyingRoute = false
let runsPollTimer: number | null = null
const runsETag = ref<string | null>(null)

const selectedTaskDef = computed(() => tasks.value.find((task) => task.label === selectedTask.value) ?? null)
const currentRunTaskDef = computed(() => {
  if (!currentRun.value || !selectedBranch.value) {
    return null
  }
  if (currentRun.value.branch !== selectedBranch.value) {
    return null
  }
  return tasks.value.find((task) => task.label === currentRun.value?.taskLabel) ?? null
})
const filteredRuns = computed(() => {
  return runs.value.filter((run) => {
    if (selectedBranch.value && run.branch !== selectedBranch.value) {
      return false
    }
    if (selectedTask.value && run.taskLabel !== selectedTask.value) {
      return false
    }
    return true
  })
})
const lastRunByTask = computed<Record<string, RunMeta>>(() => {
  if (!selectedBranch.value) {
    return {}
  }

  return runs.value.reduce<Record<string, RunMeta>>((latest, run) => {
    if (run.branch !== selectedBranch.value) {
      return latest
    }

    const current = latest[run.taskLabel]
    if (!current) {
      latest[run.taskLabel] = run
      return latest
    }

    const currentTime = new Date(current.startTime).getTime()
    const runTime = new Date(run.startTime).getTime()
    if (Number.isNaN(currentTime) || (!Number.isNaN(runTime) && runTime > currentTime)) {
      latest[run.taskLabel] = run
    }
    return latest
  }, {})
})
const currentRun = computed(() => {
  if (selectedRunDetail.value) {
    return selectedRunDetail.value
  }
  if (selectedRunId.value) {
    return runs.value.find((run) => run.runId === selectedRunId.value) ?? null
  }
  if (selectedRunNumber.value !== null && selectedBranch.value && selectedTask.value) {
    return runs.value.find((run) =>
      run.branch === selectedBranch.value &&
      run.taskLabel === selectedTask.value &&
      run.runNumber === selectedRunNumber.value,
    ) ?? null
  }
  return null
})
const runTasks = computed(() => currentRun.value?.tasks ?? [])
const currentLog = computed(() => (selectedChildTask.value ? (taskLogs.value[selectedChildTask.value] ?? '') : ''))
const currentLogSegments = computed(() => parseAnsiToSegments(currentLog.value))
const selectedRunTaskStatus = computed(() => {
  if (!selectedChildTask.value) {
    return 'pending'
  }
  return runTasks.value.find((task) => task.label === selectedChildTask.value)?.status ?? 'pending'
})
const selectedTokenScopeValues = computed(() => selectedTokenScopes())
const createTokenDisabled = computed(() => (
  tokenLoading.value ||
  tokenLabel.value.trim() === '' ||
  tokenTTLDays.value < 1 ||
  selectedTokenScopeValues.value.length === 0
))
const createdTokenHasReadScope = computed(() => createdTokenScopes.value.includes('runs:read'))
const createdTokenHasWriteScope = computed(() => createdTokenScopes.value.includes('runs:write'))

function parseRoute(pathname: string): RouteState {
  const parts = pathname.split('/').filter(Boolean).map((part) => decodeURIComponent(part))
  const state: RouteState = { branch: '', task: '', runNumber: '', runId: '', childTask: '' }
  if (parts[0] === 'runs') {
    state.runId = parts[1] ?? ''
    if (parts[2] === 'logs') {
      state.childTask = parts[3] ?? ''
    }
    return state
  }
  if (parts[0] !== 'branches') {
    return state
  }
  state.branch = parts[1] ?? ''
  if (parts[2] === 'runs') {
    state.runId = parts[3] ?? ''
    if (parts[4] === 'logs') {
      state.childTask = parts[5] ?? ''
    }
    return state
  }
  if (parts[2] !== 'tasks') {
    return state
  }
  state.task = parts[3] ?? ''
  if (parts[4] !== 'runs') {
    return state
  }
  state.runNumber = parts[5] ?? ''
  if (parts[6] === 'logs') {
    state.childTask = parts[7] ?? ''
  }
  return state
}

function buildRoute(state: RouteState): string {
  if (state.branch && state.task && state.runNumber) {
    let path = `/branches/${encodeURIComponent(state.branch)}/tasks/${encodeURIComponent(state.task)}/runs/${encodeURIComponent(state.runNumber)}`
    if (!state.childTask) {
      return path
    }
    path += `/logs/${encodeURIComponent(state.childTask)}`
    return path
  }
  if (state.runId) {
    if (state.branch && !state.task) {
      const path = `/branches/${encodeURIComponent(state.branch)}/runs/${encodeURIComponent(state.runId)}`
      return state.childTask ? `${path}/logs/${encodeURIComponent(state.childTask)}` : path
    }
    const path = `/runs/${encodeURIComponent(state.runId)}`
    return state.childTask ? `${path}/logs/${encodeURIComponent(state.childTask)}` : path
  }
  if (!state.branch) {
    return '/'
  }
  let path = `/branches/${encodeURIComponent(state.branch)}`
  if (!state.task) {
    return path
  }
  path += `/tasks/${encodeURIComponent(state.task)}`
  if (!state.childTask) {
    return path
  }
  return `${path}/logs/${encodeURIComponent(state.childTask)}`
}

function buildRunAPIPath(runId: string): string {
  return `/api/runs/${encodeURIComponent(runId)}`
}

function replaceRoute() {
  if (isApplyingRoute) {
    return
  }
  const path = buildRoute({
    branch: selectedBranch.value ?? '',
    task: selectedTask.value ?? '',
    runNumber: selectedRunNumber.value === null ? '' : String(selectedRunNumber.value),
    runId: selectedRunId.value ?? '',
    childTask: selectedChildTask.value ?? '',
  })
  if (window.location.pathname !== path) {
    window.history.replaceState({}, '', path)
  }
}

async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method ?? 'GET'
  const response = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })
  if (!response.ok) {
    const body = await response.text()
    let details = body
    try {
      const parsed = JSON.parse(body)
      if (parsed?.loginPath && typeof parsed.loginPath === 'string') {
        loginPath.value = parsed.loginPath
      }
      details = parsed?.error ? String(parsed.error) : body
    } catch {
      // Ignore JSON parse error.
    }
    const message = details || `${response.status} ${response.statusText}`
    console.error('API request failed', {
      path,
      method,
      status: response.status,
      statusText: response.statusText,
      body,
      error: message,
    })
    throw new Error(`${response.status}:${message}`)
  }
  return (await response.json()) as T
}

function asErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function isUnauthorizedMessage(message: string): boolean {
  return message.startsWith('401:')
}

async function loadMe() {
  authRequired.value = false
  authenticated.value = false
  currentUser.value = ''
  userName.value = ''
  userEmail.value = ''
  canRun.value = true
  isAdmin.value = false
  canManageTokens.value = false
  apiTokensEnabled.value = false
  runtimeMode.value = 'always-on'

  try {
    const me = await api<MeResponse>('/api/me')
    authenticated.value = me.authenticated
    currentUser.value = me.subject ?? ''
    userName.value = me.claims?.name ?? me.claims?.preferred_username ?? me.subject ?? ''
    userEmail.value = me.claims?.email ?? me.subject ?? ''
    canRun.value = me.canRun ?? true
    isAdmin.value = me.isAdmin ?? false
    canManageTokens.value = me.canManageTokens ?? false
    apiTokensEnabled.value = me.apiTokensEnabled ?? false
    runtimeMode.value = me.runtimeMode ?? 'always-on'
  } catch (error) {
    const message = asErrorMessage(error)
    if (isUnauthorizedMessage(message)) {
      authRequired.value = true
      return
    }
    errorMessage.value = message
  }
}

function statusLabel(value: boolean, positive = 'Enabled', negative = 'Disabled'): string {
  return value ? positive : negative
}

function storeSummaryLabel(store: SettingsSummary['auth']['apiTokenStore'] | SettingsSummary['storage']['object']): string {
  if (!store.backend) {
    return 'Not configured'
  }
  return store.backend
}

function loadBackgroundPreference() {
  if (typeof window === 'undefined') {
    return
  }
  backgroundPaused.value = window.localStorage.getItem('background-paused') === 'true'
}

watch(backgroundPaused, (value) => {
  if (typeof window === 'undefined') {
    return
  }
  window.localStorage.setItem('background-paused', String(value))
})

function logout() {
  window.location.href = '/auth/logout'
}

function selectedTokenScopes(): string[] {
  const scopes: string[] = []
  if (tokenScopeRead.value) {
    scopes.push('runs:read')
  }
  if (tokenScopeWrite.value) {
    scopes.push('runs:write')
  }
  return scopes
}

function currentOrigin(): string {
  if (typeof window === 'undefined') {
    return ''
  }
  return window.location.origin
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\"'\"'`)}'`
}

function apiTokenBaseCurl(): string {
  return `curl -H ${shellQuote(`Authorization: Bearer ${createdTokenValue.value}`)}`
}

function tokenExampleBranchOptions(): Branch[] {
  return branches.value
}

async function loadBranchTasks(branch: string | null): Promise<BranchTask[]> {
  if (!branch) {
    return []
  }
  const cachedBranch = branches.value.find((item) => item.shortName === branch) ?? null
  if (cachedBranch?.tasks) {
    return cachedBranch.tasks
  }
  const data = await api<unknown>(`/api/git/branches/${encodeURIComponent(branch)}/tasks`)
  const nextTasks = Array.isArray(data) ? (data as BranchTask[]) : []
  branches.value = branches.value.map((item) => (
    item.shortName === branch ? { ...item, tasks: nextTasks } : item
  ))
  return nextTasks
}

async function loadTokenExampleTasks(branch: string | null) {
  tokenExampleTasks.value = await loadBranchTasks(branch)
  if (!tokenExampleTasks.value.some((task) => task.label === tokenExampleTask.value)) {
    tokenExampleTask.value = tokenExampleTasks.value[0]?.label ?? null
  }
}

async function initializeTokenExampleSelection() {
  const branchOptions = tokenExampleBranchOptions()
  const nextBranch = (
    (selectedBranch.value && branchOptions.some((branch) => branch.shortName === selectedBranch.value) && selectedBranch.value) ||
    branchOptions[0]?.shortName ||
    null
  )
  tokenExampleBranch.value = nextBranch
  await loadTokenExampleTasks(nextBranch)
  if (selectedTask.value && tokenExampleTasks.value.some((task) => task.label === selectedTask.value)) {
    tokenExampleTask.value = selectedTask.value
    return
  }
  tokenExampleTask.value = tokenExampleTasks.value[0]?.label ?? null
}

async function onTokenExampleBranchChange(branch: string) {
  tokenExampleBranch.value = branch || null
  tokenExampleTask.value = null
  await loadTokenExampleTasks(tokenExampleBranch.value)
}

function postRunExampleBody(): Record<string, unknown> {
  return {
    branch: tokenExampleBranch.value ?? '',
    taskLabel: tokenExampleTask.value ?? '',
    inputValues: {},
  }
}

function buildExampleCurl(kind: 'me' | 'runs' | 'run-post'): string {
  const origin = currentOrigin()
  const baseCurl = apiTokenBaseCurl()
  switch (kind) {
    case 'me':
      return `${baseCurl} ${shellQuote(`${origin}/api/me`)}`
    case 'runs':
      return `${baseCurl} ${shellQuote(`${origin}/api/runs`)}`
    case 'run-post':
      return `${baseCurl} -H 'Content-Type: application/json' -X POST ${shellQuote(`${origin}/api/runs`)} -d ${shellQuote(JSON.stringify(postRunExampleBody()))}`
  }
}

function markCopied(kind: 'token' | 'me' | 'runs' | 'run-post') {
  if (typeof window === 'undefined') {
    return
  }
  if (kind === 'token') {
    copiedToken.value = true
    if (copiedTokenTimer !== null) {
      window.clearTimeout(copiedTokenTimer)
    }
    copiedTokenTimer = window.setTimeout(() => {
      copiedToken.value = false
      copiedTokenTimer = null
    }, 1800)
    return
  }
  copiedCurlTarget.value = kind
  if (copiedCurlTimer !== null) {
    window.clearTimeout(copiedCurlTimer)
  }
  copiedCurlTimer = window.setTimeout(() => {
    copiedCurlTarget.value = null
    copiedCurlTimer = null
  }, 1800)
}

async function copyText(value: string, kind?: 'token' | 'me' | 'runs' | 'run-post') {
  if (typeof navigator === 'undefined' || !navigator.clipboard) {
    return
  }
  await navigator.clipboard.writeText(value)
  if (kind) {
    markCopied(kind)
  }
}

function buildOpenAPIYAML(): string {
  const origin = currentOrigin()
  const branchExample = tokenExampleBranch.value ?? ''
  const taskExample = tokenExampleTask.value ?? ''
  return [
    'openapi: 3.0.3',
    'info:',
    '  title: runtask Batch API',
    '  version: 1.0.0',
    'servers:',
    `  - url: ${JSON.stringify(origin)}`,
    'components:',
    '  securitySchemes:',
    '    bearerAuth:',
    '      type: http',
    '      scheme: bearer',
    '      bearerFormat: API token',
    'security:',
    '  - bearerAuth: []',
    'paths:',
    '  /api/me:',
    '    get:',
    '      summary: Get current subject and capabilities',
    '      responses:',
    "        '200':",
    '          description: OK',
    '  /api/runs:',
    '    get:',
    '      summary: List runs',
    '      responses:',
    "        '200':",
    '          description: OK',
    '    post:',
    '      summary: Start a run',
    '      requestBody:',
    '        required: true',
    '        content:',
    '          application/json:',
    '            schema:',
    '              type: object',
    '              properties:',
    '                branch:',
    '                  type: string',
    '                taskLabel:',
    '                  type: string',
    '                inputValues:',
    '                  type: object',
    '                  additionalProperties:',
    '                    type: string',
    '            example:',
    `              branch: ${JSON.stringify(branchExample)}`,
    `              taskLabel: ${JSON.stringify(taskExample)}`,
    '              inputValues: {}',
    '      responses:',
    "        '202':",
    '          description: Accepted',
    '  /api/runs/{runId}:',
    '    get:',
    '      summary: Get run details',
    '      parameters:',
    '        - name: runId',
    '          in: path',
    '          required: true',
    '          schema:',
    '            type: string',
    '      responses:',
    "        '200':",
    '          description: OK',
    '  /api/runs/{runId}/log:',
    '    get:',
    '      summary: Get combined log',
    '      parameters:',
    '        - name: runId',
    '          in: path',
    '          required: true',
    '          schema:',
    '            type: string',
    '      responses:',
    "        '200':",
    '          description: OK',
    '  /api/runs/{runId}/artifacts:',
    '    get:',
    '      summary: List artifacts',
    '      parameters:',
    '        - name: runId',
    '          in: path',
    '          required: true',
    '          schema:',
    '            type: string',
    '      responses:',
    "        '200':",
    '          description: OK',
    '  /api/runs/{runId}/worktree:',
    '    get:',
    '      summary: List worktree files',
    '      parameters:',
    '        - name: runId',
    '          in: path',
    '          required: true',
    '          schema:',
    '            type: string',
    '      responses:',
    "        '200':",
    '          description: OK',
    '',
  ].join('\n')
}

async function downloadOpenAPIYAML() {
  if (typeof document === 'undefined') {
    return
  }
  const blob = new Blob([buildOpenAPIYAML()], { type: 'application/yaml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'runtask-api.yaml'
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function closeTokenManager() {
  tokenManagerOpen.value = false
  tokenError.value = ''
  createdTokenValue.value = ''
  createdTokenItemId.value = ''
  createdTokenScopes.value = []
  copiedToken.value = false
  copiedCurlTarget.value = null
}

function closeSettingsDialog() {
  settingsDialogOpen.value = false
  settingsError.value = ''
}

async function loadSettings() {
  settingsSummary.value = await api<SettingsSummary>('/api/settings')
}

async function openSettingsDialog() {
  if (!isAdmin.value) {
    return
  }
  settingsDialogOpen.value = true
  settingsLoading.value = true
  settingsError.value = ''
  try {
    await loadSettings()
  } catch (error) {
    settingsError.value = asErrorMessage(error)
  } finally {
    settingsLoading.value = false
  }
}

async function loadAPITokens() {
  tokenItems.value = await api<APITokenItem[]>('/api/tokens')
}

async function openTokenManager() {
  if (!canManageTokens.value) {
    return
  }
  tokenManagerOpen.value = true
  tokenError.value = ''
  createdTokenValue.value = ''
  createdTokenItemId.value = ''
  createdTokenScopes.value = []
  tokenLabel.value = ''
  tokenTTLDays.value = 30
  tokenScopeRead.value = true
  tokenScopeWrite.value = true
  await initializeTokenExampleSelection()
  await loadAPITokens()
}

async function createAPIToken() {
  if (createTokenDisabled.value) {
    return
  }
  tokenLoading.value = true
  tokenError.value = ''
  try {
    const response = await api<APITokenCreateResponse>('/api/tokens', {
      method: 'POST',
      body: JSON.stringify({
        label: tokenLabel.value.trim(),
        ttlHours: tokenTTLDays.value * 24,
        scopes: selectedTokenScopeValues.value,
      }),
    })
    createdTokenValue.value = response.token
    createdTokenItemId.value = response.item.id
    createdTokenScopes.value = [...response.item.scopes]
    tokenLabel.value = ''
    tokenTTLDays.value = 30
    tokenScopeRead.value = true
    tokenScopeWrite.value = true
    await loadAPITokens()
  } catch (error) {
    tokenError.value = asErrorMessage(error)
  } finally {
    tokenLoading.value = false
  }
}

async function revokeAPIToken(tokenId: string) {
  tokenLoading.value = true
  tokenError.value = ''
  try {
    await api<{ status: string }>(`/api/tokens/${encodeURIComponent(tokenId)}/revoke`, {
      method: 'POST',
    })
    await loadAPITokens()
  } catch (error) {
    tokenError.value = asErrorMessage(error)
  } finally {
    tokenLoading.value = false
  }
}

async function copyCreatedToken() {
  if (!createdTokenValue.value) {
    return
  }
  await copyText(createdTokenValue.value, 'token')
}

function formatDateTime(value?: string): string {
  if (!value) {
    return 'Never'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

async function loadBranches() {
  const data = await api<unknown>('/api/git/branches')
  branches.value = Array.isArray(data) ? (data as Branch[]) : []
}

async function loadTasksForBranch(branch: string | null) {
  tasks.value = await loadBranchTasks(branch)
  const cachedBranch = branches.value.find((item) => item.shortName === branch) ?? null
  if (cachedBranch?.loadError) {
    errorMessage.value = cachedBranch.loadError
  }
}

async function loadRuns() {
  const headers: HeadersInit = {}
  if (runsETag.value) {
    headers['If-None-Match'] = runsETag.value
  }
  const response = await fetch('/api/runs', {
    method: 'GET',
    headers,
  })
  if (response.status === 304) {
    return
  }
  if (!response.ok) {
    const body = await response.text()
    throw new Error(`${response.status}:${body || response.statusText}`)
  }
  runsETag.value = response.headers.get('ETag')
  const data = await response.json()
  runs.value = Array.isArray(data) ? (data as RunMeta[]) : []
}

async function loadRunDetail(runId: string) {
  const data = await api<RunMeta>(buildRunAPIPath(runId))
  selectedRunDetail.value = data
}

async function loadArtifacts(runId: string) {
  const data = await api<ArtifactItem[]>(`${buildRunAPIPath(runId)}/artifacts`)
  artifacts.value = data
}

function stopLogStream() {
  if (source) {
    source.close()
    source = null
  }
}

function stopMetricsStream() {
  if (metricsSource) {
    metricsSource.close()
    metricsSource = null
  }
}

function stopGlobalRunStream() {
  if (globalRunSource) {
    globalRunSource.close()
    globalRunSource = null
  }
}

function sortRuns(items: RunMeta[]): RunMeta[] {
  return [...items].sort((left, right) => {
    const leftTime = new Date(left.startTime).getTime()
    const rightTime = new Date(right.startTime).getTime()
    if (leftTime === rightTime) {
      return right.runKey.localeCompare(left.runKey)
    }
    return rightTime - leftTime
  })
}

function upsertRunSummary(run: RunMeta) {
  const next = [...runs.value]
  const index = next.findIndex((item) => item.runId === run.runId)
  if (index >= 0) {
    next[index] = { ...next[index], ...run }
  } else {
    next.unshift(run)
  }
  runs.value = sortRuns(next)
}

function startRunsPolling() {
  stopRunsPolling()
  if (typeof window === 'undefined') {
    return
  }
  runsPollTimer = window.setInterval(() => {
    if (document.hidden) {
      return
    }
    void loadRuns().catch((error) => {
      console.error('Failed to refresh runs', error)
    })
  }, 15000)
}

function stopRunsPolling() {
  if (runsPollTimer !== null) {
    window.clearInterval(runsPollTimer)
    runsPollTimer = null
  }
}

function startGlobalRunStream() {
  stopGlobalRunStream()
  if (runtimeMode.value !== 'always-on') {
    return
  }
  globalRunSource = new EventSource('/api/runs/stream')
  const handleMessage = (raw: Event) => {
    const message = raw as MessageEvent
    const run = JSON.parse(message.data) as RunMeta
    upsertRunSummary(run)
    if (message.type === 'run-finished' && currentRun.value?.runId === run.runId) {
      void loadRunDetail(run.runId)
      void loadArtifacts(run.runId)
    }
  }
  globalRunSource.addEventListener('run-created', handleMessage)
  globalRunSource.addEventListener('run-updated', handleMessage)
  globalRunSource.addEventListener('run-finished', handleMessage)
}

async function handleVisibilityChange() {
  if (typeof document === 'undefined') {
    return
  }
  if (document.hidden) {
    stopGlobalRunStream()
    return
  }
  await loadRuns()
  startGlobalRunStream()
}

function onVisibilityChange() {
  void handleVisibilityChange()
}

function closeRunDialog() {
  runDialogOpen.value = false
  pendingRunBranch.value = null
  pendingRunTaskLabel.value = null
  pendingRunTaskDef.value = null
  dialogInputValues.value = {}
}

function resetRunSelection() {
  selectedRunNumber.value = null
  selectedRunId.value = null
  selectedChildTask.value = null
  selectedRunDetail.value = null
  artifacts.value = []
  taskLogs.value = {}
  stopLogStream()
}

function clearAllSelection() {
  selectedBranch.value = null
  selectedTask.value = null
  tasks.value = []
  inputValues.value = {}
  closeRunDialog()
  resetRunSelection()
  replaceRoute()
}

function resolveInputValues(inputs: TaskInput[], baseValues?: Record<string, string>): Record<string, string> {
  const nextValues: Record<string, string> = {}
  for (const input of inputs) {
    nextValues[input.id] = baseValues?.[input.id] ?? input.default ?? ''
  }
  return nextValues
}

function applyTaskInputs() {
  inputValues.value = resolveInputValues(selectedTaskDef.value?.inputs ?? [], inputValues.value)
}

function updateDialogInputValue(id: string, value: string) {
  dialogInputValues.value = {
    ...dialogInputValues.value,
    [id]: value,
  }
}

function openRunDialog(branch: string, task: BranchTask, values: Record<string, string>) {
  pendingRunBranch.value = branch
  pendingRunTaskLabel.value = task.label
  pendingRunTaskDef.value = task
  dialogInputValues.value = resolveInputValues(task.inputs ?? [], values)
  runDialogOpen.value = true
}

async function toggleBranch(branch: string) {
  routeNotFound.value = false
  if (selectedBranch.value === branch) {
    clearAllSelection()
    return
  }
  selectedBranch.value = branch
  selectedTask.value = null
  resetRunSelection()
  await loadTasksForBranch(branch)
  applyTaskInputs()
  replaceRoute()
}

function toggleTask(taskLabel: string) {
  routeNotFound.value = false
  if (selectedTask.value === taskLabel) {
    selectedTask.value = null
    inputValues.value = {}
    resetRunSelection()
    replaceRoute()
    return
  }
  selectedTask.value = taskLabel
  resetRunSelection()
  applyTaskInputs()
  replaceRoute()
}

async function fetchLatestBranches() {
  loading.value = true
  errorMessage.value = ''
  try {
    await api<{ status: string }>('/api/git/fetch', { method: 'POST' })
    await initializeFromRoute(parseRoute(window.location.pathname))
  } catch (error) {
    errorMessage.value = asErrorMessage(error)
  } finally {
    loading.value = false
  }
}

async function submitRun(branch: string, taskLabel: string, values: Record<string, string>) {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await api<RunStartResponse>('/api/runs', {
      method: 'POST',
      body: JSON.stringify({
        branch,
        taskLabel,
        inputValues: values,
      }),
    })
    selectedBranch.value = response.branch
    await loadTasksForBranch(response.branch)
    selectedTask.value = response.taskLabel
    inputValues.value = { ...values }
    selectedRunNumber.value = response.runNumber
    selectedRunId.value = response.runId
    selectedRunDetail.value = {
      runId: response.runId,
      runKey: response.runKey,
      branch: response.branch,
      taskLabel: response.taskLabel,
      runNumber: response.runNumber,
      status: response.status,
      startTime: response.startTime,
      endTime: response.endTime,
      exitCode: response.exitCode,
      user: response.user,
      trigger: response.trigger,
      inputValues: response.inputValues,
      tasks: response.tasks,
    }
    taskLogs.value = {}
    artifacts.value = []
    await loadRuns()
    await loadArtifacts(response.runId)
    ensureSelectedChildTask()
    startLogStream(response.runId)
    replaceRoute()
  } catch (error) {
    errorMessage.value = asErrorMessage(error)
  } finally {
    loading.value = false
  }
}

async function runTask() {
  if (!selectedBranch.value || !selectedTask.value || !canRun.value || !selectedTaskDef.value) {
    return
  }
  const nextValues = resolveInputValues(selectedTaskDef.value.inputs ?? [], inputValues.value)
  inputValues.value = nextValues
  if ((selectedTaskDef.value.inputs?.length ?? 0) > 0) {
    openRunDialog(selectedBranch.value, selectedTaskDef.value, nextValues)
    return
  }
  await submitRun(selectedBranch.value, selectedTask.value, nextValues)
}

async function rerunCurrent() {
  if (!currentRun.value || !canRun.value) {
    return
  }
  selectedBranch.value = currentRun.value.branch
  await loadTasksForBranch(selectedBranch.value)
  const taskDef = tasks.value.find((task) => task.label === currentRun.value?.taskLabel) ?? null
  selectedTask.value = currentRun.value.taskLabel
  inputValues.value = resolveInputValues(taskDef?.inputs ?? [], currentRun.value.inputValues ?? {})
  if (taskDef && (taskDef.inputs?.length ?? 0) > 0) {
    openRunDialog(currentRun.value.branch, taskDef, currentRun.value.inputValues ?? {})
    return
  }
  await submitRun(currentRun.value.branch, currentRun.value.taskLabel, inputValues.value)
}

async function confirmRunDialog() {
  if (!pendingRunBranch.value || !pendingRunTaskLabel.value || !pendingRunTaskDef.value) {
    return
  }
  const branch = pendingRunBranch.value
  const taskLabel = pendingRunTaskLabel.value
  const taskDef = pendingRunTaskDef.value
  const nextValues = resolveInputValues(taskDef.inputs ?? [], dialogInputValues.value)
  inputValues.value = nextValues
  closeRunDialog()
  await submitRun(branch, taskLabel, nextValues)
}

function ensureSelectedChildTask(preferred?: string) {
  if (preferred && runTasks.value.some((task) => task.label === preferred)) {
    selectedChildTask.value = preferred
    return
  }
  if (selectedChildTask.value && runTasks.value.some((task) => task.label === selectedChildTask.value)) {
    return
  }
  selectedChildTask.value = currentRun.value?.taskLabel ?? runTasks.value[0]?.label ?? null
}

function applyTaskEvent(event: SSETaskEvent) {
  if (!event.taskLabel) {
    return
  }
  if (event.type === 'task-line') {
    taskLogs.value = {
      ...taskLogs.value,
      [event.taskLabel]: (taskLogs.value[event.taskLabel] ?? '') + (event.line ?? ''),
    }
    return
  }
  const detail = selectedRunDetail.value
  if (!detail?.tasks) {
    return
  }
  const tasksCopy = detail.tasks.map((task) => ({ ...task }))
  const current = tasksCopy.find((task) => task.label === event.taskLabel)
  if (!current) {
    return
  }
  if (event.type === 'task-start') {
    current.status = 'running'
    current.startTime = event.startTime
  } else if (event.type === 'task-finish') {
    current.status = (event.status as TaskRun['status']) || 'success'
    current.exitCode = event.exitCode ?? 0
    current.endTime = event.endTime
  } else if (event.type === 'task-skip') {
    current.status = 'skipped'
    current.exitCode = event.exitCode ?? 0
    current.endTime = event.endTime
  }
  selectedRunDetail.value = { ...detail, tasks: tasksCopy }
}

function startLogStream(runId: string) {
  stopLogStream()
  taskLogs.value = {}
  source = new EventSource(`${buildRunAPIPath(runId)}/log`)
  for (const eventName of ['task-start', 'task-line', 'task-finish', 'task-skip']) {
    source.addEventListener(eventName, (raw) => {
      const event = JSON.parse((raw as MessageEvent).data) as SSETaskEvent
      applyTaskEvent(event)
    })
  }
  source.addEventListener('done', async () => {
    stopLogStream()
    await loadRuns()
    if (currentRun.value?.runId) {
      await loadRunDetail(currentRun.value.runId)
      await loadArtifacts(currentRun.value.runId)
      ensureSelectedChildTask()
    }
  })
  source.onerror = () => {
    stopLogStream()
  }
}

function startMetricsStream() {
  stopMetricsStream()
  metricsSource = new EventSource('/api/metrics/stream')
  const onMetrics = (raw: Event) => {
    const message = raw as MessageEvent
    metrics.value = JSON.parse(message.data) as MetricsSnapshot
  }
  metricsSource.addEventListener('snapshot', onMetrics)
  metricsSource.addEventListener('metrics', onMetrics)
  metricsSource.onerror = () => {
    stopMetricsStream()
  }
}

async function selectRun(run: RunMeta) {
  routeNotFound.value = false
  const hadBranch = Boolean(selectedBranch.value)
  const hadTask = Boolean(selectedTask.value)
  if (hadBranch && hadTask) {
    selectedBranch.value = run.branch
    await loadTasksForBranch(run.branch)
    selectedTask.value = run.taskLabel
    applyTaskInputs()
    selectedRunNumber.value = run.runNumber
  } else if (hadBranch) {
    selectedRunNumber.value = null
  } else {
    selectedRunNumber.value = null
  }
  selectedRunId.value = run.runId
  selectedRunDetail.value = null
  artifacts.value = []
  taskLogs.value = {}
  await loadRunDetail(run.runId)
  await loadArtifacts(run.runId)
  ensureSelectedChildTask(run.taskLabel)
  startLogStream(run.runId)
  replaceRoute()
}

function selectChildTask(taskLabel: string) {
  selectedChildTask.value = taskLabel
  replaceRoute()
}

async function initializeFromRoute(route: RouteState) {
  isApplyingRoute = true
  routeNotFound.value = false
  errorMessage.value = ''
  stopLogStream()

  try {
    await loadBranches()
    await loadRuns()

    closeRunDialog()
    selectedBranch.value = null
    selectedTask.value = null
    tasks.value = []
    inputValues.value = {}
    selectedRunNumber.value = null
    selectedRunId.value = null
    selectedRunDetail.value = null
    selectedChildTask.value = null
    artifacts.value = []
    taskLogs.value = {}

    if (route.branch) {
      if (branches.value.some((branch) => branch.shortName === route.branch)) {
        selectedBranch.value = route.branch
        await loadTasksForBranch(route.branch)
      } else {
        routeNotFound.value = true
      }
    }

    if (route.task) {
      if (tasks.value.some((task) => task.label === route.task)) {
        selectedTask.value = route.task
      } else if (route.branch) {
        selectedTask.value = route.task
        routeNotFound.value = true
      }
      applyTaskInputs()
    }

    if (route.runId) {
      const matchingRun = runs.value.find((run) => run.runId === route.runId) ?? null
      if (!matchingRun) {
        routeNotFound.value = true
      } else {
        selectedRunId.value = matchingRun.runId
        if (route.branch) {
          if (matchingRun.branch !== route.branch) {
            routeNotFound.value = true
          }
          if (branches.value.some((branch) => branch.shortName === route.branch)) {
            selectedBranch.value = route.branch
            await loadTasksForBranch(route.branch)
          } else {
            routeNotFound.value = true
          }
        }
        await loadRunDetail(matchingRun.runId)
        await loadArtifacts(matchingRun.runId)
        startLogStream(matchingRun.runId)
        ensureSelectedChildTask(route.childTask || matchingRun.taskLabel)
        if (route.childTask && !runTasks.value.some((task) => task.label === route.childTask)) {
          routeNotFound.value = true
        }
      }
      return
    }

    if (route.runNumber && route.branch && route.task) {
      const runNumber = Number.parseInt(route.runNumber, 10)
      const matchingRun = runs.value.find((run) =>
        run.branch === route.branch &&
        run.taskLabel === route.task &&
        run.runNumber === runNumber,
      ) ?? null

      if (!matchingRun) {
        routeNotFound.value = true
      } else {
        selectedBranch.value = matchingRun.branch
        if (!tasks.value.length || route.branch !== selectedBranch.value) {
          await loadTasksForBranch(matchingRun.branch)
        }
        selectedTask.value = matchingRun.taskLabel
        selectedRunNumber.value = matchingRun.runNumber
        selectedRunId.value = matchingRun.runId
        await loadRunDetail(matchingRun.runId)
        await loadArtifacts(matchingRun.runId)
        startLogStream(matchingRun.runId)
        ensureSelectedChildTask(route.childTask || matchingRun.taskLabel)
        if (route.childTask && !runTasks.value.some((task) => task.label === route.childTask)) {
          routeNotFound.value = true
        }
      }
    }
  } finally {
    isApplyingRoute = false
    replaceRoute()
  }
}

function handlePopState() {
  void initializeFromRoute(parseRoute(window.location.pathname))
}

function handleKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape') {
    return
  }
  if (runDialogOpen.value) {
    closeRunDialog()
    return
  }
  if (tokenManagerOpen.value) {
    closeTokenManager()
    return
  }
  if (settingsDialogOpen.value) {
    closeSettingsDialog()
  }
}

watch(selectedTaskDef, () => {
  applyTaskInputs()
})

watch(() => selectedRunDetail.value?.tasks, () => {
  ensureSelectedChildTask()
}, { deep: true })

onMounted(async () => {
  loadBackgroundPreference()
  await loadMe()
  if (isAdmin.value) {
    try {
      await loadSettings()
    } catch {
      settingsSummary.value = null
    }
  }
  if (!authRequired.value) {
    await initializeFromRoute(parseRoute(window.location.pathname))
    startRunsPolling()
    startGlobalRunStream()
    startMetricsStream()
    window.addEventListener('popstate', handlePopState)
    window.addEventListener('keydown', handleKeydown)
    document.addEventListener('visibilitychange', onVisibilityChange)
  }
})

onBeforeUnmount(() => {
  stopLogStream()
  stopMetricsStream()
  stopGlobalRunStream()
  stopRunsPolling()
  if (copiedTokenTimer !== null) {
    window.clearTimeout(copiedTokenTimer)
  }
  if (copiedCurlTimer !== null) {
    window.clearTimeout(copiedCurlTimer)
  }
  window.removeEventListener('popstate', handlePopState)
  window.removeEventListener('keydown', handleKeydown)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <div class="app-frame">
    <BackgroundEffect :paused="backgroundPaused" />
    <div v-if="routeNotFound || errorMessage" class="notification-layer" aria-live="polite" aria-atomic="true">
      <p v-if="routeNotFound" class="warning-banner notification-toast">
        The current branch/tasks.json does not fully match the URL. Runs that still exist in history remain available.
      </p>
      <p v-if="errorMessage" class="error-banner notification-toast">{{ errorMessage }}</p>
    </div>

    <div :class="['shell', { 'shell-auth': authRequired }]">
      <section v-if="authRequired" class="glass-panel auth-required">
        <h2>Authentication Required</h2>
        <p>Sign-in is required in this environment.</p>
        <a :href="loginPath" class="action-link auth-login-link">Login with OIDC</a>
      </section>

      <template v-else>
        <AppHeader
          :auth-required="authRequired"
          :login-path="loginPath"
          :authenticated="authenticated"
          :can-manage-tokens="canManageTokens"
          :is-admin="isAdmin"
          :current-user="currentUser"
          :user-name="userName"
          :user-email="userEmail"
          :background-paused="backgroundPaused"
          @open-settings="openSettingsDialog"
          @open-token-manager="openTokenManager"
          @logout="logout"
          @update:background-paused="backgroundPaused = $event"
        />

        <MetricsPanel :metrics="metrics" :metrics-config="settingsSummary?.metrics ?? null" />

        <TaskSelectionPanel
          :branches="branches"
          :tasks="tasks"
          :selected-branch="selectedBranch"
          :selected-task="selectedTask"
          :last-run-by-task="lastRunByTask"
          :filtered-runs="filteredRuns"
          :current-run-key="currentRun?.runKey ?? null"
          :loading="loading"
          :can-run="canRun"
          @toggle-branch="toggleBranch"
          @toggle-task="toggleTask"
          @fetch-branches="fetchLatestBranches"
          @run-task="runTask"
          @select-run="selectRun"
        />

        <ResultPanel
          v-if="currentRun"
          :current-run="currentRun"
          :current-task-def="currentRunTaskDef"
          :run-tasks="runTasks"
          :selected-child-task="selectedChildTask"
          :selected-run-task-status="selectedRunTaskStatus"
          :current-log-segments="currentLogSegments"
          :artifacts="artifacts"
          :loading="loading"
          :can-run="canRun"
          @select-child-task="selectChildTask"
          @rerun="rerunCurrent"
        />

        <footer class="app-footer">
          <hr class="app-footer-rule" />
          <div class="app-footer-copy">
            <span>&copy; Yoshiki Shibukawa</span>
            <span>License: AGPL</span>
            <a href="https://github.com/shibukawa/vsc-taskrunner" target="_blank" rel="noreferrer">GitHub</a>
          </div>
        </footer>

        <div v-if="runDialogOpen && pendingRunTaskDef" class="run-dialog-backdrop" @click.self="closeRunDialog">
          <section class="glass-panel run-dialog" role="dialog" aria-modal="true" :aria-labelledby="'run-dialog-title'">
            <div class="panel-head">
              <div>
                <h2 id="run-dialog-title">Run {{ pendingRunTaskDef.label }}</h2>
                <p class="panel-subtitle">{{ pendingRunBranch }}</p>
              </div>
            </div>

            <div class="run-dialog-fields">
              <label v-for="input in pendingRunTaskDef.inputs ?? []" :key="input.id" class="run-dialog-field">
                <span>{{ input.description || input.id }}</span>
                <input
                  v-if="input.type === 'promptString'"
                  :value="dialogInputValues[input.id] ?? ''"
                  type="text"
                  :placeholder="input.default || ''"
                  @input="updateDialogInputValue(input.id, ($event.target as HTMLInputElement).value)"
                />
                <select
                  v-else
                  :value="dialogInputValues[input.id] ?? ''"
                  @change="updateDialogInputValue(input.id, ($event.target as HTMLSelectElement).value)"
                >
                  <option v-if="!input.options?.some((option) => option.value === (dialogInputValues[input.id] ?? ''))" :value="dialogInputValues[input.id] ?? ''">
                    {{ dialogInputValues[input.id] || input.default || 'Select' }}
                  </option>
                  <option v-for="option in input.options || []" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </label>
            </div>

            <div class="run-dialog-actions">
              <button class="ghost compact-button" :disabled="loading" @click="closeRunDialog">Cancel</button>
              <button class="primary compact-button" :disabled="loading" @click="confirmRunDialog">Run</button>
            </div>
          </section>
        </div>

        <div v-if="tokenManagerOpen" class="run-dialog-backdrop" @click.self="closeTokenManager">
          <section class="glass-panel run-dialog token-dialog" role="dialog" aria-modal="true" aria-labelledby="token-dialog-title">
            <div class="panel-head">
              <div>
                <h2 id="token-dialog-title">API Tokens</h2>
                <p class="panel-subtitle">batch execution and run history access</p>
              </div>
              <div class="panel-tools">
                <button class="ghost compact-button" type="button" @click="downloadOpenAPIYAML">Download OpenAPI YAML</button>
              </div>
            </div>

            <div class="run-dialog-fields">
              <label class="run-dialog-field">
                <span>Label</span>
                <input :value="tokenLabel" type="text" placeholder="CI deploy token" @input="tokenLabel = ($event.target as HTMLInputElement).value" />
              </label>
              <label class="run-dialog-field">
                <span>TTL Days</span>
                <input :value="tokenTTLDays" type="number" min="1" step="1" @input="tokenTTLDays = Number(($event.target as HTMLInputElement).value || 0)" />
              </label>
              <label class="run-dialog-field token-scope-row">
                <input v-model="tokenScopeRead" type="checkbox" />
                <span>runs:read</span>
              </label>
              <label class="run-dialog-field token-scope-row">
                <input v-model="tokenScopeWrite" type="checkbox" />
                <span>runs:write</span>
              </label>
            </div>
            <p v-if="selectedTokenScopeValues.length === 0" class="artifact-meta token-helper-text">Select at least one scope.</p>

            <p v-if="tokenError" class="error-banner token-error">{{ tokenError }}</p>

            <div class="run-dialog-actions">
              <button class="ghost compact-button" :disabled="tokenLoading" type="button" @click="closeTokenManager">Close</button>
              <button class="primary compact-button" :disabled="createTokenDisabled" type="button" @click="createAPIToken">Create Token</button>
            </div>

            <div v-if="apiTokensEnabled" class="token-list">
              <article v-for="item in tokenItems" :key="item.id" class="artifact-card">
                <strong>{{ item.label }}</strong>
                <span class="artifact-meta">Scopes: {{ item.scopes.join(', ') }}</span>
                <span class="artifact-meta">Created: {{ formatDateTime(item.createdAt) }}</span>
                <span class="artifact-meta">Last used: {{ formatDateTime(item.lastUsedAt) }}</span>
                <span class="artifact-meta">Expires: {{ formatDateTime(item.expiresAt) }}</span>
                <span v-if="item.revokedAt" class="artifact-meta">Revoked: {{ formatDateTime(item.revokedAt) }}</span>
                <div v-if="createdTokenValue && item.id === createdTokenItemId" class="token-created-box">
                  <span class="summary-label">Token</span>
                  <div class="token-created-row">
                    <code class="token-created-value">{{ createdTokenValue }}</code>
                    <button class="ghost compact-button" type="button" @click="copyCreatedToken">{{ copiedToken ? 'Copied' : 'Copy' }}</button>
                  </div>
                  <div class="token-example-list">
                    <article class="artifact-card token-example-card">
                      <div class="token-example-copy">
                        <div>
                          <strong>GET /api/me</strong>
                          <div class="artifact-meta">current subject and capabilities</div>
                        </div>
                        <button class="ghost compact-button" type="button" @click="void copyText(buildExampleCurl('me'), 'me')">{{ copiedCurlTarget === 'me' ? 'Copied' : 'Copy curl' }}</button>
                      </div>
                    </article>
                    <article v-if="createdTokenHasReadScope" class="artifact-card token-example-card">
                      <div class="token-example-copy">
                        <div>
                          <strong>GET /api/runs</strong>
                          <div class="artifact-meta">list available runs</div>
                        </div>
                        <button class="ghost compact-button" type="button" @click="void copyText(buildExampleCurl('runs'), 'runs')">{{ copiedCurlTarget === 'runs' ? 'Copied' : 'Copy curl' }}</button>
                      </div>
                    </article>
                    <article v-if="createdTokenHasWriteScope" class="artifact-card token-example-card">
                      <div class="token-endpoint-row">
                        <div class="token-endpoint-url">
                          <strong>POST /api/runs</strong>
                          <div class="artifact-meta">{{ currentOrigin() }}/api/runs</div>
                        </div>
                        <select class="token-endpoint-select" :value="tokenExampleBranch ?? ''" @change="void onTokenExampleBranchChange(($event.target as HTMLSelectElement).value)">
                          <option v-for="branch in branches" :key="branch.shortName" :value="branch.shortName">
                            {{ branch.shortName }}
                          </option>
                        </select>
                        <select class="token-endpoint-select" :value="tokenExampleTask ?? ''" :disabled="tokenExampleTasks.length === 0" @change="tokenExampleTask = ($event.target as HTMLSelectElement).value || null">
                          <option v-if="tokenExampleTasks.length === 0" value="">No tasks available</option>
                          <option v-for="task in tokenExampleTasks" :key="task.label" :value="task.label">
                            {{ task.label }}
                          </option>
                        </select>
                        <button class="ghost compact-button" :disabled="!tokenExampleBranch || !tokenExampleTask" type="button" @click="void copyText(buildExampleCurl('run-post'), 'run-post')">{{ copiedCurlTarget === 'run-post' ? 'Copied' : 'Copy curl' }}</button>
                      </div>
                    </article>
                  </div>
                </div>
                <div class="run-dialog-actions">
                  <button class="ghost compact-button" :disabled="tokenLoading || !!item.revokedAt" type="button" @click="revokeAPIToken(item.id)">Revoke</button>
                </div>
              </article>
            </div>
          </section>
        </div>

        <div v-if="settingsDialogOpen" class="run-dialog-backdrop" @click.self="closeSettingsDialog">
          <section class="glass-panel run-dialog token-dialog settings-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-dialog-title">
            <div class="panel-head">
              <div>
                <h2 id="settings-dialog-title">Settings Summary</h2>
                <p class="panel-subtitle">resolved runtime configuration</p>
              </div>
            </div>

            <p v-if="settingsError" class="error-banner token-error">{{ settingsError }}</p>
            <p v-else-if="settingsLoading" class="artifact-meta token-helper-text">Loading settings...</p>

            <div v-if="settingsSummary && !settingsLoading" class="settings-grid">
              <article class="artifact-card settings-section">
                <strong>Repository</strong>
                <dl class="settings-list">
                  <dt>Source</dt>
                  <dd>{{ settingsSummary.repository.source || 'Not configured' }}</dd>
                  <dt>Cache path</dt>
                  <dd>{{ settingsSummary.repository.cachePath || 'Not configured' }}</dd>
                  <dt>Access token</dt>
                  <dd>{{ statusLabel(settingsSummary.repository.accessTokenConfigured, 'Configured', 'Not configured') }}</dd>
                </dl>
              </article>

              <article class="artifact-card settings-section">
                <strong>Auth</strong>
                <dl class="settings-list">
                  <dt>Mode</dt>
                  <dd>{{ statusLabel(settingsSummary.auth.noAuth, 'NoAuth', 'OIDC') }}</dd>
                  <dt>Issuer</dt>
                  <dd>{{ settingsSummary.auth.oidcIssuer || 'Not configured' }}</dd>
                  <dt>API tokens</dt>
                  <dd>{{ statusLabel(settingsSummary.auth.apiTokensEnabled) }}</dd>
                  <dt>Token store</dt>
                  <dd>{{ storeSummaryLabel(settingsSummary.auth.apiTokenStore) }}</dd>
                  <dt v-if="settingsSummary.auth.apiTokenStore.localPath">Token path</dt>
                  <dd v-if="settingsSummary.auth.apiTokenStore.localPath">{{ settingsSummary.auth.apiTokenStore.localPath }}</dd>
                  <dt v-if="settingsSummary.auth.apiTokenStore.endpoint">Endpoint</dt>
                  <dd v-if="settingsSummary.auth.apiTokenStore.endpoint">{{ settingsSummary.auth.apiTokenStore.endpoint }}</dd>
                  <dt v-if="settingsSummary.auth.apiTokenStore.bucket">Bucket</dt>
                  <dd v-if="settingsSummary.auth.apiTokenStore.bucket">{{ settingsSummary.auth.apiTokenStore.bucket }}</dd>
                  <dt v-if="settingsSummary.auth.apiTokenStore.region">Region</dt>
                  <dd v-if="settingsSummary.auth.apiTokenStore.region">{{ settingsSummary.auth.apiTokenStore.region }}</dd>
                  <dt v-if="settingsSummary.auth.apiTokenStore.prefix">Prefix</dt>
                  <dd v-if="settingsSummary.auth.apiTokenStore.prefix">{{ settingsSummary.auth.apiTokenStore.prefix }}</dd>
                </dl>
              </article>

              <article class="artifact-card settings-section">
                <strong>Execution</strong>
                <dl class="settings-list">
                  <dt>Runtime mode</dt>
                  <dd>{{ settingsSummary.runtimeMode }}</dd>
                  <dt>Max parallel runs</dt>
                  <dd>{{ settingsSummary.execution.maxParallelRuns }}</dd>
                </dl>
              </article>

              <article class="artifact-card settings-section">
                <strong>Metrics</strong>
                <dl class="settings-list">
                  <dt>Status</dt>
                  <dd>{{ statusLabel(settingsSummary.metrics.enabled) }}</dd>
                  <dt>CPU interval</dt>
                  <dd>{{ settingsSummary.metrics.cpuInterval }}s</dd>
                  <dt>Memory interval</dt>
                  <dd>{{ settingsSummary.metrics.memoryInterval }}s</dd>
                  <dt>Storage interval</dt>
                  <dd>{{ settingsSummary.metrics.storageInterval }}s</dd>
                  <dt>Memory history window</dt>
                  <dd>{{ settingsSummary.metrics.memoryHistoryWindow }}s</dd>
                </dl>
              </article>

              <article class="artifact-card settings-section">
                <strong>Storage</strong>
                <dl class="settings-list">
                  <dt>Backend</dt>
                  <dd>{{ settingsSummary.storage.backend || 'local' }}</dd>
                  <dt>History dir</dt>
                  <dd>{{ settingsSummary.storage.historyDir || 'Not configured' }}</dd>
                  <dt>History keep count</dt>
                  <dd>{{ settingsSummary.storage.historyKeepCount }}</dd>
                  <dt>Keep worktrees on success</dt>
                  <dd>{{ settingsSummary.storage.worktree.keepOnSuccess }}</dd>
                  <dt>Keep worktrees on failure</dt>
                  <dd>{{ settingsSummary.storage.worktree.keepOnFailure }}</dd>
                  <dt v-if="settingsSummary.storage.object.endpoint">Endpoint</dt>
                  <dd v-if="settingsSummary.storage.object.endpoint">{{ settingsSummary.storage.object.endpoint }}</dd>
                  <dt v-if="settingsSummary.storage.object.bucket">Bucket</dt>
                  <dd v-if="settingsSummary.storage.object.bucket">{{ settingsSummary.storage.object.bucket }}</dd>
                  <dt v-if="settingsSummary.storage.object.region">Region</dt>
                  <dd v-if="settingsSummary.storage.object.region">{{ settingsSummary.storage.object.region }}</dd>
                  <dt v-if="settingsSummary.storage.object.prefix">Prefix</dt>
                  <dd v-if="settingsSummary.storage.object.prefix">{{ settingsSummary.storage.object.prefix }}</dd>
                </dl>
              </article>
            </div>

            <div class="run-dialog-actions">
              <button class="ghost compact-button" type="button" @click="closeSettingsDialog">Close</button>
            </div>
          </section>
        </div>
      </template>
    </div>
  </div>
</template>
