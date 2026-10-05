<script setup lang="ts">
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { t } from "@/i18n";
import { importVideo } from "@/lib/video-import";
import { WorkspaceService, type ImportedVideo, type LocalVideo } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { isCallError, isMyGo, onFileDrop } from "mygo-runtime";
import { defaultTaskName } from "@/lib/decrypt-presentation";
import { formatDateTime } from "@/lib/datetime";
import { computed, ref, onBeforeUnmount } from "vue";
import {
  FileVideo,
  Search,
  Upload,
  X,
  KeyRound,
  Check,
  ChevronLeft,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldLabel } from "@/components/ui/field";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
} from "@/components/ui/input-group";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { matchingKeys, videoFileError } from "@/lib/decrypt-selection";
import {
  type VideoChoice,
  type KeyChoice,
  type DecryptTask,
} from "@/types/decrypt";
import type { KeyJsonPayload } from "@/types/trace";
const props = defineProps<{
  files: VideoChoice[];
  keys: KeyChoice[];
  keyPayloads: Record<string, KeyJsonPayload>;
  presetKey?: string;
  serviceError?: string;
}>();
const emit = defineEmits<{
  close: [];
  created: [task: DecryptTask];
  busy: [value: boolean];
}>();
interface Selection {
  id: string;
  name: string;
  sourceId?: string;
  local?: LocalVideo;
  keyId: string;
  matched: boolean;
  uploaded?: ImportedVideo;
}
const selections = ref<Selection[]>([]);
const createdTime = formatDateTime(new Date());
const customName = ref<string | null>(null);
const name = computed({
  get: () =>
    customName.value ??
    defaultTaskName(
      selections.value.map((item) => item.name),
      createdTime,
    ),
  set: (value: string) => {
    customName.value = value;
  },
});
const step = ref(1),
  mode = ref(props.files.length ? "existing" : "upload");
const search = ref(""),
  keySearch = ref(""),
  error = ref(""),
  uploadLabel = ref("");
const busy = ref(false),
  dragging = ref(false),
  choosingFiles = ref(false),
  choosingKeyFor = ref("");
const dropZone = ref<{ $el: HTMLElement }>();
let disposed = false;
const filtered = computed(() =>
  props.files.filter((item) =>
    item.name
      .toLocaleLowerCase()
      .includes(search.value.trim().toLocaleLowerCase()),
  ),
);
const keyTarget = computed(() =>
  selections.value.find((item) => item.id === choosingKeyFor.value),
);
const suggestions = computed(() =>
  matchingKeys(keyTarget.value?.name || "", props.keys),
);
const keyOptions = computed(() =>
  props.keys
    .filter((item) =>
      item.name
        .toLocaleLowerCase()
        .includes(keySearch.value.trim().toLocaleLowerCase()),
    )
    .sort(
      (a, b) =>
        Number(suggestions.value.some((k) => k.id === b.id)) -
        Number(suggestions.value.some((k) => k.id === a.id)),
    ),
);
const missing = computed(() =>
  selections.value.filter(
    (item) => !props.keys.some((key) => key.id === item.keyId),
  ),
);
const pageSelected = computed(() => {
  const count = filtered.value.filter((item) =>
    selections.value.some((selected) => selected.sourceId === item.id),
  ).length;
  return count === 0
    ? false
    : count === filtered.value.length
      ? true
      : ("indeterminate" as const);
});
function nextStep() {
  if (busy.value || props.serviceError || !selections.value.length) return;
  error.value = "";
  step.value = 2;
}
function submitForm() {
  if (choosingKeyFor.value) return;
  if (step.value === 1) nextStep();
  else void submit();
}
function match(name: string) {
  const matches = matchingKeys(name, props.keys);
  // A key carried from history applies to the matching video, never every video.
  const preset = matches.find((item) => item.id === props.presetKey);
  return preset
    ? { keyId: preset.id, matched: false }
    : matches.length === 1
      ? { keyId: matches[0]!.id, matched: true }
      : { keyId: "", matched: false };
}
function selectExisting(item: VideoChoice, checked: boolean | "indeterminate") {
  if (busy.value) return;
  if (checked !== true) {
    selections.value = selections.value.filter(
      (selected) => selected.sourceId !== item.id,
    );
    return;
  }
  if (selections.value.some((selected) => selected.sourceId === item.id))
    return;
  if (selections.value.length >= 100) {
    error.value = "每个任务最多选择 100 个视频。";
    return;
  }
  selections.value.push({
    id: `source:${item.id}`,
    sourceId: item.id,
    name: item.name,
    ...match(item.name),
  });
}
function selectAll(checked: boolean | "indeterminate") {
  for (const item of filtered.value) selectExisting(item, checked);
}
function addFiles(files: LocalVideo[]) {
  if (busy.value) return;
  const errors: string[] = [];
  for (const file of files) {
    const id = `local:${file.path}`;
    const message = file.error || videoFileError(file);
    if (message) {
      selections.value = selections.value.filter((item) => item.id !== id);
      errors.push(`${file.name}: ${t(message)}`);
      continue;
    }
    // A native path identifies one selection even if the file changes before
    // it is picked again. Refresh its inspection data so import does not use
    // a stale size/time or a previously imported snapshot.
    const existing = selections.value.find((item) => item.id === id);
    if (existing) {
      if (
        existing.local?.size !== file.size ||
        existing.local?.modifiedTime !== file.modifiedTime
      ) {
        existing.local = file;
        existing.uploaded = undefined;
      }
      continue;
    }
    if (selections.value.length >= 100) {
      errors.push(t("每个任务最多选择 100 个视频。"));
      break;
    }
    selections.value.push({
      id,
      local: file,
      name: file.name,
      ...match(file.name),
    });
  }
  error.value = errors.join("\n");
}
async function chooseFiles() {
  if (busy.value || choosingFiles.value) return;
  if (!isMyGo()) {
    error.value = "请在桌面应用中选择视频文件。";
    return;
  }
  choosingFiles.value = true;
  try {
    const files = await WorkspaceService.chooseVideoFiles();
    if (!disposed) addFiles(files);
  } catch (cause) {
    if (!disposed) error.value = (cause as Error).message;
  } finally {
    choosingFiles.value = false;
  }
}
function dropFiles() {
  dragging.value = false;
  if (!isMyGo()) error.value = "请在桌面应用中选择视频文件。";
}
const offFileDrop = isMyGo()
  ? onFileDrop(({ paths, x, y }) => {
      const target = document.elementFromPoint(x, y);
      if (!target || !dropZone.value?.$el.contains(target) || busy.value)
        return;
      dragging.value = false;
      void WorkspaceService.inspectVideoFiles(paths)
        .then((files) => {
          if (!disposed) addFiles(files);
        })
        .catch((cause) => {
          if (!disposed) error.value = (cause as Error).message;
        });
    })
  : () => {};
function keyFor(item: Selection) {
  return props.keys.find((key) => key.id === item.keyId);
}
function pickKey(key: KeyChoice) {
  if (keyTarget.value) {
    keyTarget.value.keyId = key.id;
    keyTarget.value.matched = false;
  }
  choosingKeyFor.value = "";
}
const uploading = ref(false);
const uncertain = ref(false);
let uploadController: AbortController | undefined;
function onEscape(event: Event) {
  if (busy.value) event.preventDefault();
  else if (choosingKeyFor.value) {
    event.preventDefault();
    choosingKeyFor.value = "";
  }
}
onBeforeUnmount(() => {
  disposed = true;
  uploadController?.abort();
  offFileDrop();
});
async function submit() {
  if (
    busy.value ||
    uncertain.value ||
    !!props.serviceError ||
    !selections.value.length ||
    missing.value.length ||
    !name.value.trim()
  )
    return;
  busy.value = true;
  emit("busy", true);
  error.value = "";
  let creating = false;
  let responseReceived = false;
  uploadController = new AbortController();
  try {
    const videos = [];
    for (const [index, item] of selections.value.entries()) {
      if (item.local && !item.uploaded) {
        uploading.value = true;
        uploadLabel.value = t("导入 {index}/{count}：{name}{count2}", {
          index: index + 1,
          count: selections.value.length,
          name: item.name,
          count2: "",
        });
        item.uploaded = await importVideo(item.local, {
          signal: uploadController.signal,
          onProgress: (percent) => {
            uploadLabel.value = t("导入 {index}/{count}：{name}{count2}", {
              index: index + 1,
              count: selections.value.length,
              name: item.name,
              count2: percent === undefined ? "" : ` · ${percent}%`,
            });
          },
        });
      }
      uploadController.signal.throwIfAborted();
      const keyJson = props.keyPayloads[item.keyId];
      videos.push({
        sourceId: item.sourceId || "",
        uploadId: item.uploaded?.id || "",
        keyJson: keyJson ? JSON.stringify(keyJson) : "",
        keyId: keyJson ? "" : item.keyId.slice(5),
      });
    }
    uploading.value = false;
    uploadLabel.value = "正在创建任务…";
    creating = true;
    const task = (await withDeadline(
      WorkspaceService.createTask({ name: name.value.trim(), videos }),
      30000,
    )) as DecryptTask;
    if (!task || typeof task.id !== "string" || !task.id)
      throw new Error("任务响应异常");
    responseReceived = true;
    if (disposed) return;
    emit("created", task);
  } catch (e) {
    if (disposed) return;
    responseReceived = responseReceived || isCallError(e);
    uncertain.value = creating && !responseReceived;
    error.value = uncertain.value
      ? "任务创建结果暂未确认，请关闭弹窗并查看任务列表，避免重复提交"
      : (e as Error).message;
  } finally {
    uploading.value = false;
    busy.value = false;
    if (!disposed) emit("busy", false);
    uploadLabel.value = "";
  }
}
</script>
<template>
  <Dialog :open="true" @update:open="!$event && !busy && emit('close')">
    <DialogContent
      class="max-h-[90dvh] w-[calc(100vw-2rem)] max-w-3xl grid-cols-[minmax(0,1fr)] grid-rows-[auto_minmax(0,1fr)] gap-0 overflow-hidden p-0 sm:max-w-3xl"
      :show-close-button="!busy"
      @open-auto-focus.prevent
      @escape-key-down="onEscape"
      @interact-outside="$event.preventDefault()"
    >
      <DialogHeader class="border-b px-6 py-5 pr-12"
        ><DialogTitle>{{
          choosingKeyFor ? t("选择密钥") : t("新增解密任务")
        }}</DialogTitle
        ><DialogDescription v-if="choosingKeyFor">{{
          keyTarget?.name
        }}</DialogDescription></DialogHeader
      >
      <form
        class="flex min-h-0 min-w-0 flex-col overflow-hidden"
        @submit.prevent="submitForm"
      >
        <div class="min-h-0 space-y-5 overflow-y-auto px-6 py-5">
          <template v-if="choosingKeyFor">
            <InputGroup
              ><InputGroupInput
                v-model="keySearch"
                :placeholder="t('搜索密钥名称')"
                :aria-label="t('搜索密钥名称')" /><InputGroupAddon
                ><Search /></InputGroupAddon
            ></InputGroup>
            <p
              v-if="suggestions.length > 1 && !keySearch"
              class="text-xs text-muted-foreground"
            >
              {{ t("找到多条同名记录，请选择本次使用的密钥。") }}
            </p>
            <div class="max-h-72 overflow-y-auto rounded-lg border">
              <Button
                v-for="key in keyOptions"
                :key="key.id"
                type="button"
                variant="ghost"
                class="h-auto min-h-12 w-full justify-start gap-3 rounded-none px-3 py-3 text-left whitespace-normal"
                @click="pickKey(key)"
                ><KeyRound class="shrink-0 text-muted-foreground" /><span
                  class="min-w-0 flex-1"
                  ><span class="block break-words">{{ key.name }}</span
                  ><span
                    class="mt-1 block text-xs font-normal text-muted-foreground"
                    >{{ key.source
                    }}{{
                      suggestions.some((item) => item.id === key.id)
                        ? t(" · 同名记录")
                        : ""
                    }}</span
                  ></span
                ><Check v-if="keyTarget?.keyId === key.id"
              /></Button>
              <Empty v-if="!keyOptions.length" class="min-h-32 border-0"
                ><EmptyHeader
                  ><EmptyTitle>{{
                    t("没有匹配的密钥，请换个名称搜索。")
                  }}</EmptyTitle></EmptyHeader
                ></Empty
              >
            </div>
          </template>
          <template v-else-if="step === 1">
            <Tabs v-model="mode">
              <TabsList class="w-full"
                ><TabsTrigger value="existing" class="flex-1">{{
                  t("已有视频")
                }}</TabsTrigger
                ><TabsTrigger value="upload" class="flex-1">{{
                  t("从电脑选择")
                }}</TabsTrigger></TabsList
              >
              <TabsContent value="existing" class="space-y-3">
                <InputGroup
                  ><InputGroupInput
                    v-model="search"
                    :placeholder="t('搜索视频名称')"
                    :aria-label="t('搜索视频名称')" /><InputGroupAddon
                    ><Search /></InputGroupAddon
                ></InputGroup>
                <div v-if="filtered.length" class="rounded-lg border">
                  <Label
                    class="font-normal leading-normal flex cursor-pointer items-center gap-3 border-b bg-muted/30 px-3 py-2.5 text-sm"
                    ><Checkbox
                      :model-value="pageSelected"
                      @update:model-value="selectAll"
                    />{{
                      t("全选搜索结果（{count}）", { count: filtered.length })
                    }}</Label
                  >
                  <div class="max-h-56 overflow-y-auto">
                    <Label
                      v-for="item in filtered"
                      :key="item.id"
                      class="font-normal leading-normal flex cursor-pointer items-start gap-3 px-3 py-3 text-sm hover:bg-muted/50"
                      ><Checkbox
                        class="mt-0.5 shrink-0"
                        :model-value="
                          selections.some(
                            (selected) => selected.sourceId === item.id,
                          )
                        "
                        @update:model-value="selectExisting(item, $event)"
                      /><span class="min-w-0 break-words">{{
                        item.name
                      }}</span></Label
                    >
                  </div>
                </div>
                <Empty v-else class="min-h-32 border-0"
                  ><EmptyHeader
                    ><EmptyTitle>{{
                      t("没有匹配的视频，可从电脑选择 .sz 文件。")
                    }}</EmptyTitle></EmptyHeader
                  ></Empty
                >
              </TabsContent>
              <TabsContent
                ref="dropZone"
                value="upload"
                class="rounded-lg border border-dashed px-4 py-7 text-center"
                :class="
                  dragging ? 'border-primary bg-primary/5' : 'bg-muted/20'
                "
                @dragover.prevent="dragging = true"
                @dragleave.prevent="dragging = false"
                @drop.prevent="dropFiles"
              >
                <Upload class="mx-auto mb-3 size-6 text-muted-foreground" />
                <p class="mb-3 text-sm text-muted-foreground">
                  {{ t("将多个 .sz 视频拖到这里，或") }}
                </p>
                <Button
                  type="button"
                  variant="outline"
                  :disabled="busy || choosingFiles"
                  @click="chooseFiles"
                  >{{ t("选择视频文件") }}</Button
                >
              </TabsContent>
            </Tabs>
            <div v-if="selections.length" class="space-y-2">
              <p class="text-sm font-medium">
                {{ t("已选 {count} 个视频", { count: selections.length }) }}
              </p>
              <div class="max-h-32 overflow-y-auto">
                <div
                  v-for="item in selections"
                  :key="item.id"
                  class="flex items-center gap-2 py-1"
                >
                  <FileVideo
                    class="size-4 shrink-0 text-muted-foreground"
                  /><span class="min-w-0 flex-1 break-words text-xs">{{
                    item.name
                  }}</span
                  ><Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="t('移除 {name}', { name: item.name })"
                    @click="
                      selections = selections.filter(
                        (selected) => selected.id !== item.id,
                      )
                    "
                    ><X
                  /></Button>
                </div>
              </div>
            </div>
          </template>
          <template v-else>
            <Field
              ><FieldLabel for="task-name">{{ t("任务名称") }}</FieldLabel
              ><Input
                id="task-name"
                v-model="name"
                maxlength="100"
                :disabled="busy || uncertain"
            /></Field>
            <div class="flex flex-wrap justify-between gap-2 text-sm">
              <span>{{
                t("{count} 个视频", { count: selections.length })
              }}</span
              ><span
                :class="
                  missing.length ? 'text-destructive' : 'text-muted-foreground'
                "
                >{{
                  missing.length
                    ? t("{count} 个视频待选择密钥", { count: missing.length })
                    : t("密钥已匹配")
                }}</span
              >
            </div>
            <div class="divide-y rounded-lg border">
              <div
                v-for="item in selections"
                :key="item.id"
                class="space-y-2 p-3"
              >
                <p class="break-words text-sm font-medium">{{ item.name }}</p>
                <div class="flex items-start gap-3">
                  <div class="min-w-0 flex-1">
                    <p
                      class="break-words text-xs"
                      :class="
                        keyFor(item)
                          ? 'text-muted-foreground'
                          : 'text-destructive'
                      "
                    >
                      {{ keyFor(item)?.name || t("请选择该视频的密钥") }}
                    </p>
                    <p
                      v-if="keyFor(item)"
                      class="mt-1 text-xs text-muted-foreground"
                    >
                      {{ keyFor(item)?.source
                      }}{{ item.matched ? t(" · 自动匹配") : "" }}
                    </p>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    :disabled="busy || uncertain"
                    @click="
                      choosingKeyFor = item.id;
                      keySearch = '';
                    "
                    >{{ keyFor(item) ? t("更换密钥") : t("选择密钥") }}</Button
                  >
                </div>
              </div>
            </div>
          </template>
          <p
            v-if="error || serviceError"
            role="alert"
            class="whitespace-pre-wrap break-words text-sm text-destructive"
          >
            {{ t(error || serviceError) }}
          </p>
          <p
            v-if="busy"
            role="status"
            class="break-words text-sm text-muted-foreground"
          >
            {{ t(uploadLabel || "正在准备任务…") }}
          </p>
        </div>
        <DialogFooter
          class="m-0 shrink-0 flex-row items-center justify-end rounded-none px-6 py-4"
        >
          <Button
            v-if="choosingKeyFor"
            type="button"
            variant="outline"
            @click="choosingKeyFor = ''"
            >{{ t("返回") }}</Button
          >
          <template v-else
            ><Button
              v-if="uncertain"
              type="button"
              variant="outline"
              @click="emit('close')"
              >{{ t("关闭并查看任务") }}</Button
            >
            <Button
              v-if="uploading"
              type="button"
              variant="outline"
              :disabled="uploadController?.signal.aborted"
              @click="uploadController?.abort()"
              >{{ t("取消导入") }}</Button
            >
            <Button
              v-if="!uncertain"
              type="button"
              variant="outline"
              :disabled="busy"
              @click="step === 1 ? emit('close') : (step = 1)"
              >{{ step === 1 ? t("取消") : t("上一步") }}</Button
            ><Button
              v-if="step === 1"
              type="button"
              :disabled="!selections.length || !!serviceError"
              @click="nextStep"
              >{{ t("下一步 · 确认密钥") }}</Button
            ><Button
              v-else-if="!uncertain"
              type="submit"
              :disabled="
                busy ||
                uncertain ||
                !!missing.length ||
                !selections.length ||
                !name.trim() ||
                !!serviceError
              "
              :aria-busy="busy"
              :aria-label="busy ? t('正在提交') : undefined"
              ><Spinner v-if="busy" />{{
                t("创建任务（{count} 个视频）", { count: selections.length })
              }}</Button
            ></template
          >
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
