<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { t } from "@/i18n";
import { conciseDecryptError } from "@/lib/decrypt-presentation";
import { streamURL } from "@/lib/stream-url";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { formatDateTime } from "@/lib/datetime";
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";
import {
  Download,
  Plus,
  ArrowLeft,
  FileText,
  Trash2,
  Eye,
  RotateCcw,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import TaskStatusBadge from "@/components/TaskStatusBadge.vue";
import { Progress } from "@/components/ui/progress";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import DecryptTaskDialog from "@/components/DecryptTaskDialog.vue";
import DecryptLogsDialog from "@/components/DecryptLogsDialog.vue";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import ListPagination from "@/components/ListPagination.vue";
import { usePagination } from "@/composables/usePagination";
import { useTraceStore } from "@/stores/trace";
import {
  activeTask,
  taskMatches,
  taskProgress,
  stageLabels,
  statusLabels,
  type DecryptTask,
  type VideoChoice,
  type DecryptJob,
} from "@/types/decrypt";
import { toast } from "vue-sonner";
const store = useTraceStore();
const route = useRoute(),
  router = useRouter();
const tasks = ref<DecryptTask[]>([]),
  files = ref<VideoChoice[]>([]),
  diskKeys = ref<VideoChoice[]>([]);
const loading = ref(true),
  syncError = ref(""),
  createOpen = ref(false),
  submitting = ref(false),
  presetKey = ref("");
const taskFilter = ref("all");
const categories = [
  { value: "all", label: "全部" },
  { value: "running", label: "进行中" },
  { value: "completed", label: "已完成" },
  { value: "failed", label: "失败" },
];
const visibleTasks = computed(() =>
  tasks.value.filter((task) => taskMatches(task, taskFilter.value)),
);
const { page, pageSize, rows: taskRows } = usePagination(visibleTasks);
watch(taskFilter, () => {
  page.value = 1;
});
const selected = ref(new Set<string>());
const detailId = computed(() =>
  typeof route.query.task === "string" ? route.query.task : "",
);
const detailTask = computed(() =>
  tasks.value.find((task) => task.id === detailId.value),
);
const {
  page: childPage,
  pageSize: childPageSize,
  rows: childRows,
} = usePagination(() => detailTask.value?.children ?? []);
watch(detailId, () => {
  childPage.value = 1;
});
function openDetail(id: string) {
  void router.push({ query: { ...route.query, task: id } });
}
function closeDetail() {
  const query = { ...route.query };
  delete query.task;
  logId.value = "";
  void router.push({ query });
}
const deletable = computed(() =>
  taskRows.value.filter((task) => !activeTask(task)),
);
const pageSelection = computed(() => {
  const count = deletable.value.filter((task) =>
    selected.value.has(task.id),
  ).length;
  return !count
    ? false
    : count === deletable.value.length
      ? true
      : "indeterminate";
});
const deleteTargets = ref<DecryptTask[]>([]),
  deleteOpen = ref(false),
  deleting = ref(false),
  deleteError = ref("");
const retrying = ref(false);
async function retryJobs(task: DecryptTask, jobs: DecryptJob[]) {
  if (retrying.value || deleting.value || submitting.value) return;
  if (jobs.some((job) => !job.retryable)) {
    logId.value = "";
    openCreate();
    toast.info(t("此旧任务未保留源文件，请重新选择视频"));
    return;
  }
  retrying.value = true;
  try {
    const updated = (await withDeadline(
      WorkspaceService.retryTask(
        task.id,
        jobs.map((job) => job.id),
      ),
      20000,
    )) as DecryptTask;
    if (disposed) return;
    ++refreshVersion;
    tasks.value = tasks.value.map((item) =>
      item.id === task.id ? updated : item,
    );
    toast.success(t("已重新排队"));
  } catch (error) {
    if (!disposed) toast.error((error as Error).message);
    if (!disposed) await refresh();
  } finally {
    retrying.value = false;
  }
}
function retryJob(job: DecryptJob) {
  const task = tasks.value.find((task) =>
    task.children.some((item) => item.id === job.id),
  );
  if (task) void retryJobs(task, [job]);
}
const logId = ref("");
const logJob = computed(() =>
  tasks.value
    .flatMap((task) => task.children)
    .find((job) => job.id === logId.value),
);
const history = computed(() =>
  store.keyHistory.filter(
    (item) =>
      item.valid &&
      !(store.captureEnabled && item.id === store.currentHistoryId),
  ),
);
const keys = computed(() => [
  ...history.value.map((item) => ({
    id: `history:${item.id}`,
    name: item.name,
    source: t("提取记录"),
  })),
  ...diskKeys.value.map((item) => ({
    id: `disk:${item.id}`,
    name: item.name,
    source: t("本地密钥"),
  })),
]);
const keyPayloads = computed(() =>
  Object.fromEntries(
    history.value.map((item) => [`history:${item.id}`, item.keyJson]),
  ),
);
let timer: ReturnType<typeof setTimeout> | undefined;
let disposed = false;
let refreshVersion = 0;
async function refresh() {
  const version = ++refreshVersion;
  try {
    const data = await withDeadline(WorkspaceService.tasks(), 10000);
    if (disposed || version !== refreshVersion) return;
    if (!Array.isArray(data.tasks))
      throw new Error(t("请重启后端以加载任务接口。"));
    files.value = data.files;
    diskKeys.value = data.keys;
    tasks.value = data.tasks as DecryptTask[];
    syncError.value = "";
    const available = new Set(
      tasks.value.filter((task) => !activeTask(task)).map((task) => task.id),
    );
    selected.value = new Set(
      [...selected.value].filter((id) => available.has(id)),
    );
  } catch (e) {
    if (!disposed && version === refreshVersion)
      syncError.value = (e as Error).message;
  } finally {
    if (version === refreshVersion) loading.value = false;
  }
}
async function poll() {
  await refresh();
  if (!disposed) timer = setTimeout(poll, 5000);
}
function openCreate(key = "") {
  presetKey.value = key;
  createOpen.value = true;
}
function created(task: DecryptTask) {
  ++refreshVersion;
  tasks.value = [task, ...tasks.value.filter((item) => item.id !== task.id)];
  taskFilter.value = "all";
  page.value = 1;
  createOpen.value = false;
  toast.success(t("已创建任务，包含 {count} 个视频", { count: task.total }));
  openDetail(task.id);
  void refresh();
}
watch(
  () => route.query.key,
  (key) => {
    if (typeof key !== "string" || !key) return;
    openCreate(`history:${key}`);
    const query = { ...route.query };
    delete query.key;
    void router.replace({ query });
  },
  { immediate: true },
);
watch(taskFilter, () => selected.value.clear());
watch([page, pageSize], () => selected.value.clear());
onBeforeRouteLeave(() => {
  if (!submitting.value && !retrying.value && !deleting.value) return true;
  toast.info(t("正在处理操作，请稍候"));
  return false;
});
function select(id: string, checked: boolean | "indeterminate") {
  if (checked === true) selected.value.add(id);
  else selected.value.delete(id);
}
function selectPage(checked: boolean | "indeterminate") {
  for (const task of deletable.value) select(task.id, checked);
}
function askDelete(targets: DecryptTask[]) {
  deleteTargets.value = targets;
  deleteError.value = "";
  deleteOpen.value = true;
}
async function confirmDelete() {
  if (
    deleting.value ||
    retrying.value ||
    submitting.value ||
    !deleteTargets.value.length
  )
    return;
  deleting.value = true;
  deleteError.value = "";
  try {
    const data = await withDeadline(
      WorkspaceService.deleteTasks(deleteTargets.value.map((task) => task.id)),
      20000,
    );
    if (disposed) return;
    ++refreshVersion;
    tasks.value = tasks.value.filter((task) => !data.deleted.includes(task.id));
    for (const id of data.deleted) {
      selected.value.delete(id);
      if (detailId.value === id) closeDetail();
    }
    deleteOpen.value = false;
    toast.success(
      data.cleanupPending
        ? t("任务已删除，剩余文件将在服务重启时清理。")
        : t("已删除 {count} 个任务", { count: data.deleted.length }),
    );
  } catch (e) {
    if (!disposed) deleteError.value = (e as Error).message;
    if (!disposed) await refresh();
  } finally {
    deleting.value = false;
  }
}
function jobPercent(job: DecryptJob) {
  return job.status === "completed"
    ? 100
    : job.progress?.total
      ? Math.min(
          100,
          Math.round((job.progress.done / job.progress.total) * 100),
        )
      : 0;
}
function date(value: string) {
  return formatDateTime(value);
}
onMounted(poll);
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(timer);
});
</script>
<template>
  <section
    class="flex min-h-80 min-w-0 flex-1 flex-col"
    :aria-label="t('视频解密')"
  >
    <Teleport v-if="!detailId" to="#workspace-page-actions">
      <Button @click="openCreate()"><Plus />{{ t("新增解密") }}</Button>
    </Teleport>
    <div v-if="detailId" class="flex min-h-0 flex-1 flex-col gap-5">
      <Button variant="ghost" class="-ml-2 self-start" @click="closeDetail"
        ><ArrowLeft />{{ t("返回任务列表") }}</Button
      >
      <template v-if="detailTask">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div class="min-w-0 space-y-2">
            <h2 class="break-words text-lg font-semibold">
              {{ detailTask.name }}
            </h2>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <TaskStatusBadge :status="detailTask.status" />
            <Button
              v-if="detailTask.failed"
              variant="outline"
              size="sm"
              :disabled="retrying"
              @click="
                retryJobs(
                  detailTask,
                  detailTask.children.filter((job) => job.status === 'failed'),
                )
              "
              ><RotateCcw />{{ t("重试失败项") }}</Button
            >
            <Button v-if="detailTask.completed" as-child size="sm"
              ><a
                :href="
                  streamURL(`/api/decrypt/tasks/${detailTask.id}/download`)
                "
                ><Download />{{
                  t("下载已完成视频（{count}）", {
                    count: detailTask.completed,
                  })
                }}</a
              ></Button
            >
          </div>
        </div>
        <div class="space-y-2">
          <div class="flex flex-wrap justify-between gap-2 text-sm">
            <span>{{
              t("总进度 {progress}%", { progress: taskProgress(detailTask) })
            }}</span>
            <span class="text-muted-foreground">{{
              t("成功 {value} · 失败 {value2}", {
                value: detailTask.completed,
                value2: detailTask.failed,
              })
            }}</span>
          </div>
          <Progress
            :model-value="taskProgress(detailTask)"
            :aria-label="t('任务总进度（按视频阶段估算）')"
            class="h-1.5"
          />
        </div>
        <p v-if="syncError" role="alert" class="text-sm text-destructive">
          {{ t(syncError) }}
        </p>
        <div class="data-list-panel">
          <div class="data-list-scroll">
            <DataTable class="min-w-[680px] table-fixed"
              ><TableHeader
                ><TableRow
                  ><TableHead>{{ t("视频") }}</TableHead
                  ><TableHead class="w-[32%]">{{ t("进度") }}</TableHead
                  ><TableHead class="w-24 text-center">{{
                    t("状态")
                  }}</TableHead
                  ><TableHead class="w-36 text-center">{{
                    t("操作")
                  }}</TableHead></TableRow
                ></TableHeader
              ><TableBody
                ><TableRow v-for="job in childRows" :key="job.id">
                  <TableCell
                    ><p class="truncate text-sm" :title="job.name">
                      {{ job.name }}
                    </p>
                    <p
                      v-if="job.error"
                      class="mt-1 max-w-lg break-words whitespace-normal text-xs text-destructive"
                    >
                      {{ conciseDecryptError(job.error) }}
                    </p></TableCell
                  >
                  <TableCell
                    ><div class="flex items-center gap-3">
                      <p
                        v-if="job.status === 'running'"
                        class="shrink-0 text-xs tabular-nums"
                      >
                        {{
                          `${t(stageLabels[job.progress?.stage || "decrypt"] || "处理中")} · ${jobPercent(job)}%`
                        }}
                      </p>
                      <Progress
                        v-if="job.status !== 'failed'"
                        :model-value="jobPercent(job)"
                        class="h-1.5 min-w-0 flex-1"
                        :aria-label="
                          t('{name} 当前阶段进度', { name: job.name })
                        "
                      /></div
                  ></TableCell>
                  <TableCell class="text-center"
                    ><TaskStatusBadge :status="job.status"
                  /></TableCell>
                  <TableCell
                    ><div class="flex justify-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        class="size-8"
                        :aria-label="t('查看日志 {name}', { name: job.name })"
                        :title="t('查看日志')"
                        @click="logId = job.id"
                        ><FileText /></Button
                      ><Button
                        v-if="job.status === 'failed'"
                        variant="ghost"
                        size="icon"
                        class="size-8"
                        :disabled="retrying"
                        :title="t('重试解密')"
                        :aria-label="t('重试解密 {name}', { name: job.name })"
                        @click="retryJob(job)"
                        ><RotateCcw /></Button
                      ><Button
                        v-if="job.status === 'completed'"
                        as-child
                        variant="ghost"
                        size="icon"
                        class="size-8"
                        ><a
                          :href="
                            streamURL(`/api/decrypt/jobs/${job.id}/download`)
                          "
                          :title="t('下载 MP4')"
                          :aria-label="t('下载 MP4 {name}', { name: job.name })"
                          ><Download /></a
                      ></Button></div
                  ></TableCell> </TableRow></TableBody
            ></DataTable>
          </div>
          <ListPagination
            v-model:page="childPage"
            v-model:page-size="childPageSize"
            :total="detailTask.children.length"
            :label="t('子任务分页')"
          />
        </div>
      </template>
      <div
        v-else-if="loading"
        role="status"
        :aria-label="t('正在读取任务')"
        class="space-y-5"
      >
        <Skeleton class="h-7 w-1/3" />
        <Skeleton class="h-4 w-1/2" />
        <Skeleton class="h-48 w-full" />
      </div>
      <Empty v-else
        ><EmptyHeader
          ><EmptyTitle>{{
            syncError && !tasks.length ? t("任务暂不可用") : t("未找到该任务")
          }}</EmptyTitle
          ><EmptyDescription>{{
            t(syncError || "任务可能已被删除，请返回任务列表。")
          }}</EmptyDescription></EmptyHeader
        ></Empty
      >
    </div>
    <Tabs
      v-else
      v-model="taskFilter"
      class="flex min-h-0 min-w-0 flex-1 flex-col gap-4"
      :aria-label="t('任务分类')"
    >
      <div class="flex flex-wrap items-center justify-between gap-3">
        <TabsList
          ><TabsTrigger
            v-for="category in categories"
            :key="category.value"
            :value="category.value"
            >{{ category.label
            }}<span class="ml-1 text-xs tabular-nums text-muted-foreground">{{
              loading && !tasks.length
                ? "…"
                : tasks.filter((task) => taskMatches(task, category.value))
                    .length
            }}</span></TabsTrigger
          ></TabsList
        >
        <div class="flex items-center gap-2">
          <Button
            v-if="selected.size"
            variant="destructive"
            @click="askDelete(tasks.filter((task) => selected.has(task.id)))"
            ><Trash2 />{{
              t("删除所选（{count}）", { count: selected.size })
            }}</Button
          >
        </div>
      </div>
      <TabsContent
        :value="taskFilter"
        class="flex min-h-0 min-w-0 flex-1 flex-col gap-4"
      >
        <p v-if="syncError" role="alert" class="text-sm text-destructive">
          {{ t(syncError) }}
        </p>
        <div class="data-list-panel">
          <div class="data-list-scroll" :aria-busy="loading && !tasks.length">
            <DataTable class="min-w-[700px] table-fixed">
              <TableHeader
                ><TableRow
                  ><TableHead class="w-10"
                    ><Checkbox
                      :model-value="pageSelection"
                      :disabled="!deletable.length"
                      :aria-label="t('全选当前页可删除的任务')"
                      @update:model-value="selectPage" /></TableHead
                  ><TableHead>{{ t("任务名称") }}</TableHead
                  ><TableHead class="w-[26%]">{{ t("进度") }}</TableHead
                  ><TableHead class="w-12 text-center">{{
                    t("成功")
                  }}</TableHead
                  ><TableHead class="w-12 text-center">{{
                    t("失败")
                  }}</TableHead
                  ><TableHead class="w-24 text-center">{{
                    t("状态")
                  }}</TableHead
                  ><TableHead class="hidden w-44 @4xl/main:table-cell">{{
                    t("创建时间")
                  }}</TableHead
                  ><TableHead class="w-32 text-center">{{
                    t("操作")
                  }}</TableHead></TableRow
                ></TableHeader
              >
              <TableBody
                ><TableSkeletonRows
                  v-if="loading && !tasks.length"
                  :columns="8"
                  selection />
                <template v-for="task in taskRows" :key="task.id">
                  <TableRow
                    :data-state="selected.has(task.id) ? 'selected' : undefined"
                  >
                    <TableCell
                      ><Checkbox
                        :model-value="selected.has(task.id)"
                        :disabled="activeTask(task)"
                        :aria-label="t('选择任务 {name}', { name: task.name })"
                        :title="
                          activeTask(task)
                            ? t('运行中或排队中的任务不能删除')
                            : undefined
                        "
                        @update:model-value="select(task.id, $event)"
                    /></TableCell>
                    <TableCell>
                      <div class="min-w-0">
                        <Button
                          variant="ghost"
                          class="h-auto min-h-7 max-w-full justify-start px-0 whitespace-normal text-left font-medium"
                          :title="task.name"
                          @click="openDetail(task.id)"
                          ><span class="break-words [overflow-wrap:anywhere]">{{
                            task.name
                          }}</span></Button
                        >
                      </div>
                    </TableCell>
                    <TableCell>
                      <div
                        class="flex items-center gap-3 whitespace-nowrap text-sm tabular-nums"
                      >
                        <Progress
                          :model-value="taskProgress(task)"
                          :aria-label="
                            t('{name} 总进度（按视频阶段估算）', {
                              name: task.name,
                            })
                          "
                          :title="t('按视频处理阶段估算')"
                          class="h-1.5 min-w-0 flex-1"
                        />
                        <span class="w-11 shrink-0 text-right"
                          >{{ taskProgress(task) }}%</span
                        >
                      </div>
                    </TableCell>
                    <TableCell class="text-center tabular-nums">{{
                      task.completed
                    }}</TableCell>
                    <TableCell
                      class="text-center tabular-nums"
                      :class="
                        task.failed
                          ? 'text-destructive'
                          : 'text-muted-foreground'
                      "
                      >{{ task.failed }}</TableCell
                    >
                    <TableCell class="text-center"
                      ><TaskStatusBadge :status="task.status"
                    /></TableCell>
                    <TableCell
                      class="hidden whitespace-nowrap text-muted-foreground tabular-nums @4xl/main:table-cell"
                      >{{ date(task.createdAt) }}</TableCell
                    >
                    <TableCell class="w-32">
                      <div class="flex items-center justify-center gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          class="size-8"
                          :title="t('查看详情')"
                          :aria-label="
                            t('查看详情 {name}', { name: task.name })
                          "
                          @click="openDetail(task.id)"
                          ><Eye
                        /></Button>
                        <Button
                          v-if="task.failed"
                          variant="ghost"
                          size="icon"
                          class="size-8"
                          :disabled="retrying"
                          :title="t('重试失败项')"
                          :aria-label="
                            t('重试失败项 {name}', { name: task.name })
                          "
                          @click="
                            retryJobs(
                              task,
                              task.children.filter(
                                (job) => job.status === 'failed',
                              ),
                            )
                          "
                          ><RotateCcw
                        /></Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          class="size-8"
                          :title="
                            activeTask(task)
                              ? t('任务进行中，暂不可删除')
                              : t('删除任务')
                          "
                          :disabled="activeTask(task)"
                          :aria-label="
                            t('删除任务 {name}', { name: task.name })
                          "
                          @click="askDelete([task])"
                          ><Trash2
                        /></Button>
                      </div>
                    </TableCell>
                  </TableRow> </template
              ></TableBody> </DataTable
            ><Empty
              v-if="!loading && !visibleTasks.length"
              class="min-h-40 flex-1"
              ><EmptyHeader
                ><EmptyTitle>{{
                  syncError && !tasks.length
                    ? t("任务暂不可用")
                    : taskFilter === "all"
                      ? t("还没有解密任务")
                      : t("这个分类下暂无任务")
                }}</EmptyTitle></EmptyHeader
              ></Empty
            >
          </div>
          <ListPagination
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="visibleTasks.length"
            :loading="loading"
            :total-pending="loading && !tasks.length"
            :disabled="loading && !tasks.length"
            :label="t('任务分页')"
          />
        </div>
      </TabsContent>
    </Tabs>
  </section>
  <DecryptTaskDialog
    v-if="createOpen"
    :files="files"
    :keys="keys"
    :key-payloads="keyPayloads"
    :preset-key="presetKey"
    :service-error="syncError"
    @close="createOpen = false"
    @busy="submitting = $event"
    @created="created"
  />
  <DecryptLogsDialog
    v-if="logJob"
    :job="logJob"
    :retrying="retrying"
    @retry="retryJob"
    @close="logId = ''"
  />
  <AlertDialog
    :open="deleteOpen"
    @update:open="!deleting && (deleteOpen = $event)"
    ><AlertDialogContent
      ><AlertDialogHeader
        ><AlertDialogTitle>{{
          t("删除 {count} 个任务？", { count: deleteTargets.length })
        }}</AlertDialogTitle
        ><AlertDialogDescription>{{
          t(
            "将删除所选任务下的 {count} 个视频的任务记录、日志及重试副本。已生成的资源和原文件保留，此操作无法撤销。",
            { count: deleteTargets.reduce((sum, task) => sum + task.total, 0) },
          )
        }}</AlertDialogDescription></AlertDialogHeader
      >
      <div class="max-h-32 overflow-auto text-sm">
        <p v-for="task in deleteTargets" :key="task.id" class="break-words">
          {{ task.name }}
        </p>
      </div>
      <p v-if="deleteError" role="alert" class="text-sm text-destructive">
        {{ t(deleteError) }}
      </p>
      <AlertDialogFooter
        ><AlertDialogCancel :disabled="deleting">{{
          t("取消")
        }}</AlertDialogCancel
        ><Button
          variant="destructive"
          :disabled="deleting"
          :aria-busy="deleting"
          :aria-label="deleting ? t('正在删除') : undefined"
          @click="confirmDelete"
          ><Spinner v-if="deleting" />{{ t("确认删除") }}</Button
        ></AlertDialogFooter
      ></AlertDialogContent
    ></AlertDialog
  >
</template>
