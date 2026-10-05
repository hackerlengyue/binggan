<script setup lang="ts">
import { Label } from "@/components/ui/label";
import { t } from "@/i18n";
import { streamURL } from "@/lib/stream-url";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { computed, ref, watch, nextTick, onBeforeUnmount } from "vue";
import { Copy, Download, Search, RotateCcw } from "@lucide/vue";
import { toast } from "vue-sonner";
import { copyText } from "@/lib/clipboard";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Button } from "@/components/ui/button";
import TaskStatusBadge from "@/components/TaskStatusBadge.vue";
import { Checkbox } from "@/components/ui/checkbox";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { type DecryptJob } from "@/types/decrypt";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
} from "@/components/ui/input-group";
import { formatLogTimes, parseLogChunk, parseLogLine } from "@/lib/logs";
import {
  decryptLogEvents,
  conciseDecryptError,
} from "@/lib/decrypt-presentation";
const props = defineProps<{ job: DecryptJob; retrying?: boolean }>();
const emit = defineEmits<{ close: []; retry: [job: DecryptJob] }>();
const text = ref(""),
  error = ref(""),
  loading = ref(true),
  follow = ref(["running", "queued"].includes(props.job.status)),
  query = ref(""),
  mode = ref("summary");
const pane = ref<HTMLElement>();
const events = computed(() =>
  decryptLogEvents(formatLogTimes(text.value)).filter((e) =>
    `${e.time} ${e.message}`
      .toLocaleLowerCase()
      .includes(query.value.trim().toLocaleLowerCase()),
  ),
);
const active = computed(() => ["running", "queued"].includes(props.job.status));
// Lines of the full log, split into time, level and message where the line follows
// the "[time] [LEVEL] message" shape written by the processing engine.
const lines = computed(() => {
  const needle = query.value.trim().toLocaleLowerCase();
  return formatLogTimes(text.value)
    .split("\n")
    .filter((line) => line.trim())
    .map((line, index) => {
      const parsed = parseLogLine(line);
      return { n: index + 1, ...parsed, raw: line };
    })
    .filter((line) => !needle || line.raw.toLocaleLowerCase().includes(needle));
});
const levelClass = (level: string) =>
  ["ERROR", "FATAL"].includes(level)
    ? "text-destructive"
    : ["WARN", "WARNING"].includes(level)
      ? "text-warning"
      : "text-muted-foreground";
const dotClass = (event: { level: string; message: string }) =>
  ["ERROR", "FATAL"].includes(event.level)
    ? "bg-destructive"
    : ["WARN", "WARNING"].includes(event.level)
      ? "bg-warning"
      : /完成|通过|已生成/.test(event.message)
        ? "bg-success"
        : "bg-muted-foreground/40";
async function copyVisible() {
  try {
    await copyText(
      mode.value === "raw"
        ? lines.value.map((line) => line.raw).join("\n")
        : events.value
            .map((event) => `${event.time} ${event.message}`)
            .join("\n"),
    );
    toast.success(t("已复制"));
  } catch {
    toast.error(t("复制失败"));
  }
}
let offset = 0,
  generation = 0,
  disposed = false;
let decoder = new TextDecoder();
let timer: ReturnType<typeof setTimeout> | undefined;
async function scrollLatest() {
  await nextTick();
  if (follow.value && !query.value.trim() && pane.value)
    pane.value.scrollTop = pane.value.scrollHeight;
}
async function readLog(token: number) {
  try {
    let more = true;
    while (more && token === generation && !disposed) {
      const data = await withDeadline(
        WorkspaceService.jobLogs(props.job.id, offset),
        10000,
      );
      if (token !== generation || disposed) return;
      parseLogChunk(data, offset);
      if (data.data)
        text.value += decoder.decode(
          Uint8Array.from(atob(data.data), (c) => c.charCodeAt(0)),
          { stream: true },
        );
      offset = data.offset;
      more = data.hasMore;
      error.value = "";
      await scrollLatest();
    }
  } catch (e) {
    if (token === generation && !disposed) error.value = (e as Error).message;
  } finally {
    if (token === generation && !disposed) {
      loading.value = false;
      if (error.value || ["running", "queued"].includes(props.job.status))
        timer = setTimeout(() => readLog(token), 2000);
    }
  }
}
watch(
  () => [props.job.id, props.job.status, props.job.attempt] as const,
  (value, previous) => {
    generation++;
    clearTimeout(timer);
    if (!previous || value[0] !== previous[0]) {
      text.value = "";
      query.value = "";
      error.value = "";
      offset = 0;
      decoder = new TextDecoder();
      loading.value = true;
    }
    void readLog(generation);
  },
  { immediate: true },
);
watch([mode, follow], scrollLatest);
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(timer);
});
</script>
<template>
  <Dialog :open="true" @update:open="!$event && emit('close')">
    <DialogContent
      class="flex h-[min(44rem,88dvh)] w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl"
      @open-auto-focus.prevent
    >
      <DialogHeader class="gap-1 border-b px-6 pt-5 pb-4 pr-14">
        <div class="flex items-center gap-2.5">
          <DialogTitle>{{ t("处理日志") }}</DialogTitle>
          <TaskStatusBadge :status="job.status" />
        </div>
        <DialogDescription class="break-words">{{
          job.name
        }}</DialogDescription>
      </DialogHeader>
      <div class="flex flex-wrap items-center gap-2 border-b px-6 py-3">
        <Tabs v-model="mode"
          ><TabsList
            ><TabsTrigger value="summary">{{ t("处理过程") }}</TabsTrigger
            ><TabsTrigger value="raw">{{
              t("完整日志")
            }}</TabsTrigger></TabsList
          ></Tabs
        >
        <InputGroup class="h-8 min-w-40 flex-1"
          ><InputGroupAddon><Search /></InputGroupAddon
          ><InputGroupInput
            v-model="query"
            :placeholder="t('搜索日志')"
            :aria-label="t('搜索日志')"
        /></InputGroup>
        <Button
          variant="outline"
          size="icon"
          :aria-label="t('复制')"
          :title="t('复制')"
          :disabled="!text"
          @click="copyVisible"
          ><Copy
        /></Button>
        <Button as-child variant="outline"
          ><a :href="streamURL(`/api/decrypt/jobs/${job.id}/logs?download=1`)"
            ><Download />{{ t("导出日志") }}</a
          ></Button
        >
        <Button
          v-if="job.status === 'failed'"
          variant="outline"
          :disabled="retrying"
          @click="emit('retry', job)"
          ><RotateCcw />{{ t("重试解密") }}</Button
        >
      </div>
      <p v-if="error" role="alert" class="px-6 pt-3 text-sm text-destructive">
        {{ t(error) }}
      </p>
      <div
        ref="pane"
        class="min-h-0 flex-1 overflow-auto"
        tabindex="0"
        :aria-label="t('解密日志')"
      >
        <div
          v-if="loading && !text"
          class="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground"
        >
          <Spinner />{{ t("正在读取日志") }}
        </div>
        <template v-else-if="mode === 'summary'">
          <ol v-if="events.length" class="px-6 py-5">
            <li
              v-for="(event, i) in events"
              :key="i"
              class="relative grid grid-cols-[4.25rem_0.75rem_minmax(0,1fr)] items-start gap-x-3 pb-5 text-sm last:pb-0"
            >
              <time
                class="pt-0.5 font-mono text-xs tabular-nums text-muted-foreground"
                >{{ event.time }}</time
              >
              <span
                class="relative mt-2 size-2 justify-self-center rounded-full ring-4 ring-background"
                :class="dotClass(event)"
              />
              <span
                v-if="i < events.length - 1"
                class="absolute top-4 bottom-0 left-[calc(4.25rem+0.75rem+0.375rem-0.5px)] w-px bg-border"
                aria-hidden="true"
              />
              <p
                class="min-w-0 break-words leading-6"
                :class="
                  ['ERROR', 'FATAL'].includes(event.level)
                    ? 'font-medium text-destructive'
                    : ''
                "
              >
                {{ event.message }}
              </p>
            </li>
          </ol>
          <div v-else class="p-6">
            <Alert v-if="job.error && !query.trim()" variant="destructive"
              ><AlertTitle>{{ t("处理失败") }}</AlertTitle
              ><AlertDescription>{{
                conciseDecryptError(job.error)
              }}</AlertDescription></Alert
            >
            <Empty v-else class="min-h-40">
              <EmptyHeader
                ><EmptyTitle>{{
                  query.trim() ? t("无匹配内容") : t("等待处理记录")
                }}</EmptyTitle></EmptyHeader
              >
            </Empty>
          </div>
        </template>
        <template v-else>
          <ol v-if="lines.length" class="py-2 font-mono text-xs leading-6">
            <li
              v-for="line in lines"
              :key="line.n"
              class="grid grid-cols-[2.75rem_4.25rem_3.25rem_minmax(0,1fr)] gap-x-3 px-4 hover:bg-muted/50"
            >
              <span
                class="select-none text-right tabular-nums text-muted-foreground/60"
                >{{ line.n }}</span
              >
              <span class="tabular-nums text-muted-foreground">{{
                line.time
              }}</span>
              <span class="font-medium" :class="levelClass(line.level)">{{
                line.level
              }}</span>
              <span
                class="min-w-0 whitespace-pre-wrap break-words [overflow-wrap:anywhere]"
                :class="
                  ['ERROR', 'FATAL'].includes(line.level)
                    ? 'text-destructive'
                    : ''
                "
                >{{ line.message }}</span
              >
            </li>
          </ol>
          <Empty v-else class="min-h-40 p-6">
            <EmptyHeader
              ><EmptyTitle>{{
                query.trim() ? t("无匹配内容") : t("暂无日志")
              }}</EmptyTitle></EmptyHeader
            >
          </Empty>
        </template>
      </div>
      <div
        class="flex items-center justify-between gap-3 border-t px-6 py-2.5 text-xs text-muted-foreground"
      >
        <span class="tabular-nums">{{
          t("共 {count} 条", {
            count: mode === "raw" ? lines.length : events.length,
          })
        }}</span>
        <div v-if="active" class="flex items-center gap-4">
          <span class="flex items-center gap-2"
            ><Spinner class="size-3" />{{ t("实时更新") }}</span
          >
          <Label class="font-normal leading-normal flex items-center gap-2"
            ><Checkbox
              :model-value="follow"
              @update:model-value="follow = $event === true"
            />{{ t("跟随最新") }}</Label
          >
        </div>
      </div>
    </DialogContent>
  </Dialog>
</template>
