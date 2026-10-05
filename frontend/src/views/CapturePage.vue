<script setup lang="ts">
import { t } from "@/i18n";
import { computed } from "vue";
import { Download, Pencil } from "@lucide/vue";
import { toast } from "vue-sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import RecordWorkspace from "@/components/RecordWorkspace.vue";
import { useTraceStore } from "@/stores/trace";
const emit = defineEmits<{ rename: [id: string] }>();
const store = useTraceStore();
const entry = computed(() =>
  store.keyHistory.find((item) => item.id === store.currentHistoryId),
);
function download() {
  if (!entry.value) return;
  try {
    store.downloadHistory(entry.value.id);
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}
</script>

<template>
  <section
    class="flex min-h-80 flex-1 flex-col gap-4 md:gap-6"
    :aria-label="t('本次秘钥提取')"
  >
    <div
      v-if="entry && !store.captureEnabled"
      class="flex shrink-0 items-center justify-end gap-2"
    >
      <Button variant="outline" @click="emit('rename', entry.id)"
        ><Pencil />{{ t("改名") }}</Button
      >
      <Button
        variant="outline"
        :disabled="!entry.valid"
        :title="entry.valid ? undefined : entry.issues.join('；')"
        @click="download"
        ><Download />{{ t("下载 JSON") }}</Button
      >
    </div>
    <RecordWorkspace
      :key="store.currentHistoryId"
      :payload="
        store.historyError
          ? store.keyResult.keyJson
          : (entry?.keyJson ?? store.keyResult.keyJson)
      "
      :traces="
        store.historyError ? store.traces : (entry?.traces ?? store.traces)
      "
      :capturing="store.captureEnabled"
      :current="!entry || store.captureEnabled"
      :loading="store.loading"
      ><template #status
        ><Badge variant="secondary" role="status" aria-live="polite">{{
          t(
            store.captureEnabled
              ? "正在提取"
              : store.currentHistoryId
                ? "已停止"
                : "未开始",
          )
        }}</Badge></template
      ></RecordWorkspace
    >
  </section>
</template>
