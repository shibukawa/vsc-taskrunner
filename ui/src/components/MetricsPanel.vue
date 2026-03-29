<script setup lang="ts">
import { computed } from 'vue'
import type { MetricsSnapshot } from '../types'
import { formatDateTime, formatBytes } from '../formatters'

const props = defineProps<{
  metrics: MetricsSnapshot | null
}>()

const cpuPercent = computed(() => Math.min(Math.max(props.metrics?.cpu.percent ?? 0, 0), 100))
const cpuNeedleStyle = computed(() => ({
  transform: `rotate(${(cpuPercent.value / 100) * 180 - 90}deg)`,
}))
const memoryPercent = computed(() => Math.min(Math.max(props.metrics?.memory.current.usedPercent ?? 0, 0), 100))
const memoryUsageText = computed(() => {
  const current = props.metrics?.memory.current
  if (!current) {
    return '0 B / 0 B'
  }
  return `${formatBytes(current.usedBytes)} / ${formatBytes(current.totalBytes)}`
})
const memoryChartPoints = computed(() => {
  const history = props.metrics?.memory.history ?? []
  if (history.length === 0) {
    return ''
  }
  return history.map((point, index) => {
    const x = history.length === 1 ? 0 : (index / (history.length - 1)) * 100
    const y = 100 - Math.min(point.usedPercent, 100)
    return `${x},${y}`
  }).join(' ')
})
const storageUsedBytes = computed(() => {
  const storage = props.metrics?.storage
  if (!storage) {
    return 0
  }
  return storage.historyBytes + storage.artifactBytes + storage.worktreeBytes
})
const storageCapacityBytes = computed(() => storageUsedBytes.value + (props.metrics?.storage.freeBytes ?? 0))
const storageUsagePercent = computed(() => {
  if (storageCapacityBytes.value <= 0) {
    return 0
  }
  return Math.min((storageUsedBytes.value / storageCapacityBytes.value) * 100, 100)
})
const storageDonutStyle = computed(() => ({
  background: `conic-gradient(from -90deg, #0f7c6c 0deg ${(storageUsagePercent.value / 100) * 360}deg, rgba(255, 255, 255, 0.24) ${(storageUsagePercent.value / 100) * 360}deg 360deg)`,
}))
</script>

<template>
  <article class="glass-panel metrics-panel floating-metrics">
    <div class="metrics-grid">
      <div class="metric-slot">
        <button
          id="metric-cpu-button"
          type="button"
          class="metric-card cpu"
        >
          <span class="metric-label">CPU</span>
          <div class="metric-hero metric-gauge">
            <div class="gauge-arc"></div>
            <div class="gauge-needle" :style="cpuNeedleStyle"></div>
          </div>
          <strong class="metric-primary">{{ cpuPercent.toFixed(1) }}%</strong>
        </button>
        <div
          id="metric-cpu-popover"
          class="metric-popover glass-panel"
        >
          <span class="metric-popover-label">CPU</span>
          <strong class="metric-popover-value">{{ cpuPercent.toFixed(1) }}%</strong>
          <span>usage {{ cpuPercent.toFixed(1) }}%</span>
          <span>sample {{ metrics?.cpu.timestamp ? formatDateTime(metrics.cpu.timestamp) : 'waiting...' }}</span>
        </div>
      </div>

      <div class="metric-slot">
        <button
          id="metric-memory-button"
          type="button"
          class="metric-card memory"
        >
          <span class="metric-label">Memory</span>
          <svg viewBox="0 0 100 100" preserveAspectRatio="none" class="sparkline metric-hero">
            <polyline v-if="memoryChartPoints" :points="memoryChartPoints" />
          </svg>
          <strong class="metric-primary">{{ memoryPercent.toFixed(1) }}%</strong>
        </button>
        <div
          id="metric-memory-popover"
          class="metric-popover glass-panel"
        >
          <span class="metric-popover-label">Memory</span>
          <strong class="metric-popover-value">{{ memoryPercent.toFixed(1) }}%</strong>
          <span>{{ memoryUsageText }}</span>
          <span>sample {{ metrics?.memory.current.timestamp ? formatDateTime(metrics.memory.current.timestamp) : 'waiting...' }}</span>
        </div>
      </div>

      <div class="metric-slot">
        <button
          id="metric-storage-button"
          type="button"
          class="metric-card storage"
        >
          <span class="metric-label">Storage</span>
          <div class="metric-hero metric-donut" :style="storageDonutStyle">
            <div class="metric-donut-inner">
              <span>{{ storageUsagePercent.toFixed(0) }}%</span>
            </div>
          </div>
          <strong class="metric-primary">{{ formatBytes(storageUsedBytes) }}</strong>
        </button>
        <div
          id="metric-storage-popover"
          class="metric-popover metric-popover-storage glass-panel"
        >
          <span class="metric-popover-label">Storage</span>
          <strong class="metric-popover-value">{{ formatBytes(storageUsedBytes) }}</strong>
          <span>history {{ formatBytes(metrics?.storage.historyBytes) }}</span>
          <span>artifacts {{ formatBytes(metrics?.storage.artifactBytes) }}</span>
          <span>worktrees {{ formatBytes(metrics?.storage.worktreeBytes) }}</span>
          <span>free {{ formatBytes(metrics?.storage.freeBytes) }}</span>
        </div>
      </div>
    </div>
  </article>
</template>

<style>
.floating-metrics {
  position: absolute;
  top: 14px;
  right: 14px;
  width: 262px;
  padding: 7px 8px 8px;
  z-index: 10;
}

.metrics-panel {
  padding: 7px 8px 8px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.18), rgba(255, 255, 255, 0.08)),
    rgba(255, 255, 255, 0.12);
}

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
}

.metric-slot {
  position: relative;
}

.metric-card {
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 3px;
  align-items: center;
  border-radius: 13px;
  padding: 5px 6px 6px;
  min-height: 98px;
  text-align: left;
  color: var(--ink);
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  box-shadow: none;
  transition:
    transform 180ms ease,
    border-color 180ms ease,
    background 180ms ease,
    box-shadow 180ms ease;
}

.metric-card:hover,
.metric-card:focus-visible,
.metric-card[aria-expanded='true'] {
  transform: translateY(-1px);
  border-color: rgba(255, 255, 255, 0.2);
  background: rgba(255, 255, 255, 0.07);
  box-shadow: 0 6px 18px rgba(34, 42, 70, 0.08);
}

.metric-card:focus-visible {
  outline: 2px solid rgba(15, 124, 108, 0.34);
  outline-offset: 2px;
}

.metric-primary {
  font-size: 0.9rem;
  line-height: 1.05;
  letter-spacing: -0.02em;
  display: block;
  text-align: center;
}

.metric-hero {
  align-self: center;
  justify-self: center;
}

.metric-label {
  display: block;
  min-height: 1.05em;
  font-size: 0.72rem;
  line-height: 1;
}

.sparkline {
  width: 100%;
  height: 34px;
  border-radius: 10px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.1), rgba(255, 255, 255, 0.03)),
    rgba(255, 255, 255, 0.08);
}

.sparkline polyline {
  fill: none;
  stroke: #1a8f7f;
  stroke-width: 2.6;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.metric-popover {
  position: absolute;
  top: calc(100% + 10px);
  left: 50%;
  translate: -50% 0;
  display: grid;
  width: min(228px, calc(100vw - 24px));
  margin: 0;
  padding: 12px 14px;
  border-radius: 18px;
  color: var(--ink);
  font-size: 0.8rem;
  line-height: 1.25;
  background: rgba(247, 250, 250, 0.96);
  border: 1px solid rgba(255, 255, 255, 0.7);
  box-shadow: 0 12px 28px rgba(33, 42, 57, 0.14);
  gap: 8px;
  align-content: start;
  justify-items: start;
  height: auto;
  min-height: 0;
  max-height: none;
  overflow: hidden;
  opacity: 0;
  pointer-events: none;
  transition: opacity 140ms ease, transform 140ms ease;
  z-index: 20;
}

.metric-slot:hover .metric-popover,
.metric-slot:focus-within .metric-popover {
  opacity: 1;
  pointer-events: auto;
}

.metric-popover-storage {
  left: auto;
  right: 0;
  translate: 0 0;
}

.metric-popover-label {
  color: rgba(76, 86, 102, 0.92);
}

.metric-popover-value {
  font-size: 0.96rem;
  line-height: 1.1;
}

.metric-gauge {
  position: relative;
  width: 48px;
  height: 29px;
  display: grid;
  align-items: end;
  justify-items: center;
}

.gauge-arc {
  position: absolute;
  inset: 0;
  border-top-left-radius: 48px;
  border-top-right-radius: 48px;
  border: 5px solid rgba(255, 255, 255, 0.24);
  border-bottom: 0;
  background: linear-gradient(90deg, rgba(74, 214, 161, 0.12), rgba(15, 124, 108, 0.28));
}

.gauge-needle {
  position: absolute;
  bottom: 0;
  left: 50%;
  width: 2px;
  height: 19px;
  border-radius: 999px;
  background: linear-gradient(180deg, #0f7c6c, #173532);
  transform-origin: 50% calc(100% - 2px);
  transition: transform 220ms ease;
  margin-left: -1px;
}

.gauge-needle::after {
  content: '';
  position: absolute;
  bottom: -2px;
  left: 50%;
  width: 7px;
  height: 7px;
  border-radius: 999px;
  background: #173532;
  transform: translateX(-50%);
  box-shadow: 0 0 0 2px rgba(255, 255, 255, 0.42);
}

.metric-gauge strong {
  display: none;
}

.metric-donut {
  position: relative;
  width: 44px;
  height: 44px;
  border-radius: 50%;
}

.metric-donut-inner {
  position: absolute;
  inset: 7px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: rgba(247, 250, 250, 0.92);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.72);
}

.metric-donut-inner span {
  font-size: 0.72rem;
  font-weight: 700;
}

@media (max-width: 1260px) {
  .floating-metrics {
    position: static;
    width: auto;
  }

  .metrics-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .floating-metrics {
    display: none;
  }

  .metric-popover {
    width: min(248px, calc(100vw - 24px));
  }
}
</style>
