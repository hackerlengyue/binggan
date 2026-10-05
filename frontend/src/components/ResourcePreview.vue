<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef, watch } from "vue";
import { useIntersectionObserver } from "@vueuse/core";
const props = defineProps<{ src: string }>();
const image = useTemplateRef<HTMLImageElement>("image");
const visible = ref(false);
const failed = ref(false);
const retrying = ref(false);
const attempt = ref(0);
let retryTimer: ReturnType<typeof setTimeout> | undefined;
const imageSrc = computed(() => {
  if (!visible.value || retrying.value || failed.value) return undefined;
  if (!attempt.value) return props.src;
  return `${props.src}${props.src.includes("?") ? "&" : "?"}retry=${attempt.value}`;
});
function onImageError() {
  if (attempt.value >= 2) {
    failed.value = true;
    return;
  }
  retrying.value = true;
  clearTimeout(retryTimer);
  retryTimer = setTimeout(
    () => {
      attempt.value += 1;
      retrying.value = false;
    },
    attempt.value ? 1200 : 500,
  );
}
watch(
  () => props.src,
  () => {
    clearTimeout(retryTimer);
    failed.value = false;
    retrying.value = false;
    attempt.value = 0;
  },
);
onBeforeUnmount(() => clearTimeout(retryTimer));
const { stop } = useIntersectionObserver(
  image,
  ([entry]) => {
    if (entry?.isIntersecting) {
      visible.value = true;
      stop();
    }
  },
  { rootMargin: "120px" },
);
</script>
<template>
  <img
    ref="image"
    :src="imageSrc"
    @error="onImageError"
    loading="lazy"
    decoding="async"
    fetchpriority="low"
    alt=""
    class="pointer-events-none absolute inset-0 h-full w-full object-cover"
    :class="{ invisible: failed || retrying || !visible }"
    aria-hidden="true"
  />
</template>
