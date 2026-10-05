<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { appReady, reducedMotion } from "@/lib/app-ready";

const props = defineProps<{
  value?: number;
  format: (value: number) => string;
}>();
const shown = ref(props.value ?? 0);
let frame = 0;

function settle() {
  cancelAnimationFrame(frame);
  shown.value = props.value ?? 0;
}
// Ease out so the count lands softly; a short tween keeps frequent updates calm.
function tween(to: number) {
  cancelAnimationFrame(frame);
  const from = shown.value;
  if (from === to || reducedMotion()) {
    shown.value = to;
    return;
  }
  const start = performance.now();
  const duration = Math.min(900, 380 + Math.abs(to - from) * 12);
  const step = (at: number) => {
    const progress = Math.min(1, (at - start) / duration);
    shown.value = Math.round(
      from + (to - from) * (1 - Math.pow(1 - progress, 3)),
    );
    if (progress < 1) frame = requestAnimationFrame(step);
  };
  frame = requestAnimationFrame(step);
}
watch(
  [() => props.value, appReady],
  ([value, ready]) => {
    if (value === undefined) return;
    if (ready) tween(value);
    else shown.value = 0;
  },
  { immediate: true },
);
onBeforeUnmount(() => cancelAnimationFrame(frame));
defineExpose({ settle });
</script>

<template>
  <span class="tabular-nums">{{
    value === undefined ? "-" : format(shown)
  }}</span>
</template>
