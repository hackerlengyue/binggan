<script setup lang="ts">
import { onBeforeUnmount, ref } from "vue";
import { Upload } from "@lucide/vue";
import { toast } from "vue-sonner";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { t } from "@/i18n";
import { keyFileError, prepareImportedKeys } from "@/lib/key-import";
import { useTraceStore } from "@/stores/trace";
import type { KeyHistoryEntry } from "@/types/trace";

const props = withDefaults(
  defineProps<{ disabled?: boolean; multiple?: boolean }>(),
  {
    multiple: true,
  },
);
const emit = defineEmits<{
  imported: [entries: KeyHistoryEntry[]];
  busy: [value: boolean];
}>();
const store = useTraceStore();
const input = ref<HTMLInputElement>();
const uploading = ref(false);
const error = ref("");
let disposed = false;
onBeforeUnmount(() => {
  disposed = true;
});

async function upload(event: Event) {
  const target = event.target as HTMLInputElement;
  const files = Array.from(target.files ?? []);
  target.value = "";
  if (!files.length || uploading.value || props.disabled) return;
  uploading.value = true;
  emit("busy", true);
  error.value = "";
  try {
    if (files.length > 100) throw new Error(t("每次请选择 1–100 个密钥文件。"));
    for (const file of files) {
      const message = keyFileError(file);
      if (message) throw new Error(`${file.name}: ${t(message)}`);
    }
    const contents = await Promise.all(
      files.map(async (file) => ({
        name: file.name,
        raw: await file.text(),
      })),
    );
    if (disposed) return;
    const prepared = await prepareImportedKeys(contents);
    if (disposed) return;
    const entries = await store.importKeyFiles(prepared.files);
    if (disposed) return;
    toast.success(t("已导入 {count} 份密钥", { count: entries.length }));
    if (prepared.adjusted)
      toast.info(
        t("已自动匹配 {count} 份密钥的解密参数", { count: prepared.adjusted }),
      );
    emit("imported", entries);
  } catch (cause) {
    if (!disposed) error.value = t((cause as Error).message);
  } finally {
    uploading.value = false;
    if (!disposed) emit("busy", false);
  }
}
</script>

<template>
  <div class="flex min-w-0 flex-col items-start gap-2">
    <input
      ref="input"
      class="hidden"
      type="file"
      accept=".json,application/json"
      :multiple="multiple"
      :disabled="disabled || uploading || !!store.busy || !store.historyReady"
      :aria-label="t('上传密钥')"
      @change="upload"
    />
    <Button
      type="button"
      variant="outline"
      :disabled="disabled || uploading || !!store.busy || !store.historyReady"
      :aria-busy="uploading"
      @click="input?.click()"
      ><Spinner v-if="uploading" /><Upload v-else />{{
        t(uploading ? "正在导入密钥…" : "上传密钥")
      }}</Button
    >
    <p
      v-if="error"
      role="alert"
      class="max-w-md break-words text-sm text-destructive"
    >
      {{ error }}
    </p>
  </div>
</template>
