<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import type { ArtifactRule, Branch, BranchTask, PreRunTask, RunMeta } from '../types'
import { formatDateTime, formatRelativeTime } from '../formatters'

const props = defineProps<{
  branches: Branch[]
  tasks: BranchTask[]
  selectedBranch: string | null
  selectedTask: string | null
  lastRunByTask: Record<string, RunMeta>
  filteredRuns: RunMeta[]
  currentRunKey: string | null
  loading: boolean
  canRun: boolean
}>()

const emit = defineEmits<{
  'toggle-branch': [shortName: string]
  'toggle-task': [label: string]
  'fetch-branches': []
  'run-task': []
  'select-run': [run: RunMeta]
}>()

type BranchPopoverState = {
  branch: Branch
  anchor: HTMLElement
}

type BranchPopoverPosition = {
  top: number
  left: number
}

const branchPopover = ref<BranchPopoverState | null>(null)
const branchPopoverPosition = ref<BranchPopoverPosition>({ top: 0, left: 0 })
const branchPopoverElement = ref<HTMLElement | null>(null)

type TaskPopoverState = {
  task: BranchTask
  anchor: HTMLElement
}

const taskPopover = ref<TaskPopoverState | null>(null)
const taskPopoverPosition = ref<BranchPopoverPosition>({ top: 0, left: 0 })
const taskPopoverElement = ref<HTMLElement | null>(null)

let branchPopoverFrame: number | null = null
let taskPopoverFrame: number | null = null

function statusEmoji(status?: RunMeta['status']): string {
  switch (status) {
    case 'success':
      return '🟢'
    case 'failed':
      return '🔴'
    case 'running':
      return '🟡'
    default:
      return '⚪'
  }
}

function latestRunForTask(task: BranchTask): RunMeta | null {
  return props.lastRunByTask[task.label] ?? null
}

function taskRowTitle(task: BranchTask): string {
  return task.label
}

function taskRowSubtitle(task: BranchTask): string {
  const lastRun = latestRunForTask(task)
  if (!lastRun) {
    return 'no exec'
  }
  return formatRelativeTime(lastRun.startTime)
}

function taskRowSubtitleTitle(task: BranchTask): string | undefined {
  const lastRun = latestRunForTask(task)
  if (!lastRun) {
    return undefined
  }
  return formatDateTime(lastRun.startTime)
}

function taskHasArtifacts(task: BranchTask): boolean {
  return Boolean(latestRunForTask(task)?.hasArtifacts)
}

function runRowTitle(run: RunMeta): string {
  return `#${run.runNumber} ${run.taskLabel} (${run.branch})`
}

function updateBranchPopoverPosition(): void {
  if (typeof window === 'undefined' || !branchPopover.value) {
    return
  }

  branchPopoverPosition.value = computePopoverPosition(branchPopover.value.anchor, branchPopoverElement.value, 228)
}

function computePopoverPosition(anchor: HTMLElement, element: HTMLElement | null, fallbackWidth: number): BranchPopoverPosition {
  if (typeof window === 'undefined') {
    return { top: 0, left: 0 }
  }

  const gap = 8
  const viewportMargin = 12
  const rect = anchor.getBoundingClientRect()
  const popoverWidth = element?.offsetWidth ?? Math.min(fallbackWidth, window.innerWidth - viewportMargin * 2)
  const popoverHeight = element?.offsetHeight ?? 64

  const maxLeft = Math.max(viewportMargin, window.innerWidth - popoverWidth - viewportMargin)
  const left = Math.min(Math.max(rect.left + 28, viewportMargin), maxLeft)

  const preferredTop = rect.bottom + gap
  const wouldOverflowBottom = preferredTop + popoverHeight > window.innerHeight - viewportMargin
  const top = wouldOverflowBottom ? rect.top - popoverHeight - gap : preferredTop

  return {
    top: Math.min(Math.max(top, viewportMargin), Math.max(viewportMargin, window.innerHeight - popoverHeight - viewportMargin)),
    left,
  }
}

function scheduleBranchPopoverPositionUpdate(): void {
  if (typeof window === 'undefined' || !branchPopover.value) {
    return
  }
  if (branchPopoverFrame !== null) {
    window.cancelAnimationFrame(branchPopoverFrame)
  }
  branchPopoverFrame = window.requestAnimationFrame(() => {
    branchPopoverFrame = null
    updateBranchPopoverPosition()
  })
}

function updateTaskPopoverPosition(): void {
  if (typeof window === 'undefined' || !taskPopover.value) {
    return
  }

  taskPopoverPosition.value = computePopoverPosition(taskPopover.value.anchor, taskPopoverElement.value, 360)
}

function scheduleTaskPopoverPositionUpdate(): void {
  if (typeof window === 'undefined' || !taskPopover.value) {
    return
  }
  if (taskPopoverFrame !== null) {
    window.cancelAnimationFrame(taskPopoverFrame)
  }
  taskPopoverFrame = window.requestAnimationFrame(() => {
    taskPopoverFrame = null
    updateTaskPopoverPosition()
  })
}

function showBranchPopover(branch: Branch, event: MouseEvent | FocusEvent): void {
  const anchor = event.currentTarget
  if (!(anchor instanceof HTMLElement)) {
    return
  }
  branchPopover.value = { branch, anchor }
}

function hideBranchPopover(branchShortName: string): void {
  if (branchPopover.value?.branch.shortName === branchShortName) {
    branchPopover.value = null
  }
}

function showTaskPopover(task: BranchTask, event: MouseEvent | FocusEvent): void {
  const anchor = event.currentTarget
  if (!(anchor instanceof HTMLElement)) {
    return
  }
  taskPopover.value = { task, anchor }
}

function hideTaskPopover(taskLabel: string): void {
  if (taskPopover.value?.task.label === taskLabel) {
    taskPopover.value = null
  }
}

function taskPopoverDependsOrder(task: BranchTask): string {
  return task.dependsOrder || 'parallel'
}

function taskPopoverBool(value: boolean | undefined): string {
  return value ? 'true' : 'false'
}

function formatArtifactRule(rule: ArtifactRule): string {
  const details = [rule.path]
  if (rule.format) {
    details.push(`format: ${rule.format}`)
  }
  if (rule.nameTemplate) {
    details.push(`name: ${rule.nameTemplate}`)
  }
  return details.join(' / ')
}

function formatPreRunTask(task: PreRunTask): string {
  const parts = [task.command]
  if (task.args?.length) {
    parts.push(task.args.join(' '))
  }
  if (task.cwd) {
    parts.push(`cwd: ${task.cwd}`)
  }
  if (task.shell?.executable) {
    parts.push(`shell: ${task.shell.executable}`)
  }
  return parts.join(' / ')
}

watch(branchPopover, async (value) => {
  if (typeof window === 'undefined') {
    return
  }

  if (value) {
    await nextTick()
    updateBranchPopoverPosition()
    window.addEventListener('resize', scheduleBranchPopoverPositionUpdate)
    window.addEventListener('scroll', scheduleBranchPopoverPositionUpdate, true)
    return
  }

  window.removeEventListener('resize', scheduleBranchPopoverPositionUpdate)
  window.removeEventListener('scroll', scheduleBranchPopoverPositionUpdate, true)
})

watch(taskPopover, async (value) => {
  if (typeof window === 'undefined') {
    return
  }

  if (value) {
    await nextTick()
    updateTaskPopoverPosition()
    window.addEventListener('resize', scheduleTaskPopoverPositionUpdate)
    window.addEventListener('scroll', scheduleTaskPopoverPositionUpdate, true)
    return
  }

  window.removeEventListener('resize', scheduleTaskPopoverPositionUpdate)
  window.removeEventListener('scroll', scheduleTaskPopoverPositionUpdate, true)
})

watch(branchPopoverElement, () => {
  if (branchPopover.value) {
    scheduleBranchPopoverPositionUpdate()
  }
})

watch(taskPopoverElement, () => {
  if (taskPopover.value) {
    scheduleTaskPopoverPositionUpdate()
  }
})

watch(
  () => props.branches,
  (branches) => {
    if (!branchPopover.value) {
      return
    }
    const current = branches.find((branch) => branch.shortName === branchPopover.value?.branch.shortName)
    if (!current) {
      branchPopover.value = null
      return
    }
    branchPopover.value = {
      ...branchPopover.value,
      branch: current,
    }
    scheduleBranchPopoverPositionUpdate()
  },
  { deep: true },
)

watch(
  () => props.tasks,
  (tasks) => {
    if (!taskPopover.value) {
      return
    }
    const current = tasks.find((task) => task.label === taskPopover.value?.task.label)
    if (!current) {
      taskPopover.value = null
      return
    }
    taskPopover.value = {
      ...taskPopover.value,
      task: current,
    }
    scheduleTaskPopoverPositionUpdate()
  },
  { deep: true },
)

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') {
    window.removeEventListener('resize', scheduleBranchPopoverPositionUpdate)
    window.removeEventListener('scroll', scheduleBranchPopoverPositionUpdate, true)
    window.removeEventListener('resize', scheduleTaskPopoverPositionUpdate)
    window.removeEventListener('scroll', scheduleTaskPopoverPositionUpdate, true)
    if (branchPopoverFrame !== null) {
      window.cancelAnimationFrame(branchPopoverFrame)
    }
    if (taskPopoverFrame !== null) {
      window.cancelAnimationFrame(taskPopoverFrame)
    }
  }
})
</script>

<template>
  <article class="glass-panel task-selection-panel">
    <div class="selection-grid">
      <div class="selection-branches">
        <div class="panel-head">
          <h2 id="branches-list-label">Branches</h2>
          <div class="panel-tools">
            <button class="ghost compact-button panel-action panel-action-small" :disabled="loading" @click="emit('fetch-branches')">Fetch</button>
          </div>
        </div>

        <div class="branch-list" role="listbox" aria-labelledby="branches-list-label" aria-orientation="vertical">
          <button
            v-for="branch in branches"
            :key="branch.shortName"
            type="button"
            :class="['list-row', 'branch-row', { active: branch.shortName === selectedBranch }]"
            role="option"
            :aria-selected="branch.shortName === selectedBranch"
            :aria-describedby="branchPopover?.branch.shortName === branch.shortName ? 'branch-popover-tooltip' : undefined"
            @click="emit('toggle-branch', branch.shortName)"
            @mouseenter="showBranchPopover(branch, $event)"
            @mouseleave="hideBranchPopover(branch.shortName)"
            @focus="showBranchPopover(branch, $event)"
            @blur="hideBranchPopover(branch.shortName)"
          >
            <span class="branch-row-main">
              <span class="branch-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M7 6a3 3 0 1 1 0-6 3 3 0 0 1 0 6Z" transform="translate(0 1.5)" />
                  <path d="M17 24a3 3 0 1 1 0-6 3 3 0 0 1 0 6Z" transform="translate(0 -1.5)" />
                  <path d="M17 12a3 3 0 1 1 0-6 3 3 0 0 1 0 6Z" />
                  <path d="M10 4.5h3a4 4 0 0 1 4 4" />
                  <path d="M7 7.5v9a6 6 0 0 0 6 6h1" />
                </svg>
              </span>
              <span class="branch-row-copy">
                <strong class="row-title branch-title">{{ branch.shortName }}</strong>
                <span class="branch-time">{{ formatRelativeTime(branch.commitDate) }}</span>
              </span>
            </span>
          </button>
        </div>
      </div>

      <div class="selection-tasks">
        <div class="panel-head">
          <h2 id="tasks-list-label">Tasks</h2>
          <div class="panel-tools">
            <button class="primary compact-button panel-action panel-action-small" :disabled="loading || !selectedBranch || !selectedTask || !canRun" @click="emit('run-task')">Run</button>
          </div>
        </div>

        <template v-if="selectedBranch">
          <div v-if="tasks.length" class="task-list" role="listbox" aria-labelledby="tasks-list-label" aria-orientation="vertical">
            <button
              v-for="task in tasks"
              :key="task.label"
              type="button"
              :class="['list-row', 'task-row', { active: task.label === selectedTask }]"
              role="option"
              :aria-selected="task.label === selectedTask"
              :aria-describedby="taskPopover?.task.label === task.label ? 'task-popover-tooltip' : undefined"
              @click="emit('toggle-task', task.label)"
              @mouseenter="showTaskPopover(task, $event)"
              @mouseleave="hideTaskPopover(task.label)"
              @focus="showTaskPopover(task, $event)"
              @blur="hideTaskPopover(task.label)"
            >
              <span class="row-status" aria-hidden="true">{{ statusEmoji(latestRunForTask(task)?.status) }}</span>
              <span class="row-copy">
                <strong class="row-title">{{ taskRowTitle(task) }}</strong>
                <span class="row-subline">
                  <span class="row-time" :title="taskRowSubtitleTitle(task)">{{ taskRowSubtitle(task) }}</span>
                  <span v-if="taskHasArtifacts(task)" class="artifact-marker" aria-label="artifacts">📁</span>
                </span>
              </span>
            </button>
          </div>
          <p v-else class="empty-copy">No tasks were found for this branch.</p>
        </template>
      </div>

      <div class="selection-runs">
        <div class="panel-head">
          <h2 id="history-list-label">History</h2>
        </div>

        <div v-if="filteredRuns.length" class="run-list" role="listbox" aria-labelledby="history-list-label" aria-orientation="vertical">
          <button
            v-for="run in filteredRuns"
            :key="run.runKey"
            type="button"
            :class="['list-row', 'run-row', { active: run.runKey === currentRunKey }]"
            role="option"
            :aria-selected="run.runKey === currentRunKey"
            @click="emit('select-run', run)"
          >
            <span class="row-status" aria-hidden="true">{{ statusEmoji(run.status) }}</span>
            <span class="row-copy">
              <strong class="row-title">{{ runRowTitle(run) }}</strong>
              <span class="row-subline">
                <span class="row-time" :title="formatDateTime(run.startTime)">{{ formatRelativeTime(run.startTime) }}</span>
                <span v-if="run.hasArtifacts" class="artifact-marker" aria-label="artifacts">📁</span>
              </span>
            </span>
          </button>
        </div>
        <p v-else class="empty-copy">No runs are available to display.</p>
      </div>
    </div>
  </article>

  <Teleport to="body">
    <span
      v-if="branchPopover"
      id="branch-popover-tooltip"
      ref="branchPopoverElement"
      class="branch-popover branch-popover-floating"
      role="tooltip"
      :style="{
        top: `${branchPopoverPosition.top}px`,
        left: `${branchPopoverPosition.left}px`,
      }"
    >
      <span class="branch-popover-label">{{ formatDateTime(branchPopover.branch.commitDate) }}</span>
      <strong>{{ branchPopover.branch.commitHash.slice(0, 7) }}</strong>
    </span>

    <section
      v-if="taskPopover"
      id="task-popover-tooltip"
      ref="taskPopoverElement"
      class="task-popover branch-popover-floating"
      role="tooltip"
      :style="{
        top: `${taskPopoverPosition.top}px`,
        left: `${taskPopoverPosition.left}px`,
      }"
    >
      <div class="task-popover-head">
        <span class="branch-popover-label">Task</span>
        <strong>{{ taskPopover.task.label }}</strong>
      </div>
      <dl class="task-popover-list">
        <template v-if="taskPopover.task.type">
          <dt>Type</dt>
          <dd>{{ taskPopover.task.type }}</dd>
        </template>
        <template v-if="taskPopover.task.group">
          <dt>Group</dt>
          <dd>{{ taskPopover.task.group }}</dd>
        </template>
        <template v-if="taskPopover.task.dependsOn?.length">
          <dt>Depends On</dt>
          <dd>{{ taskPopover.task.dependsOn.join(', ') }}</dd>
        </template>
        <template v-if="taskPopover.task.dependsOn?.length">
          <dt>Depends Order</dt>
          <dd>{{ taskPopoverDependsOrder(taskPopover.task) }}</dd>
        </template>
        <template v-if="taskPopover.task.resolvedTaskLabels?.length">
          <dt>Run Tasks</dt>
          <dd>
            <ul class="task-popover-bullets">
              <li v-for="label in taskPopover.task.resolvedTaskLabels" :key="label">{{ label }}</li>
            </ul>
          </dd>
        </template>
        <template v-if="taskPopover.task.preRunTasks?.length">
          <dt>Pre Run</dt>
          <dd>
            <ul class="task-popover-bullets">
              <li v-for="(item, index) in taskPopover.task.preRunTasks" :key="`${taskPopover.task.label}-pre-${index}`">{{ formatPreRunTask(item) }}</li>
            </ul>
          </dd>
        </template>
        <dt>Worktree Disabled</dt>
        <dd>{{ taskPopoverBool(taskPopover.task.worktree?.disabled) }}</dd>
        <template v-if="taskPopover.task.artifacts?.length">
          <dt>Artifacts</dt>
          <dd>
            <ul class="task-popover-bullets">
              <li v-for="(artifact, index) in taskPopover.task.artifacts" :key="`${taskPopover.task.label}-artifact-${index}`">{{ formatArtifactRule(artifact) }}</li>
            </ul>
          </dd>
        </template>
        <template v-if="taskPopover.task.taskFilePath">
          <dt>Task File</dt>
          <dd class="task-popover-path">{{ taskPopover.task.taskFilePath }}</dd>
        </template>
      </dl>
    </section>
  </Teleport>
</template>
<style>
.task-selection-panel {
  grid-area: selection;
  padding: 18px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.selection-grid {
  display: grid;
  grid-template-areas:
    "branches runs"
    "tasks    runs";
  grid-template-columns: minmax(0, 1.3fr) minmax(0, 1.1fr);
  grid-template-rows: auto 1fr;
  gap: 12px;
  flex: 1;
  min-height: 0;
}

.selection-branches {
  grid-area: branches;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.selection-tasks {
  grid-area: tasks;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.selection-runs {
  grid-area: runs;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.branch-list,
.task-list,
.run-list {
  gap: 4px;
  padding-right: 2px;
  align-content: start;
  grid-auto-rows: max-content;
}

.list-row {
  appearance: none;
  -webkit-appearance: none;
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 10px;
  border: 1px solid transparent;
  border-radius: 12px;
  background: transparent;
  box-shadow: none;
  color: var(--ink);
  text-align: left;
  transition:
    background 160ms ease,
    border-color 160ms ease,
    color 160ms ease,
    transform 160ms ease;
}

.list-row:hover,
.list-row:focus-visible {
  background: rgba(255, 255, 255, 0.12);
  border-color: rgba(255, 255, 255, 0.16);
}

.list-row.active {
  background:
    linear-gradient(180deg, rgba(28, 34, 45, 0.92), rgba(17, 21, 30, 0.88)),
    rgba(16, 19, 25, 0.9);
  border-color: rgba(255, 255, 255, 0.12);
  color: #f7fbfc;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08),
    0 8px 18px rgba(10, 12, 18, 0.18);
}

.list-row:focus-visible {
  outline: 2px solid rgba(255, 255, 255, 0.35);
  outline-offset: 2px;
}

.row-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
  flex: 1;
}

.row-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'Hiragino Sans', 'Yu Gothic', 'Noto Sans JP', sans-serif;
  font-size: 0.86rem;
  font-weight: 600;
  letter-spacing: 0.01em;
}

.row-copy {
  display: grid;
  gap: 2px;
  min-width: 0;
}

.row-subline {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--muted);
  font-size: 0.8rem;
}

.list-row.active .row-subline,
.list-row.active .branch-time {
  color: rgba(247, 251, 252, 0.72);
}

.row-time {
  white-space: nowrap;
}

.row-time-empty {
  opacity: 0.72;
}

.row-status,
.artifact-marker {
  flex: none;
}

.branch-row {
  position: relative;
  display: block;
  min-height: 54px;
}

.branch-row-main {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.branch-icon {
  width: 18px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: currentColor;
  opacity: 0.82;
}

.branch-icon svg {
  width: 18px;
  height: 18px;
}

.branch-title {
  display: block;
  max-width: 100%;
}

.branch-time {
  font-size: 0.78rem;
  color: rgba(76, 86, 102, 0.92);
  white-space: nowrap;
}

.branch-popover {
  display: grid;
  gap: 2px;
  padding: 8px 10px;
  width: min(228px, calc(100vw - 24px));
  border-radius: 10px;
  color: var(--ink);
  background: rgba(247, 250, 250, 0.96);
  border: 1px solid rgba(255, 255, 255, 0.7);
  box-shadow: 0 12px 28px rgba(33, 42, 57, 0.14);
  pointer-events: none;
}

.task-popover {
  display: grid;
  gap: 10px;
  padding: 12px 14px;
  width: min(360px, calc(100vw - 24px));
  border-radius: 12px;
  color: var(--ink);
  background: rgba(247, 250, 250, 0.98);
  border: 1px solid rgba(255, 255, 255, 0.72);
  box-shadow: 0 14px 30px rgba(33, 42, 57, 0.16);
  pointer-events: none;
}

.branch-popover-label {
  font-size: 0.72rem;
  color: rgba(76, 86, 102, 0.92);
}

.branch-popover-floating {
  position: fixed;
  z-index: 30;
}

.task-popover-head {
  display: grid;
  gap: 2px;
}

.task-popover-list {
  margin: 0;
  display: grid;
  grid-template-columns: minmax(88px, auto) minmax(0, 1fr);
  gap: 6px 10px;
  align-items: start;
  font-size: 0.8rem;
}

.task-popover-list dt {
  color: rgba(76, 86, 102, 0.92);
  margin: 0;
}

.task-popover-list dd {
  margin: 0;
  min-width: 0;
  word-break: break-word;
}

.task-popover-bullets {
  margin: 0;
  padding-left: 16px;
}

.task-popover-bullets li + li {
  margin-top: 2px;
}

.task-popover-path {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 0.75rem;
}

.task-row,
.run-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  min-height: 54px;
}

.task-row .row-status,
.run-row .row-status {
  align-self: center;
}

@media (max-width: 1260px) {
  .selection-grid {
    grid-template-areas:
      "branches"
      "tasks"
      "runs";
    grid-template-columns: 1fr;
    grid-template-rows: auto auto auto;
  }

  .task-selection-panel {
    overflow: visible;
  }

  .selection-branches,
  .selection-tasks,
  .selection-runs {
    overflow: visible;
  }

  .branch-list,
  .task-list {
    flex: none;
    overflow-y: visible;
  }

  .run-list {
    flex: none;
    max-height: 400px;
  }
}

@media (max-width: 720px) {
  .task-selection-panel {
    padding: 16px;
    border-radius: var(--panel-radius);
  }

  .list-row,
  .branch-row {
    align-items: start;
  }

  .row-copy,
  .branch-row-copy {
    justify-items: start;
  }

  .branch-popover {
    width: min(228px, calc(100vw - 24px));
  }

  .task-popover {
    width: min(360px, calc(100vw - 24px));
  }

  .row-title {
    white-space: normal;
  }
}
</style>
