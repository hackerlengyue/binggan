<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from "vue";
import { t } from "@/i18n";
import { reducedMotion } from "@/lib/app-ready";

const props = defineProps<{
  phase: string;
  leaving: boolean;
}>();
const emit = defineEmits<{ done: [] }>();
const root = ref<HTMLElement>();
const mark = ref<HTMLElement>();
const contact = ref<HTMLElement>();
const stage = ref<HTMLElement>();
const version = __APP_VERSION__;
const name = ref<HTMLElement>();
const detail = ref<HTMLElement>();
// Show the startup phase only when services take longer than usual.
const slow = ref(false);
onMounted(() => {
  window.setTimeout(() => (slow.value = true), 2600);
});

function fadeOut() {
  const animation = root.value?.animate([{ opacity: 1 }, { opacity: 0 }], {
    duration: reducedMotion() ? 160 : 320,
    easing: "ease-out",
    fill: "forwards",
  });
  if (animation)
    animation.finished.then(
      () => emit("done"),
      () => emit("done"),
    );
  else emit("done");
}

// Exit: a calm hand-over. The figure and name drift up and fade, the backdrop
// fades a beat later, and the workspace beneath settles from a hair smaller to
// full size, so nothing large sweeps across the screen.
async function leave() {
  await nextTick();
  const shell = document.querySelector<HTMLElement>(".desktop-workspace");
  if (reducedMotion() || !stage.value || !root.value) {
    fadeOut();
    return;
  }
  stage.value.animate(
    [
      { opacity: 1, transform: "none" },
      { opacity: 0, transform: "translateY(-12px) scale(0.98)" },
    ],
    { duration: 520, easing: "cubic-bezier(0.4, 0, 0.2, 1)", fill: "forwards" },
  );
  shell?.animate([{ transform: "scale(0.985)" }, { transform: "none" }], {
    duration: 760,
    delay: 220,
    easing: "cubic-bezier(0.22, 1, 0.36, 1)",
    fill: "backwards",
  });
  const fade = root.value.animate([{ opacity: 1 }, { opacity: 0 }], {
    duration: 620,
    delay: 240,
    easing: "cubic-bezier(0.4, 0, 0.2, 1)",
    fill: "forwards",
  });
  fade.finished.then(
    () => emit("done"),
    () => emit("done"),
  );
}
watch(
  () => props.leaving,
  (value) => {
    if (value) void leave();
  },
);
</script>

<template>
  <div
    ref="root"
    class="splash"
    data-window-drag
    role="status"
    aria-live="polite"
  >
    <div ref="stage" class="splash-stage">
      <div class="splash-figure">
        <span ref="contact" class="splash-contact" aria-hidden="true" />
        <!-- The mascot comes alive over the flat artwork: eyelids, blush and
             pearl glints are positioned on the drawing's own features. -->
        <div ref="mark" class="splash-mark">
          <img
            src="/binggan-logo.png"
            alt=""
            width="88"
            height="88"
            draggable="false"
          />
          <span class="lid lid-left" aria-hidden="true" />
          <span class="lid lid-right" aria-hidden="true" />
          <span class="blush blush-left" aria-hidden="true" />
          <span class="blush blush-right" aria-hidden="true" />
          <span class="glint glint-left" aria-hidden="true" />
          <span class="glint glint-right" aria-hidden="true" />
        </div>
      </div>
      <div class="splash-name-mask">
        <h1 ref="name" class="splash-name">{{ t("饼干大小姐") }}</h1>
      </div>
      <p class="splash-version">v{{ version }}</p>
      <div
        ref="detail"
        class="splash-detail"
        :class="{ shown: slow && !leaving && phase !== '准备就绪' }"
      >
        <p>{{ t(phase) }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
@font-face {
  font-family: "LXGW WenKai Splash";
  src: url("/fonts/lxgw-wenkai/splash-regular.woff2") format("woff2");
  font-style: normal;
  font-weight: 400;
  font-display: block;
}

@font-face {
  font-family: "LXGW WenKai Splash";
  src: url("/fonts/lxgw-wenkai/splash-medium.woff2") format("woff2");
  font-style: normal;
  font-weight: 500;
  font-display: block;
}

/* The native window is #fafafa until the page paints; the splash uses the same
   colour so launching never flashes. */
.splash {
  position: fixed;
  inset: 0;
  z-index: 200;
  font-family: "LXGW WenKai Splash", serif;
  display: grid;
  place-items: center;
  background: #fafafa;
  color: oklch(0.21 0 0);
  --skin: rgb(252 238 221);
  --blush: rgb(247 168 160);
}
.splash-stage {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
}
.splash-figure {
  position: relative;
  width: 88px;
  height: 88px;
}

/* 1. The tile drops in and settles with a little squash, like a biscuit set
      down on a table; its contact shadow tightens as it lands. */
.splash-mark {
  position: relative;
  width: 88px;
  height: 88px;
  border-radius: 21px;
  overflow: hidden;
  box-shadow:
    0 1px 2px oklch(0 0 0 / 10%),
    0 12px 24px -12px oklch(0 0 0 / 35%);
  transform-origin: 50% 100%;
  will-change: transform;
  animation: drop 960ms linear both;
}
.splash-mark img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.splash-contact {
  position: absolute;
  left: 50%;
  bottom: -6px;
  width: 64px;
  height: 10px;
  margin-left: -32px;
  border-radius: 50%;
  background: radial-gradient(closest-side, oklch(0 0 0 / 22%), transparent);
  animation: contact 960ms linear both;
}
@keyframes drop {
  0% {
    opacity: 0;
    transform: translateY(-34px) scale(0.86);
    animation-timing-function: cubic-bezier(0.5, 0, 0.9, 0.6);
  }
  48% {
    opacity: 1;
    transform: translateY(0) scale(1.05, 0.93);
    animation-timing-function: cubic-bezier(0.2, 0.7, 0.3, 1);
  }
  70% {
    transform: translateY(-3px) scale(0.98, 1.03);
    animation-timing-function: cubic-bezier(0.4, 0, 0.3, 1);
  }
  100% {
    opacity: 1;
    transform: none;
  }
}
@keyframes contact {
  0% {
    opacity: 0;
    transform: scale(0.4);
  }
  48% {
    opacity: 1;
    transform: scale(1.1);
  }
  70% {
    transform: scale(0.95);
  }
  100% {
    opacity: 1;
    transform: none;
  }
}

/* 2. The name rises from behind a mask, the way an editorial title is set. */
.splash-name-mask {
  margin-top: 22px;
  overflow: hidden;
  padding: 0 4px;
}
.splash-name {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  line-height: 1.35;
  letter-spacing: 0.04em;
  animation: rise 800ms cubic-bezier(0.16, 1, 0.3, 1) 520ms both;
}
@keyframes rise {
  from {
    transform: translateY(105%);
  }
}

.splash-version {
  margin: 6px 0 0;
  font-size: 12px;
  letter-spacing: 0.06em;
  font-variant-numeric: tabular-nums;
  color: oklch(0.6 0 0);
  animation: version 700ms ease-out 900ms both;
}
@keyframes version {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
}

/* 3. She blinks, her cheeks warm and the pearls catch the light; while startup
      takes longer she keeps blinking now and then. */
.lid {
  position: absolute;
  width: 10%;
  height: 17.5%;
  margin: -8.75% 0 0 -5%;
  border-radius: 45%;
  background: var(--skin);
  transform-origin: 50% 0;
  transform: rotate(16deg) scaleY(0);
  animation: blink 4200ms ease-in-out 1150ms infinite;
}
.lid-left {
  left: 21.3%;
  top: 65.1%;
}
.lid-right {
  left: 43.7%;
  top: 71.1%;
}
@keyframes blink {
  0%,
  100% {
    transform: rotate(16deg) scaleY(0);
  }
  4% {
    transform: rotate(16deg) scaleY(1);
  }
  8% {
    transform: rotate(16deg) scaleY(0);
  }
  11% {
    transform: rotate(16deg) scaleY(0);
  }
  14% {
    transform: rotate(16deg) scaleY(1);
  }
  18% {
    transform: rotate(16deg) scaleY(0);
  }
}
.blush {
  position: absolute;
  width: 11%;
  height: 7.5%;
  border-radius: 50%;
  background: var(--blush);
  mix-blend-mode: multiply;
  opacity: 0;
  transform: translate(-50%, -50%) scale(0.6);
  animation: blush 1250ms cubic-bezier(0.2, 0.8, 0.2, 1) 1450ms both;
}
.blush-left {
  left: 11.2%;
  top: 71.4%;
}
.blush-right {
  left: 49.4%;
  top: 81.2%;
}
@keyframes blush {
  45% {
    opacity: 0.75;
    transform: translate(-50%, -50%) scale(1.15);
  }
  100% {
    opacity: 0.45;
    transform: translate(-50%, -50%) scale(1);
  }
}
.glint {
  position: absolute;
  width: 7px;
  height: 7px;
  margin: -5px 0 0 -5px;
  background:
    linear-gradient(white, white) center / 1.5px 100% no-repeat,
    linear-gradient(white, white) center / 100% 1.5px no-repeat;
  opacity: 0;
  animation: glint 860ms ease-out both;
}
.glint-left {
  left: 2.6%;
  top: 67.3%;
  animation-delay: 1560ms;
}
.glint-right {
  left: 58.4%;
  top: 82.2%;
  animation-delay: 1740ms;
}
@keyframes glint {
  0% {
    opacity: 0;
    transform: rotate(0deg) scale(0);
  }
  45% {
    opacity: 1;
    transform: rotate(45deg) scale(1);
  }
  100% {
    opacity: 0;
    transform: rotate(90deg) scale(0.2);
  }
}

.splash-detail {
  position: absolute;
  top: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  margin-top: 18px;
  opacity: 0;
  transition: opacity 300ms ease-out;
}
.splash-detail.shown {
  opacity: 1;
}
.splash-detail p {
  margin: 8px 0 0;
  font-size: 12px;
  white-space: nowrap;
  color: oklch(0.52 0 0);
}
@media (prefers-reduced-motion: reduce) {
  .splash-mark,
  .splash-contact,
  .splash-name,
  .splash-version {
    animation: splash-fade 160ms linear both;
  }
  .lid,
  .blush,
  .glint {
    animation: none;
  }
  @keyframes splash-fade {
    from {
      opacity: 0;
    }
  }
}
</style>
