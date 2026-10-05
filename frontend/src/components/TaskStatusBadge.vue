<script setup lang="ts">
import { t } from "@/i18n";
import { Badge } from "@/components/ui/badge";
import { statusLabels, type DecryptTask } from "@/types/decrypt";
defineProps<{ status: DecryptTask["status"] }>();
const variants = {
  queued: "secondary",
  running: "info",
  completed: "success",
  failed: "destructive",
  partial_failed: "warning",
} as const;
</script>
<template>
  <Badge :variant="variants[status]" class="gap-1.5">
    <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />{{
      t(statusLabels[status])
    }}
  </Badge>
</template>
