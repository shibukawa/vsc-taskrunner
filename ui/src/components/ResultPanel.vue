<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { RunMeta, TaskRun, ArtifactItem, LogSegment, BranchTask } from '../types'
import { formatBytes, formatDateTime, prettyStatus, statusClass } from '../formatters'

const props = defineProps<{
  currentRun: RunMeta | null
  currentTaskDef: BranchTask | null
  runTasks: TaskRun[]
  selectedChildTask: string | null
  selectedRunTaskStatus: string
  currentLogSegments: LogSegment[]
  artifacts: ArtifactItem[]
  loading: boolean
  canRun: boolean
}>()

const emit = defineEmits<{
  'select-child-task': [label: string]
  'rerun': []
}>()

type SummaryRow = {
  label: string
  value: string
  wide?: boolean
  fullValue?: string
  isHash?: boolean
}

type InputRow = {
  id: string
  label: string
  value: string
}

type TimelineTask = {
  label: string
  status: TaskRun['status']
  statusMarkerClass: string
  elapsedSecondsText: string
  durationSecondsText: string
  compactMetaText: string
  showMeta: boolean
  showEmptyText: boolean
  tooltip: string
  laneStyle: Record<string, string>
  hasBar: boolean
  active: boolean
}

type TimelineWindow = {
  start: number
  end: number
  duration: number
}

const inputRows = computed<InputRow[]>(() => {
  const currentRun = props.currentRun
  if (!currentRun?.inputValues) {
    return []
  }
  const inputDefinitions = new Map((props.currentTaskDef?.inputs ?? []).map((input) => [input.id, input]))
  return Object.entries(currentRun.inputValues).map(([id, value]) => ({
    id,
    label: inputDefinitions.get(id)?.description || id,
    value,
  }))
})

const showTaskFlow = computed(() => props.runTasks.length > 0)
const showArtifacts = computed(() => props.artifacts.length > 0)
const resultStatusEmoji = computed(() => {
  switch (props.currentRun?.status) {
    case 'success':
      return '🟢'
    case 'failed':
      return '🔴'
    case 'running':
      return '🟡'
    default:
      return '⚪'
  }
})
const now = ref(Date.now())
let nowTimer: number | null = null
const copiedArtifactPath = ref<string | null>(null)
let copiedArtifactTimer: number | null = null

function syncNowTimer() {
  const hasRunningTask = props.runTasks.some((task) => task.status === 'running')
  if (hasRunningTask && nowTimer === null && typeof window !== 'undefined') {
    now.value = Date.now()
    nowTimer = window.setInterval(() => {
      now.value = Date.now()
    }, 1000)
    return
  }
  if (!hasRunningTask && nowTimer !== null) {
    window.clearInterval(nowTimer)
    nowTimer = null
  }
}

onMounted(() => {
  syncNowTimer()
})

watch(() => props.runTasks, () => {
  syncNowTimer()
}, { deep: true })

onBeforeUnmount(() => {
  if (nowTimer !== null) {
    window.clearInterval(nowTimer)
  }
  if (copiedArtifactTimer !== null) {
    window.clearTimeout(copiedArtifactTimer)
  }
})

function shortHash(value: string): string {
  if (!value) {
    return 'N/A'
  }
  return value.length > 7 ? value.slice(0, 7) : value
}

async function copyArtifactHash(artifact: ArtifactItem) {
  if (!artifact.hashSha256 || typeof navigator === 'undefined' || !navigator.clipboard?.writeText) {
    return
  }
  await navigator.clipboard.writeText(artifact.hashSha256)
  copiedArtifactPath.value = artifact.path
  if (copiedArtifactTimer !== null) {
    window.clearTimeout(copiedArtifactTimer)
  }
  copiedArtifactTimer = window.setTimeout(() => {
    copiedArtifactPath.value = null
    copiedArtifactTimer = null
  }, 1600)
}

function parseTime(value?: string): number | null {
  if (!value) {
    return null
  }
  const date = new Date(value)
  const timestamp = date.getTime()
  if (Number.isNaN(timestamp) || date.getUTCFullYear() <= 1) {
    return null
  }
  return timestamp
}

function compareTaskOrder(a: TaskRun, b: TaskRun): number {
  const aStart = parseTime(a.startTime)
  const bStart = parseTime(b.startTime)
  if (aStart !== null && bStart !== null && aStart !== bStart) {
    return aStart - bStart
  }
  if (aStart !== null && bStart === null) {
    return -1
  }
  if (aStart === null && bStart !== null) {
    return 1
  }
  return a.label.localeCompare(b.label)
}

const orderedRunTasks = computed(() => {
  if (props.runTasks.length <= 1) {
    return props.runTasks
  }

  const byLabel = new Map(props.runTasks.map((task) => [task.label, task]))
  const indegree = new Map(props.runTasks.map((task) => [task.label, 0]))
  const dependents = new Map<string, string[]>()

  for (const task of props.runTasks) {
    for (const dep of task.dependsOn ?? []) {
      if (!byLabel.has(dep)) {
        continue
      }
      indegree.set(task.label, (indegree.get(task.label) ?? 0) + 1)
      const next = dependents.get(dep) ?? []
      next.push(task.label)
      dependents.set(dep, next)
    }
  }

  const queue = props.runTasks
    .filter((task) => (indegree.get(task.label) ?? 0) === 0)
    .map((task) => task.label)
  queue.sort((left, right) => {
    const leftTask = byLabel.get(left)
    const rightTask = byLabel.get(right)
    if (!leftTask || !rightTask) {
      return left.localeCompare(right)
    }
    return compareTaskOrder(leftTask, rightTask)
  })
  const ordered: TaskRun[] = []

  while (queue.length > 0) {
    const label = queue.shift()
    if (!label) {
      continue
    }
    const task = byLabel.get(label)
    if (!task) {
      continue
    }
    ordered.push(task)
    for (const dependent of dependents.get(label) ?? []) {
      const next = (indegree.get(dependent) ?? 0) - 1
      indegree.set(dependent, next)
      if (next === 0) {
        queue.push(dependent)
        queue.sort((left, right) => {
          const leftTask = byLabel.get(left)
          const rightTask = byLabel.get(right)
          if (!leftTask || !rightTask) {
            return left.localeCompare(right)
          }
          return compareTaskOrder(leftTask, rightTask)
        })
      }
    }
  }

  if (ordered.length === props.runTasks.length) {
    return ordered
  }
  return [...props.runTasks].sort(compareTaskOrder)
})

function formatSeconds(milliseconds: number | null): string {
  if (milliseconds === null || milliseconds < 0) {
    return '...'
  }
  return `${(Math.max(0, milliseconds) / 1000).toFixed(1)}s`
}

function formatDurationMilliseconds(milliseconds: number | null): string {
  if (milliseconds === null || milliseconds < 0) {
    return 'N/A'
  }
  const seconds = Math.max(0, Math.round(milliseconds / 1000))
  if (seconds < 60) {
    return `${seconds}s`
  }
  const minutes = Math.floor(seconds / 60)
  const remain = seconds % 60
  return `${minutes}m ${remain}s`
}

const timelineWindow = computed<TimelineWindow>(() => {
  const taskStartTimes = orderedRunTasks.value
    .map((task) => parseTime(task.startTime))
    .filter((value): value is number => value !== null)
  const start = taskStartTimes.length > 0
    ? Math.min(...taskStartTimes)
    : (parseTime(props.currentRun?.startTime) ?? now.value)
  let end = start

  for (const task of orderedRunTasks.value) {
    const taskStart = parseTime(task.startTime)
    const taskEnd = parseTime(task.endTime) ?? (task.status === 'running' ? now.value : null)
    if (taskStart !== null) {
      end = Math.max(end, taskEnd ?? taskStart)
    }
  }

  if (taskStartTimes.length === 0) {
    const fallbackEnd = parseTime(props.currentRun?.endTime) ?? now.value
    end = Math.max(end, fallbackEnd)
  }

  return {
    start,
    end,
    duration: Math.max(end - start, 0),
  }
})

const summaryRows = computed<SummaryRow[]>(() => {
  if (!props.currentRun) {
    return []
  }
  const rows: SummaryRow[] = [
    { label: 'User', value: props.currentRun.user || 'anonymous' },
    { label: 'Started', value: formatDateTime(props.currentRun.startTime) },
    { label: 'Duration', value: formatDurationMilliseconds(timelineWindow.value.duration) },
    { label: 'Exit Code', value: String(props.currentRun.exitCode) },
  ]
  if (props.currentRun.tokenLabel) {
    rows.splice(1, 0, { label: 'API Token', value: props.currentRun.tokenLabel })
  }
  if (props.currentRun.commitHash) {
    rows.push({
      label: 'Git Hash',
      value: shortHash(props.currentRun.commitHash),
      fullValue: props.currentRun.commitHash,
      isHash: true,
    })
  }
  return rows
})

const timelineTasks = computed<TimelineTask[]>(() => {
  const { start: timelineStart, duration: timelineDuration } = timelineWindow.value
  const total = Math.max(timelineDuration, 1)
  return orderedRunTasks.value.map((task) => {
    const start = parseTime(task.startTime)
    const end = parseTime(task.endTime) ?? (task.status === 'running' ? now.value : null)
    const hasBar = start !== null && task.status !== 'pending' && task.status !== 'skipped'
    const rawOffset = start === null ? 0 : ((start - timelineStart) / total) * 100
    const rawWidth = start === null || end === null ? 0 : ((Math.max(end - start, 0)) / total) * 100
    const laneStyle = {
      '--task-offset': `${Math.min(Math.max(rawOffset, 0), 100)}%`,
      '--task-width': `${Math.min(Math.max(rawWidth, 0), 100)}%`,
    }
    const elapsedMilliseconds = start === null ? null : start - timelineStart
    const durationMilliseconds = start === null || end === null ? null : end - start
    const elapsedSecondsText = `+${formatSeconds(elapsedMilliseconds)}`
    const durationSecondsText = formatSeconds(durationMilliseconds)
    const compactMetaText = `${elapsedSecondsText} / ${durationSecondsText}`
    const hasValidMeta = elapsedMilliseconds !== null && durationMilliseconds !== null
    const showMeta = task.status !== 'running' && hasValidMeta
    const showEmptyText = !hasBar && showMeta
    const tooltipParts = [task.label, `Status: ${prettyStatus(task.status)}`]
    if (showMeta) {
      tooltipParts.push(`Elapsed: ${elapsedSecondsText}`)
      tooltipParts.push(`Duration: ${durationSecondsText}`)
    }
    return {
      label: task.label,
      status: task.status,
      statusMarkerClass: `status-marker status-marker-${task.status}`,
      elapsedSecondsText,
      durationSecondsText,
      compactMetaText,
      showMeta,
      showEmptyText,
      tooltip: tooltipParts.join('\n'),
      laneStyle,
      hasBar,
      active: task.label === props.selectedChildTask,
    }
  })
})
</script>

<template>
  <article class="glass-panel result-panel">
    <template v-if="currentRun">
      <div class="result-scroll">
        <div :class="['result-grid', { 'result-grid-no-artifacts': !showArtifacts }]">
          <div class="panel-head result-grid-head result-grid-head-status">
            <div>
              <h2><span class="result-title-status" aria-hidden="true">{{ resultStatusEmoji }}</span>Result</h2>
              <p class="panel-subtitle">#{{ currentRun.runNumber }} / {{ currentRun.branch }} / {{ currentRun.taskLabel }}</p>
            </div>
            <div class="panel-tools">
              <button class="ghost compact-button panel-action panel-action-small" :disabled="loading || !canRun" @click="emit('rerun')">Rerun</button>
            </div>
          </div>

          <div class="result-grid-body result-grid-body-status">
            <section class="result-summary-panel">
              <div class="result-summary-grid">
                <div v-for="row in summaryRows" :key="row.label" :class="['summary-cell', { wide: row.wide }]">
                  <span class="summary-label">{{ row.label }}</span>
                  <strong
                    :class="['summary-value', { 'summary-hash': row.isHash }]"
                    :title="row.fullValue || undefined"
                  >
                    {{ row.value }}
                  </strong>
                </div>
              </div>
            </section>

            <section v-if="inputRows.length" class="result-summary-panel result-input-panel">
              <div class="result-flow-head">
                <h3>Inputs</h3>
                <span class="panel-note">{{ inputRows.length }} values</span>
              </div>
              <div class="result-input-list">
                <div v-for="row in inputRows" :key="row.id" class="result-input-row">
                  <span class="summary-label">{{ row.label }}</span>
                  <strong class="summary-value">{{ row.value }}</strong>
                </div>
              </div>
            </section>
          </div>

          <div v-if="showArtifacts" class="panel-head result-grid-head result-grid-head-artifacts">
            <h2>Artifacts</h2>
          </div>

          <div v-if="showArtifacts" class="result-grid-body result-grid-body-artifacts">
            <div class="artifact-list">
                <div v-for="artifact in artifacts" :key="artifact.path" class="artifact-card">
                <a :href="artifact.downloadUrl" class="artifact-link">{{ artifact.path }}</a>
                <div class="artifact-meta-row">
                  <span class="artifact-meta">{{ formatBytes(artifact.sizeBytes) }}</span>
                  <span class="artifact-meta artifact-hash" :title="artifact.hashSha256">SHA-256 <span class="hash-linkish">{{ shortHash(artifact.hashSha256) }}</span></span>
                  <button
                    type="button"
                    class="artifact-copy-button"
                    :aria-label="`Copy SHA-256 for ${artifact.path}`"
                    :title="copiedArtifactPath === artifact.path ? 'Copied' : 'Copy full SHA-256'"
                    @click="copyArtifactHash(artifact)"
                  >
                    {{ copiedArtifactPath === artifact.path ? 'Copied' : 'Copy' }}
                  </button>
                </div>
              </div>
            </div>
          </div>

          <div v-if="showTaskFlow" class="result-grid-head result-grid-head-flow">
            <h3 id="result-task-flow-label">Tasks</h3>
          </div>

          <section v-if="showTaskFlow" class="result-flow-panel result-grid-body result-grid-body-flow">
            <div class="timeline-list" role="listbox" aria-labelledby="result-task-flow-label">
              <template v-for="task in timelineTasks" :key="task.label">
                <button
                  type="button"
                  :class="['timeline-row', statusClass(task.status), { active: task.active }]"
                  role="option"
                  :aria-selected="task.active"
                  :title="task.tooltip"
                  @click="emit('select-child-task', task.label)"
                >
                  <span :class="task.statusMarkerClass" aria-hidden="true"></span>
                  <span class="timeline-label">{{ task.label }}</span>
                  <span class="timeline-track">
                    <span v-if="task.hasBar" class="timeline-bar-shell" :style="task.laneStyle">
                      <span class="timeline-bar"></span>
                    </span>
                    <span v-else-if="task.showEmptyText" class="timeline-empty">{{ task.compactMetaText }}</span>
                  </span>
                  <span v-if="task.showMeta" class="timeline-meta">{{ task.compactMetaText }}</span>
                </button>
              </template>
            </div>
          </section>

          <div class="result-grid-head result-grid-head-logs">
            <h2>Logs</h2>
          </div>

          <section class="result-logs result-grid-body result-grid-body-logs">
            <pre class="terminal"><template v-for="(segment, index) in currentLogSegments" :key="index"><span :class="segment.class" :style="segment.style">{{ segment.text }}</span></template><template v-if="!currentLogSegments.length">No logs yet.</template></pre>
          </section>
        </div>
      </div>
    </template>

  </article>
</template>

<style>
.result-panel {
  grid-area: result;
  padding: 22px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.result-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}

.result-grid {
  display: grid;
  grid-template-areas:
    "status-title artifacts-title"
    "status-body artifacts-body"
    "status-gap artifacts-gap"
    "flow-title flow-title"
    "flow-body flow-body"
    "flow-gap flow-gap"
    "logs-title logs-title"
    "logs-body logs-body";
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  grid-template-rows: auto auto 14px auto auto 14px auto auto;
  column-gap: 14px;
  row-gap: 0;
  min-height: min-content;
  align-items: start;
}

.result-grid-no-artifacts {
  grid-template-areas:
    "status-title"
    "status-body"
    "status-gap"
    "flow-title"
    "flow-body"
    "flow-gap"
    "logs-title"
    "logs-body";
  grid-template-columns: minmax(0, 1fr);
  grid-template-rows: auto auto 14px auto auto 14px auto auto;
}

.result-flow-panel {
  grid-area: flow-body;
}

.result-logs {
  grid-area: logs-body;
}

.result-grid-head {
  align-self: end;
  min-height: 0;
}

.result-grid-head-status {
  grid-area: status-title;
}

.result-grid-head-artifacts {
  grid-area: artifacts-title;
}

.result-grid-head-flow {
  grid-area: flow-title;
}

.result-grid-head-logs {
  grid-area: logs-title;
}

.result-grid-head h2,
.result-grid-head h3 {
  margin: 0;
  font-family: 'IBM Plex Sans', sans-serif;
  font-size: 1.05rem;
  line-height: 1.15;
  font-weight: 600;
  letter-spacing: -0.01em;
  min-width: 0;
}

.result-grid-body {
  align-self: start;
  min-height: 0;
}

.result-grid-body-status {
  grid-area: status-body;
}

.result-grid-body-artifacts {
  grid-area: artifacts-body;
}

.result-grid-body-flow {
  grid-area: flow-body;
}

.result-grid-body-logs {
  grid-area: logs-body;
}

.result-grid-body-status,
.result-grid-body-artifacts {
  display: grid;
  gap: 12px;
}

.result-summary-panel,
.result-flow-panel {
  border-radius: 18px;
  padding: 14px 16px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.16), rgba(255, 255, 255, 0.08)),
    rgba(79, 107, 205, 0.14);
  border: 1px solid rgba(255, 255, 255, 0.2);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.14);
}

.result-title-status {
  display: inline-block;
  margin-right: 0.38rem;
}

.result-summary-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 14px;
}

.result-input-panel {
  margin-top: 12px;
}

.result-input-list {
  display: grid;
  gap: 10px;
}

.result-input-row {
  display: grid;
  gap: 4px;
}

.summary-cell {
  display: grid;
  gap: 4px;
  align-content: start;
}

.summary-cell.wide {
  grid-column: 1 / -1;
}

.summary-label {
  color: var(--muted);
  font-size: 0.74rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.summary-value {
  color: var(--ink);
  font-size: 0.98rem;
  line-height: 1.3;
  font-weight: 600;
  word-break: break-word;
}

.summary-hash,
.hash-linkish {
  text-decoration: underline;
  text-decoration-color: rgba(255, 255, 255, 0.38);
  text-underline-offset: 0.18em;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
}

.timeline-list {
  display: grid;
  gap: 4px;
}

.timeline-row {
  display: grid;
  grid-template-columns: auto minmax(84px, 168px) minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 4px 8px;
  border-radius: 12px;
  border: 1px solid transparent;
  background: transparent;
  text-align: left;
  transition:
    transform 140ms ease,
    border-color 140ms ease,
    background 140ms ease,
    box-shadow 140ms ease;
}

.timeline-row:hover,
.timeline-row:focus-visible {
  transform: translateY(-1px);
  border-color: rgba(255, 255, 255, 0.16);
  background: rgba(255, 255, 255, 0.06);
}

.timeline-row.active {
  border-color: rgba(255, 255, 255, 0.12);
  background:
    linear-gradient(180deg, rgba(28, 34, 45, 0.92), rgba(17, 21, 30, 0.88)),
    rgba(16, 19, 25, 0.9);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 8px 18px rgba(10, 12, 18, 0.18);
}

.status-marker {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: rgba(95, 109, 113, 0.55);
  flex: none;
}

.status-marker-success {
  background: #0d7144;
}

.status-marker-failed {
  background: #c53f3f;
}

.status-marker-running {
  background: #c38b16;
}

.status-marker-skipped,
.status-marker-pending {
  background: rgba(95, 109, 113, 0.5);
}

.timeline-label {
  color: var(--ink);
  font-weight: 600;
  min-width: 0;
  word-break: break-word;
  font-size: 0.84rem;
  line-height: 1.15;
}

.timeline-row.active .timeline-label,
.timeline-row.active .timeline-meta {
  color: #f7fbfc;
}

.timeline-row.active .timeline-meta {
  opacity: 0.72;
}

.timeline-track {
  position: relative;
  height: 12px;
  border-radius: 999px;
  background:
    linear-gradient(90deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.05)),
    rgba(14, 26, 46, 0.18);
  overflow: hidden;
}

.timeline-track::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image: linear-gradient(to right, rgba(255, 255, 255, 0.08) 1px, transparent 1px);
  background-size: 25% 100%;
  opacity: 0.35;
}

.timeline-bar-shell {
  position: absolute;
  top: 2px;
  bottom: 2px;
  left: var(--task-offset);
  width: var(--task-width);
  min-width: 4px;
}

.timeline-bar {
  display: block;
  width: 100%;
  height: 100%;
  border-radius: 999px;
  background: rgba(95, 109, 113, 0.55);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.25);
}

.timeline-row.status-running .timeline-bar {
  background: linear-gradient(90deg, rgba(243, 207, 138, 0.92), rgba(214, 164, 37, 0.92));
}

.timeline-row.status-success .timeline-bar {
  background: linear-gradient(90deg, rgba(79, 177, 121, 0.9), rgba(13, 113, 68, 0.9));
}

.timeline-row.status-failed .timeline-bar {
  background: linear-gradient(90deg, rgba(214, 110, 110, 0.92), rgba(165, 52, 52, 0.92));
}

.timeline-empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  padding-inline: 6px;
  color: var(--muted);
  font-size: 0.68rem;
}

.timeline-meta {
  color: var(--muted);
  font-size: 0.72rem;
  white-space: nowrap;
}

.result-grid-body-artifacts .artifact-list {
  gap: 8px;
  align-content: start;
  overflow: visible;
  flex: none;
  min-height: auto;
}

.result-grid-body-artifacts .artifact-card {
  padding: 10px 12px;
}

.artifact-card {
  display: grid;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 16px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.08), rgba(255, 255, 255, 0.03)),
    rgba(79, 107, 205, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.14);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.artifact-link {
  color: var(--ink);
  text-decoration: underline;
  text-decoration-color: rgba(255, 255, 255, 0.35);
  text-underline-offset: 0.18em;
  word-break: break-all;
  font-weight: 600;
}

.artifact-meta-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
  text-align: right;
}

.artifact-meta {
  color: var(--muted);
  font-size: 0.84rem;
  line-height: 1.35;
  word-break: break-word;
}

.artifact-hash {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, 'Liberation Mono', monospace;
}

.artifact-copy-button {
  border: 1px solid rgba(255, 255, 255, 0.22);
  background: rgba(255, 255, 255, 0.08);
  color: var(--ink);
  border-radius: 999px;
  padding: 4px 10px;
  font-size: 0.75rem;
  line-height: 1;
  cursor: pointer;
  transition:
    background 140ms ease,
    border-color 140ms ease,
    transform 140ms ease;
}

.artifact-copy-button:hover,
.artifact-copy-button:focus-visible {
  background: rgba(255, 255, 255, 0.14);
  border-color: rgba(255, 255, 255, 0.34);
  transform: translateY(-1px);
}

.terminal {
  margin: 0;
  padding: 16px 18px;
  border-radius: 22px;
  border: 1px solid rgba(122, 195, 174, 0.16);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.03), transparent),
    var(--terminal);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.03);
  color: #d7ffe8;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  overflow: visible;
}

.terminal-segment {
  white-space: inherit;
}

.ansi-bold {
  font-weight: 700;
}

.ansi-dim {
  opacity: 0.72;
}

.ansi-italic {
  font-style: italic;
}

.ansi-underline {
  text-decoration: underline;
}

.ansi-strike {
  text-decoration: line-through;
}

@media (max-width: 1260px) {
  .result-grid {
    grid-template-areas:
      "status-title"
      "status-body"
      "artifacts-title"
      "artifacts-body"
      "flow-title"
      "flow-body"
      "logs-title"
      "logs-body";
    grid-template-columns: 1fr;
    grid-template-rows: auto auto 14px auto auto 14px auto auto;
    row-gap: 0;
  }

  .result-grid-no-artifacts {
    grid-template-areas:
      "status-title"
      "status-body"
      "flow-title"
      "flow-body"
      "logs-title"
      "logs-body";
    grid-template-columns: 1fr;
    grid-template-rows: auto auto 14px auto auto 14px;
    row-gap: 0;
  }

  .result-panel {
    overflow: visible;
  }

  .result-scroll {
    overflow: visible;
    padding-right: 0;
  }

  .timeline-row {
    grid-template-columns: auto minmax(72px, 132px) minmax(0, 1fr) auto;
  }
}

@media (max-width: 720px) {
  .result-panel {
    padding: 18px;
    border-radius: var(--panel-radius);
  }

  .result-summary-grid {
    grid-template-columns: 1fr;
  }

  .timeline-row {
    grid-template-columns: auto minmax(0, 1fr);
    grid-template-areas:
      "marker label"
      "track track"
      "meta meta";
    gap: 4px 8px;
  }

  .status-marker {
    grid-area: marker;
    align-self: center;
  }

  .timeline-label {
    grid-area: label;
  }

  .timeline-track {
    grid-area: track;
  }

  .timeline-meta {
    grid-area: meta;
  }
}
</style>
