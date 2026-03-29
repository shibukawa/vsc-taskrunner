<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  paused?: boolean
}>()

const interactiveBlobX = ref(0)
const interactiveBlobY = ref(0)
const interactiveBlobEnabled = ref(false)

let interactiveBlobTargetX = 0
let interactiveBlobTargetY = 0
let interactiveBlobFrame = 0

const interactiveBlobStyle = computed(() => ({
  transform: `translate3d(${interactiveBlobX.value}px, ${interactiveBlobY.value}px, 0)`,
}))

function shouldEnableInteractiveBlob(): boolean {
  if (typeof window === 'undefined') {
    return false
  }
  if (props.paused) {
    return false
  }
  return window.matchMedia('(pointer: fine)').matches &&
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches &&
    window.innerWidth >= 900
}

function animateInteractiveBlob() {
  interactiveBlobX.value += (interactiveBlobTargetX - interactiveBlobX.value) / 18
  interactiveBlobY.value += (interactiveBlobTargetY - interactiveBlobY.value) / 18
  interactiveBlobFrame = window.requestAnimationFrame(animateInteractiveBlob)
}

function handleInteractivePointerMove(event: MouseEvent) {
  interactiveBlobTargetX = event.clientX - (window.innerWidth * 0.5)
  interactiveBlobTargetY = event.clientY - (window.innerHeight * 0.5)
}

function refreshInteractiveBlob() {
  const enabled = shouldEnableInteractiveBlob()
  if (enabled === interactiveBlobEnabled.value) {
    return
  }
  interactiveBlobEnabled.value = enabled
  if (enabled) {
    interactiveBlobTargetX = 0
    interactiveBlobTargetY = 0
    window.addEventListener('mousemove', handleInteractivePointerMove)
    if (interactiveBlobFrame === 0) {
      interactiveBlobFrame = window.requestAnimationFrame(animateInteractiveBlob)
    }
    return
  }
  window.removeEventListener('mousemove', handleInteractivePointerMove)
  if (interactiveBlobFrame !== 0) {
    window.cancelAnimationFrame(interactiveBlobFrame)
    interactiveBlobFrame = 0
  }
  interactiveBlobX.value = 0
  interactiveBlobY.value = 0
  interactiveBlobTargetX = 0
  interactiveBlobTargetY = 0
}

onMounted(() => {
  refreshInteractiveBlob()
  window.addEventListener('resize', refreshInteractiveBlob)
})

watch(() => props.paused, () => {
  refreshInteractiveBlob()
})

onBeforeUnmount(() => {
  window.removeEventListener('mousemove', handleInteractivePointerMove)
  window.removeEventListener('resize', refreshInteractiveBlob)
  if (interactiveBlobFrame !== 0) {
    window.cancelAnimationFrame(interactiveBlobFrame)
  }
})
</script>

<template>
  <div :class="['gradient-bg', { paused: props.paused }]" aria-hidden="true">
    <svg class="goo-svg" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <filter id="goo">
          <feGaussianBlur in="SourceGraphic" stdDeviation="10" result="blur" />
          <feColorMatrix
            in="blur"
            mode="matrix"
            values="1 0 0 0 0  0 1 0 0 0  0 0 1 0 0  0 0 0 18 -8"
            result="goo"
          />
          <feBlend in="SourceGraphic" in2="goo" />
        </filter>
      </defs>
    </svg>

    <div class="gradients-container">
      <div class="g1"></div>
      <div class="g2"></div>
      <div class="g3"></div>
      <div class="g4"></div>
      <div class="g5"></div>
      <div :class="['interactive', { active: interactiveBlobEnabled }]" :style="interactiveBlobStyle"></div>
    </div>
  </div>
</template>

<style>
.gradient-bg {
  position: fixed;
  inset: 0;
  overflow: hidden;
  pointer-events: none;
  background: linear-gradient(40deg, var(--color-bg1), var(--color-bg2));
}

.goo-svg {
  position: fixed;
  inset: 0;
  width: 0;
  height: 0;
}

.gradients-container {
  width: 100%;
  height: 100%;
  filter: url(#goo) blur(40px);
}

.g1,
.g2,
.g3,
.g4,
.g5,
.interactive {
  position: absolute;
  mix-blend-mode: var(--blending);
}

.g1,
.g2,
.g3,
.g4 {
  width: var(--circle-size);
  height: var(--circle-size);
}

.g1 {
  top: calc(50% - var(--circle-size) / 2);
  left: calc(50% - var(--circle-size) / 2);
  background: radial-gradient(circle at center, rgba(var(--color1), 0.8) 0, rgba(var(--color1), 0) 50%) no-repeat;
  transform-origin: center center;
  animation: moveVertical 30s ease infinite;
}

.g2 {
  top: calc(50% - var(--circle-size) / 2);
  left: calc(50% - var(--circle-size) / 2);
  background: radial-gradient(circle at center, rgba(var(--color2), 0.8) 0, rgba(var(--color2), 0) 50%) no-repeat;
  transform-origin: calc(50% - 420px);
  animation: moveInCircle 20s reverse infinite;
}

.g3 {
  top: calc(50% - var(--circle-size) / 2 + 200px);
  left: calc(50% - var(--circle-size) / 2 - 500px);
  background: radial-gradient(circle at center, rgba(var(--color3), 0.8) 0, rgba(var(--color3), 0) 50%) no-repeat;
  transform-origin: calc(50% + 400px);
  animation: moveInCircle 40s linear infinite;
}

.g4 {
  top: calc(50% - var(--circle-size) / 2);
  left: calc(50% - var(--circle-size) / 2);
  background: radial-gradient(circle at center, rgba(var(--color4), 0.78) 0, rgba(var(--color4), 0) 50%) no-repeat;
  transform-origin: calc(50% - 200px);
  animation: moveHorizontal 40s ease infinite;
  opacity: 0.68;
}

.g5 {
  width: calc(var(--circle-size) * 2);
  height: calc(var(--circle-size) * 2);
  top: calc(50% - var(--circle-size));
  left: calc(50% - var(--circle-size));
  background: radial-gradient(circle at center, rgba(var(--color5), 0.72) 0, rgba(var(--color5), 0) 50%) no-repeat;
  transform-origin: calc(50% - 800px) calc(50% + 200px);
  animation: moveInCircle 20s ease infinite;
}

.interactive {
  width: 100%;
  height: 100%;
  top: -50%;
  left: -50%;
  background: radial-gradient(circle at center, rgba(var(--color-interactive), 0.55) 0, rgba(var(--color-interactive), 0) 50%) no-repeat;
  opacity: 0.32;
  transition: opacity 240ms ease;
}

.interactive.active {
  opacity: 0.72;
}

.gradient-bg.paused .g1,
.gradient-bg.paused .g2,
.gradient-bg.paused .g3,
.gradient-bg.paused .g4,
.gradient-bg.paused .g5,
.gradient-bg.paused .interactive {
  animation: none;
}

@keyframes moveInCircle {
  0% {
    transform: rotate(0deg);
  }

  50% {
    transform: rotate(180deg);
  }

  100% {
    transform: rotate(360deg);
  }
}

@keyframes moveVertical {
  0% {
    transform: translateY(-50%);
  }

  50% {
    transform: translateY(50%);
  }

  100% {
    transform: translateY(-50%);
  }
}

@keyframes moveHorizontal {
  0% {
    transform: translateX(-50%) translateY(-10%);
  }

  50% {
    transform: translateX(50%) translateY(10%);
  }

  100% {
    transform: translateX(-50%) translateY(-10%);
  }
}

@media (max-width: 900px) {
  .gradients-container {
    filter: url(#goo) blur(28px);
  }

  .interactive {
    opacity: 0.2;
  }
}

@media (prefers-reduced-motion: reduce) {
  .g1,
  .g2,
  .g3,
  .g4,
  .g5,
  .interactive {
    animation: none;
    transform: none;
  }

  .interactive {
    opacity: 0.25;
  }
}
</style>
