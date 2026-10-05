<script setup lang="ts">
import type { HTMLAttributes } from "vue";
import { Table } from "@/components/ui/table";

defineOptions({ inheritAttrs: false });
defineProps<{ class?: HTMLAttributes["class"] }>();
</script>

<template>
  <div
    data-slot="data-table"
    class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-card"
  >
    <Table v-bind="$attrs" :class="$props.class"><slot /></Table>
  </div>
</template>

<style scoped>
/* Keep the official Table markup and one scroll owner for both axes. Sticky
   headers must belong to this viewport, not an outer page or dialog scroller. */
[data-slot="data-table"] > :deep([data-slot="table-container"]) {
  flex: 1 1 0%;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
  scroll-padding-top: 2.5rem;
}
:deep([data-slot="table-header"]) {
  position: sticky;
  top: 0;
  z-index: 10;
  background: var(--muted);
  box-shadow: 0 1px 0 var(--border);
}
:deep([data-slot="table-header"] [data-slot="table-row"]:hover) {
  background: transparent;
}
</style>
