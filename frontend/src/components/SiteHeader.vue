<script setup lang="ts">
import { t } from "@/i18n";
import { SidebarTrigger } from "@/components/ui/sidebar";
defineProps<{ title: string }>();
</script>
<template>
  <header
    class="window-toolbar flex h-(--header-height) shrink-0 items-center gap-2 border-b px-4"
    data-window-drag
  >
    <SidebarTrigger :aria-label="t('切换导航')" :title="t('切换导航')" />
    <h1 class="min-w-0 flex-1 text-[14px] font-semibold">{{ title }}</h1>
    <div id="workspace-page-actions" class="flex shrink-0 items-center gap-2" />
    <div v-if="$slots.status" class="flex items-center gap-2">
      <slot name="status" />
    </div>
    <div v-if="$slots.default" class="flex items-center gap-2"><slot /></div>
  </header>
</template>
