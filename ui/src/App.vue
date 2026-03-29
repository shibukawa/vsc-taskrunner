<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type {
  Branch, BranchTask, TaskInput, TaskRun, ArtifactItem, RunMeta,
  RunStartResponse, MeResponse, RouteState, SSETaskEvent,
  MetricsSnapshot,
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
const routeNotFound = ref(false)
const backgroundPaused = ref(false)

let source: EventSource | null = null
let metricsSource: EventSource | null = null
let isApplyingRoute = false

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

  try {
    const me = await api<MeResponse>('/api/me')
    authenticated.value = me.authenticated
    currentUser.value = me.subject ?? ''
    userName.value = me.claims?.name ?? me.claims?.preferred_username ?? me.subject ?? ''
    userEmail.value = me.claims?.email ?? me.subject ?? ''
    canRun.value = me.canRun ?? true
  } catch (error) {
    const message = asErrorMessage(error)
    if (isUnauthorizedMessage(message)) {
      authRequired.value = true
      return
    }
    errorMessage.value = message
  }
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

async function loadBranches() {
  const data = await api<unknown>('/api/git/branches')
  branches.value = Array.isArray(data) ? (data as Branch[]) : []
}

async function loadTasksForBranch(branch: string | null) {
  if (!branch) {
    tasks.value = []
    return
  }
  const cachedBranch = branches.value.find((item) => item.shortName === branch) ?? null
  if (cachedBranch?.tasks) {
    tasks.value = cachedBranch.tasks
    if (cachedBranch.loadError) {
      errorMessage.value = cachedBranch.loadError
    }
    return
  }
  const data = await api<unknown>(`/api/git/branches/${encodeURIComponent(branch)}/tasks`)
  tasks.value = Array.isArray(data) ? (data as BranchTask[]) : []
}

async function loadRuns() {
  const data = await api<unknown>('/api/runs')
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
  if (event.key === 'Escape' && runDialogOpen.value) {
    closeRunDialog()
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
  if (!authRequired.value) {
    await initializeFromRoute(parseRoute(window.location.pathname))
    startMetricsStream()
    window.addEventListener('popstate', handlePopState)
    window.addEventListener('keydown', handleKeydown)
  }
})

onBeforeUnmount(() => {
  stopLogStream()
  stopMetricsStream()
  window.removeEventListener('popstate', handlePopState)
  window.removeEventListener('keydown', handleKeydown)
})
</script>

<template>
  <div class="app-frame">
    <BackgroundEffect :paused="backgroundPaused" />
    <div v-if="routeNotFound || errorMessage" class="notification-layer" aria-live="polite" aria-atomic="true">
      <p v-if="routeNotFound" class="warning-banner notification-toast">
        現在の branch/tasks.json と URL は一部一致しません。履歴が残っている run は表示を継続しています。
      </p>
      <p v-if="errorMessage" class="error-banner notification-toast">{{ errorMessage }}</p>
    </div>

    <div :class="['shell', { 'shell-auth': authRequired }]">
      <section v-if="authRequired" class="glass-panel auth-required">
        <h2>Authentication Required</h2>
        <p>この環境ではログインが必要です。</p>
        <a :href="loginPath" class="action-link auth-login-link">Login with OIDC</a>
      </section>

      <template v-else>
        <AppHeader
          :auth-required="authRequired"
          :login-path="loginPath"
          :authenticated="authenticated"
          :current-user="currentUser"
          :user-name="userName"
          :user-email="userEmail"
          :background-paused="backgroundPaused"
          @logout="logout"
          @update:background-paused="backgroundPaused = $event"
        />

        <MetricsPanel :metrics="metrics" />

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
      </template>
    </div>
  </div>
</template>
