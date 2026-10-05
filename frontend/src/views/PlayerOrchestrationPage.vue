<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { computed, onMounted, onUnmounted, ref } from "vue";
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  Circle,
  ExternalLink,
  FolderOpen,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Search,
} from "@lucide/vue";
import { toast } from "vue-sonner";
import CourseTreeNode from "@/components/CourseTreeNode.vue";
import { t } from "@/i18n";
import { formatDateTime } from "@/lib/datetime";
import {
  executionStageLabels,
  formatLogTime,
  presentExecutionLog,
} from "@/lib/orchestration-log";
import { playerAutomation } from "@/lib/player-automation";
import { buildCourseTree } from "@/lib/course-tree";
import {
  courseActivity,
  formatPlaybackProgress,
} from "@/lib/orchestration-display";
import {
  usePlayerOrchestrationStore,
  type QueueCourse,
} from "@/stores/player-orchestration";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyMedia,
} from "@/components/ui/empty";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from "@/components/ui/input-group";
import { Progress } from "@/components/ui/progress";
import { Separator } from "@/components/ui/separator";
import { Slider } from "@/components/ui/slider";
import { Spinner } from "@/components/ui/spinner";
import { Skeleton } from "@/components/ui/skeleton";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

type FlowStep = "select" | "review" | "settings" | "board";

const store = usePlayerOrchestrationStore();
const flowStep = ref<FlowStep>("review");
const permissionDialogOpen = ref(false);
const permissionOpening = ref(false);
const playerOpening = ref(false);
const permissionRevealOpening = ref(false);
let permissionCheckTimer: ReturnType<typeof setInterval> | undefined;
const logCourse = ref<QueueCourse | null>(null);
const logDialogOpen = ref(false);
function showLogs(course: QueueCourse) {
  logCourse.value = course;
  logDialogOpen.value = true;
}
const visibleLogs = computed(() =>
  store.logs
    .filter((entry) => {
      if (!logCourse.value) return true;
      return entry.courseId
        ? entry.courseId === logCourse.value.id
        : entry.course === logCourse.value.name &&
            store.courses.filter((course) => course.name === entry.course)
              .length === 1;
    })
    .slice()
    .reverse()
    .map((entry) => ({ ...entry, ...presentExecutionLog(entry) })),
);
const logLevels = { info: "信息", warn: "警告", error: "错误" };
const taskTitle = computed(() => {
  if (store.recovering) return "正在尝试恢复";
  if (store.runState === "completed") return "任务已完成";
  if (store.runState === "ended") return "任务已结束";
  if (store.runState === "paused")
    return store.activeCourse?.state === "error"
      ? "任务需要处理"
      : "任务已暂停";
  return store.runState === "running" ? "任务进行中" : "正在准备任务";
});
const endDialogOpen = ref(false);
const search = ref("");
const playbackRateStep = computed(() =>
  store.config.playbackRate === null
    ? 0
    : Math.round(store.config.playbackRate * 10) - 4,
);
const playbackRateLabel = computed(() =>
  store.config.playbackRate === null
    ? t("保持当前倍速")
    : `${store.config.playbackRate.toFixed(1)}×`,
);

function setPlaybackRateStep(value: number[] | undefined) {
  const step = value?.[0];
  if (step === undefined || !Number.isInteger(step) || step < 0 || step > 16)
    return;
  store.config.playbackRate = step === 0 ? null : (step + 4) / 10;
  store.updateConfig();
}

const filteredCourses = computed(() => {
  const query = search.value.trim().toLocaleLowerCase();
  if (!query) return store.availableCourses;
  return store.availableCourses.filter((course) =>
    course.name.toLocaleLowerCase().includes(query),
  );
});
const courseTree = computed(() => buildCourseTree(filteredCourses.value));
const selectedCourseIds = computed(
  () => new Set(store.selectedCourses.map((course) => course.id)),
);

const boardVisible = computed(
  () => flowStep.value === "board" || store.runState !== "idle",
);

const flowSteps = [
  { id: "review", index: 1, label: "开始前检查" },
  { id: "select", index: 2, label: "选择课程" },
  { id: "settings", index: 3, label: "播放设置" },
  { id: "board", index: 4, label: "执行任务" },
] as const;
const readinessItems = computed(() => [
  {
    label: "SzPlayer",
    ready:
      store.status?.installed === true &&
      store.status.running === true &&
      store.status.verifiedProfile === true,
  },
  {
    label: "辅助功能",
    ready: store.status?.accessibilityReady === true,
  },
  {
    label: "接收服务",
    ready: store.receiverConnected,
  },
  {
    label: "密钥历史",
    ready: store.historyReady,
  },
]);
const startBlockReason = computed(() => {
  if (store.statusLoading) return "正在检查运行条件";
  if (store.statusError) return "播放器状态读取失败，请重新检测";
  if (!store.status) return "正在检查运行条件";
  if (!store.status?.installed) return "";
  if (!store.status.running) return "";
  if (!store.status.verifiedProfile) return "当前 SzPlayer 版本尚未适配";
  if (!store.status.accessibilityReady) return "请先开启辅助功能权限";
  if (!store.historyReady) return "密钥历史仍在加载";
  if (!store.receiverConnected) return "接收服务尚未连接";
  if (store.captureActive) return "已有提取任务正在运行";
  return "";
});
const startBlocked = computed(
  () =>
    !store.status?.installed ||
    !store.status.running ||
    !!startBlockReason.value,
);

function queueBadge(state: QueueCourse["state"]) {
  if (state === "error") return "destructive" as const;
  if (state === "completed") return "secondary" as const;
  return "outline" as const;
}

async function pauseTask() {
  try {
    await store.pause();
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}

async function resumeTask() {
  try {
    await store.resume();
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}

async function endTask() {
  try {
    await store.endTask();
    endDialogOpen.value = false;
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}

function flowStepState(id: FlowStep) {
  const current = flowSteps.findIndex((step) => step.id === flowStep.value);
  const target = flowSteps.findIndex((step) => step.id === id);
  return target < current
    ? "done"
    : target === current
      ? "current"
      : "upcoming";
}

async function refreshCourses() {
  await store.refreshStatus();
  if (startBlocked.value) {
    flowStep.value = "review";
    return;
  }
  await loadCourses();
}

async function loadCourses() {
  try {
    await store.refreshCourses();
    if (!store.availableCourses.length) search.value = "";
  } catch {
    // The selection step displays the courseError from the store.
  }
}

async function continueToSelection() {
  await store.refreshStatus();
  if (startBlocked.value) return;
  flowStep.value = "select";
  await loadCourses();
}

async function continueToSettings() {
  if (!store.canContinueSelection) return;
  await store.refreshStatus();
  if (startBlocked.value) flowStep.value = "review";
  else if (store.canContinueSelection) flowStep.value = "settings";
}

async function startTask() {
  if (!store.canStart) return;
  flowStep.value = "board";
  try {
    await store.run();
  } catch (error) {
    toast.error(t((error as Error).message));
    flowStep.value = "review";
  } finally {
    void store.refreshStatus();
  }
}

function startNewTask() {
  store.resetQueue();
  logCourse.value = null;
  search.value = "";
  flowStep.value = "review";
}

async function openPlayer() {
  if (playerOpening.value) return;
  playerOpening.value = true;
  try {
    await playerAutomation.openPlayer();
    // The app needs a moment to start; check twice so the row turns ready by itself.
    for (const delay of [1500, 3500]) {
      await new Promise((resolve) => setTimeout(resolve, delay));
      await store.refreshStatus();
      if (store.status?.running) break;
    }
  } catch (error) {
    toast.error(t((error as Error).message));
  } finally {
    playerOpening.value = false;
  }
}

async function openPermissionSettings() {
  if (permissionOpening.value) return;
  permissionDialogOpen.value = true;
  permissionOpening.value = true;
  try {
    await playerAutomation.openPermissionSettings();
  } catch (error) {
    toast.error(t((error as Error).message));
  } finally {
    permissionOpening.value = false;
  }
}

async function revealAppForPermission() {
  if (permissionRevealOpening.value) return;
  permissionRevealOpening.value = true;
  try {
    await playerAutomation.revealAppForPermission();
  } catch (error) {
    toast.error(t((error as Error).message));
  } finally {
    permissionRevealOpening.value = false;
  }
}

async function refreshPermissionStatus() {
  await store.refreshStatus();
  if (store.status?.accessibilityReady) permissionDialogOpen.value = false;
}

function refreshPermissionOnFocus() {
  if (permissionDialogOpen.value && !store.statusLoading) {
    void refreshPermissionStatus();
  }
}

onMounted(() => {
  store.hydrate();
  void store.refreshStatus();
  window.addEventListener("focus", refreshPermissionOnFocus);
  permissionCheckTimer = setInterval(refreshPermissionOnFocus, 500);
});
onUnmounted(() => {
  window.removeEventListener("focus", refreshPermissionOnFocus);
  if (permissionCheckTimer) clearInterval(permissionCheckTimer);
});
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" :aria-label="t('播放编排')">
    <template v-if="!boardVisible">
      <nav
        class="mx-auto mb-6 w-full max-w-4xl px-2"
        :aria-label="t('任务步骤')"
      >
        <ol class="grid grid-cols-4">
          <li
            v-for="(step, index) in flowSteps"
            :key="step.id"
            class="relative flex items-center"
          >
            <span
              v-if="index"
              class="absolute right-1/2 left-0 h-px bg-border"
              aria-hidden="true"
            />
            <span
              v-if="index < flowSteps.length - 1"
              class="absolute right-0 left-1/2 h-px bg-border"
              aria-hidden="true"
            />
            <div
              class="relative z-10 mx-auto flex items-center gap-2 bg-(--canvas) px-3"
            >
              <span
                class="grid size-7 place-items-center rounded-full border text-xs font-medium tabular-nums"
                :class="
                  flowStepState(step.id) === 'current'
                    ? 'border-primary bg-primary text-primary-foreground'
                    : flowStepState(step.id) === 'done'
                      ? 'border-foreground bg-foreground text-background'
                      : 'bg-card text-muted-foreground'
                "
              >
                <Check
                  v-if="flowStepState(step.id) === 'done'"
                  class="size-3.5"
                />
                <template v-else>{{ step.index }}</template>
              </span>
              <span
                class="hidden text-sm sm:inline"
                :class="
                  flowStepState(step.id) === 'current'
                    ? 'font-medium text-foreground'
                    : 'text-muted-foreground'
                "
                >{{ t(step.label) }}</span
              >
            </div>
          </li>
        </ol>
      </nav>

      <div
        v-if="flowStep === 'select'"
        class="mx-auto flex w-full max-w-4xl flex-col"
      >
        <div
          v-if="store.courseLoading"
          role="status"
          :aria-label="t('正在获取课程')"
          class="h-[clamp(460px,calc(100dvh-300px),560px)] overflow-hidden rounded-lg border bg-card"
        >
          <div class="flex h-14 items-center gap-3 border-b px-4">
            <Skeleton class="h-8 w-32" />
            <Skeleton class="h-8 min-w-0 flex-1" />
          </div>
          <div class="space-y-1 p-4">
            <Skeleton v-for="row in 7" :key="row" class="h-10 w-full" />
          </div>
        </div>

        <div
          v-else-if="store.availableCourses.length"
          class="flex h-[clamp(460px,calc(100dvh-300px),560px)] min-h-0 flex-col overflow-hidden rounded-lg border bg-card"
        >
          <div class="flex flex-wrap items-center gap-3 border-b px-4 py-3">
            <InputGroup class="min-w-56 flex-1">
              <InputGroupAddon><Search /></InputGroupAddon>
              <InputGroupInput
                v-model="search"
                :placeholder="t('搜索课程')"
                :aria-label="t('搜索课程')"
              />
            </InputGroup>
            <span class="text-xs tabular-nums text-muted-foreground">{{
              t("已选择 {selected} / {total} 节", {
                selected: store.selectedCourses.length,
                total: store.availableCourses.length,
              })
            }}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              :disabled="store.courseLoading || store.statusLoading"
              :aria-label="t('重新获取课程')"
              :title="t('重新获取课程')"
              @click="refreshCourses"
            >
              <Spinner v-if="store.courseLoading" /><RefreshCw v-else />
            </Button>
          </div>
          <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain">
            <div :key="search">
              <CourseTreeNode
                v-for="node in courseTree"
                :key="node.key"
                :node="node"
                :selected-ids="selectedCourseIds"
                :disabled="!store.canEditSelection"
                @select-course="store.setCourseSelected"
                @select-folder="store.setCoursesSelected"
              />
            </div>
            <Empty v-if="!filteredCourses.length" class="min-h-40 border-0"
              ><EmptyHeader
                ><EmptyTitle>{{ t("没有匹配的课程") }}</EmptyTitle>
                <EmptyDescription>{{
                  t("换个关键词，或清空搜索查看全部课程。")
                }}</EmptyDescription></EmptyHeader
              >
              <Button variant="outline" size="sm" @click="search = ''">{{
                t("清空搜索")
              }}</Button>
            </Empty>
          </div>
        </div>

        <Empty
          v-else
          class="h-[clamp(460px,calc(100dvh-300px),560px)] flex-none border bg-card"
        >
          <EmptyHeader>
            <EmptyMedia variant="icon"
              ><AlertCircle
                v-if="store.courseError"
                class="text-destructive" /><FolderOpen v-else
            /></EmptyMedia>
            <EmptyTitle>{{
              t(store.courseError ? "课程读取失败" : "暂无课程")
            }}</EmptyTitle>
            <EmptyDescription>{{
              store.courseError ||
              t("请先在 SzPlayer 中添加课程文件或目录，再重新获取。")
            }}</EmptyDescription>
          </EmptyHeader>
          <Button
            :disabled="store.courseLoading || store.statusLoading"
            @click="refreshCourses"
          >
            <Spinner v-if="store.courseLoading" /><RefreshCw v-else />{{
              t(store.courseError ? "重试读取" : "重新获取课程")
            }}
          </Button>
        </Empty>

        <div class="mt-4 flex items-center justify-between border-t pt-4">
          <Button variant="ghost" @click="flowStep = 'review'">
            <ArrowLeft />{{ t("上一步") }}
          </Button>
          <Button
            :disabled="!store.canContinueSelection || store.statusLoading"
            @click="continueToSettings"
          >
            {{ t("下一步") }}<ArrowRight />
          </Button>
        </div>
      </div>

      <div
        v-else-if="flowStep === 'review'"
        class="mx-auto flex w-full max-w-4xl flex-col"
      >
        <Alert v-if="store.statusError" variant="destructive" class="mb-4">
          <AlertCircle />
          <AlertTitle>{{ t("播放器状态读取失败") }}</AlertTitle>
          <AlertDescription>{{ store.statusError }}</AlertDescription>
        </Alert>
        <section
          class="flex h-[clamp(460px,calc(100dvh-300px),560px)] flex-col overflow-hidden rounded-lg border bg-card"
        >
          <div
            class="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-4"
          >
            <h3 class="text-sm font-medium">{{ t("运行条件") }}</h3>
            <Button
              variant="ghost"
              size="icon-sm"
              :disabled="store.statusLoading"
              :aria-label="t('重新检查')"
              :title="t('重新检查')"
              @click="store.refreshStatus"
            >
              <Spinner v-if="store.statusLoading" /><RefreshCw v-else />
            </Button>
          </div>
          <div
            v-if="store.statusLoading && !store.status"
            role="status"
            :aria-label="t('正在读取播放器状态')"
            class="min-h-0 flex-1 divide-y px-4"
          >
            <div
              v-for="row in 6"
              :key="row"
              class="flex min-h-12 items-center gap-3 py-2"
            >
              <Skeleton class="size-4" />
              <Skeleton class="h-4 w-36" />
            </div>
          </div>
          <div v-else class="min-h-0 flex-1 divide-y px-4">
            <div
              v-for="item in readinessItems"
              :key="item.label"
              class="flex min-h-12 items-center gap-3 py-2"
            >
              <CheckCircle2
                v-if="item.ready"
                class="size-4 shrink-0 text-success"
              />
              <AlertCircle v-else class="size-4 shrink-0 text-warning" />
              <span class="text-sm">{{ t(item.label) }}</span>
              <span class="sr-only">{{
                t(item.ready ? "正常" : "未就绪")
              }}</span>
              <Button
                v-if="
                  item.label === 'SzPlayer' &&
                  store.status?.platform === 'darwin' &&
                  store.status.installed &&
                  !item.ready
                "
                variant="outline"
                size="sm"
                class="ml-auto"
                :disabled="playerOpening"
                @click="openPlayer"
              >
                <Spinner v-if="playerOpening" /><ExternalLink v-else />{{
                  t("打开 SzPlayer")
                }}
              </Button>
              <Button
                v-if="
                  item.label === '辅助功能' &&
                  store.status?.platform === 'darwin' &&
                  !item.ready
                "
                variant="outline"
                size="sm"
                class="ml-auto"
                :disabled="permissionOpening"
                @click="openPermissionSettings"
              >
                <Spinner v-if="permissionOpening" /><ExternalLink v-else />{{
                  t("打开权限设置")
                }}
              </Button>
            </div>
          </div>
        </section>

        <div
          class="mt-4 flex flex-wrap items-center justify-end gap-3 border-t pt-4"
        >
          <span v-if="startBlockReason" class="text-sm text-warning">{{
            t(startBlockReason)
          }}</span>
          <Button :disabled="startBlocked" @click="continueToSelection">
            {{ t("下一步") }}<ArrowRight />
          </Button>
        </div>
      </div>

      <div v-else class="mx-auto w-full max-w-4xl">
        <div
          class="h-[clamp(460px,calc(100dvh-300px),560px)] overflow-y-auto overscroll-contain rounded-lg border bg-card p-5 sm:p-6"
        >
          <div>
            <h2 class="mb-5 text-sm font-semibold">{{ t("播放控制") }}</h2>
            <div class="grid gap-x-6 gap-y-5 sm:grid-cols-2">
              <Field>
                <FieldLabel for="playback-volume">{{
                  t("播放音量")
                }}</FieldLabel>
                <InputGroup>
                  <InputGroupInput
                    id="playback-volume"
                    v-model="store.config.playbackVolumePercent"
                    type="text"
                    inputmode="numeric"
                    :aria-label="t('播放音量百分比')"
                    @change="store.updateConfig"
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupText>%</InputGroupText>
                  </InputGroupAddon>
                </InputGroup>
              </Field>
              <Field>
                <FieldLabel id="playback-rate-label">{{
                  t("播放倍速")
                }}</FieldLabel>
                <div class="flex h-8 min-w-0 items-center gap-3">
                  <Slider
                    id="playback-rate"
                    class="min-w-0 flex-1"
                    :model-value="[playbackRateStep]"
                    :min="0"
                    :max="16"
                    :step="1"
                    :thumb-label="t('播放倍速')"
                    :thumb-value-text="playbackRateLabel"
                    @update:model-value="setPlaybackRateStep"
                  />
                  <output
                    for="playback-rate"
                    class="shrink-0 text-right text-sm font-medium tabular-nums whitespace-nowrap"
                    :class="
                      store.config.playbackRate === null
                        ? 'text-muted-foreground'
                        : 'text-foreground'
                    "
                    >{{ playbackRateLabel }}</output
                  >
                </div>
              </Field>
            </div>
          </div>
          <Separator class="my-6" />
          <div>
            <h2 class="mb-5 text-sm font-semibold">{{ t("执行节奏") }}</h2>
            <div class="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel for="action-delay">{{
                  t("操作间隔（毫秒）")
                }}</FieldLabel>
                <Input
                  id="action-delay"
                  v-model="store.config.actionDelayMs"
                  type="text"
                  inputmode="numeric"
                  @change="store.updateConfig"
                />
              </Field>
              <Field>
                <FieldLabel for="course-ready-timeout">{{
                  t("等待课程（秒）")
                }}</FieldLabel>
                <Input
                  id="course-ready-timeout"
                  v-model="store.config.courseReadyTimeoutSeconds"
                  type="text"
                  inputmode="numeric"
                  @change="store.updateConfig"
                />
              </Field>
              <Field>
                <FieldLabel for="poll-delay">{{
                  t("检查间隔（毫秒）")
                }}</FieldLabel>
                <Input
                  id="poll-delay"
                  v-model="store.config.pollIntervalMs"
                  type="text"
                  inputmode="numeric"
                  @change="store.updateConfig"
                />
              </Field>
              <Field>
                <FieldLabel for="max-minutes">{{
                  t("单课超时（分钟）")
                }}</FieldLabel>
                <Input
                  id="max-minutes"
                  v-model="store.config.maxPlaybackMinutes"
                  type="text"
                  inputmode="numeric"
                  @change="store.updateConfig"
                />
                <FieldDescription>{{
                  t("正常播放会自动延长等待，不限制视频总时长。")
                }}</FieldDescription>
              </Field>
            </div>
          </div>
        </div>

        <div
          class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t pt-4"
        >
          <Button variant="ghost" @click="flowStep = 'select'">
            <ArrowLeft />{{ t("上一步") }}
          </Button>
          <div class="flex flex-wrap items-center justify-end gap-3">
            <span v-if="startBlockReason" class="text-sm text-warning">{{
              t(startBlockReason)
            }}</span>
            <Button :disabled="!store.canStart" @click="startTask">
              <Play />{{ t("开始任务") }}
            </Button>
          </div>
        </div>
      </div>
    </template>

    <template v-else>
      <div
        class="mb-4 flex flex-wrap items-center gap-x-5 gap-y-3 border-b pb-4"
      >
        <div class="min-w-0 flex-1">
          <h2 class="text-lg font-semibold">
            {{ t(taskTitle) }}
          </h2>
        </div>
        <div class="w-48 shrink-0">
          <div
            class="mb-1.5 flex justify-between text-xs text-muted-foreground"
          >
            <span>{{ t("任务进度") }}</span>
            <span class="tabular-nums">{{
              `${store.processedCount} / ${store.courses.length}`
            }}</span>
          </div>
          <Progress :model-value="store.progress" />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <Button
            v-if="
              store.runState === 'running' &&
              store.currentStep === 'waitingCompletion'
            "
            variant="outline"
            @click="store.markPlaybackComplete"
          >
            <CheckCircle2 />{{ t("确认本课完成") }}
          </Button>
          <Button
            v-if="store.runState === 'running'"
            variant="outline"
            :disabled="store.operationBusy"
            @click="pauseTask"
          >
            <Pause />{{ t("暂停") }}
          </Button>
          <Button
            v-else-if="store.runState === 'paused'"
            :disabled="store.operationBusy || store.recovering"
            @click="resumeTask"
          >
            <Play />{{
              t(store.activeCourse?.pendingCapture ? "重试保存" : "继续执行")
            }}
          </Button>
          <Button v-else variant="outline" @click="startNewTask">
            <Plus />{{ t("新建任务") }}
          </Button>
          <Button
            v-if="store.runState === 'running' || store.runState === 'paused'"
            variant="outline"
            :disabled="store.operationBusy"
            @click="endDialogOpen = true"
            >{{ t("结束任务") }}</Button
          >
        </div>
      </div>

      <div class="flex min-h-64 flex-1">
        <section
          class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-lg border bg-card"
        >
          <div class="data-list-scroll min-h-0 flex-1">
            <DataTable class="table-fixed">
              <TableHeader>
                <TableRow>
                  <TableHead class="w-12">#</TableHead>
                  <TableHead>{{ t("课程") }}</TableHead>
                  <TableHead class="w-40">{{ t("播放进度") }}</TableHead>
                  <TableHead class="w-40 text-center">{{
                    t("执行状态")
                  }}</TableHead>
                  <TableHead class="w-24 text-center">{{
                    t("日志")
                  }}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow
                  v-for="(course, index) in store.courses"
                  :key="course.id"
                  :class="
                    index === store.currentIndex &&
                    store.runState !== 'completed'
                      ? 'bg-muted/45'
                      : ''
                  "
                >
                  <TableCell class="tabular-nums text-muted-foreground">{{
                    index + 1
                  }}</TableCell>
                  <TableCell class="whitespace-normal break-words">
                    <p class="font-medium">{{ course.name }}</p>
                  </TableCell>
                  <TableCell
                    class="tabular-nums text-xs text-muted-foreground"
                    >{{
                      t(
                        course.state === "completed" && !course.playback
                          ? "已完成"
                          : formatPlaybackProgress(course.playback),
                      )
                    }}</TableCell
                  >
                  <TableCell class="text-center">
                    <Badge :variant="queueBadge(course.state)">
                      <Spinner
                        v-if="
                          course.state === 'running' &&
                          store.runState === 'running'
                        "
                      />
                      <Pause
                        v-else-if="
                          course.state === 'running' &&
                          store.runState === 'paused'
                        "
                      />
                      <CheckCircle2 v-else-if="course.state === 'completed'" />
                      <AlertCircle v-else-if="course.state === 'error'" />
                      <Circle v-else />{{
                        t(
                          courseActivity(
                            course.state,
                            store.runState,
                            index === store.currentIndex
                              ? store.currentStep
                              : undefined,
                            !!course.pendingCapture,
                          ),
                        )
                      }}
                    </Badge>
                  </TableCell>
                  <TableCell class="text-center">
                    <Button variant="ghost" size="sm" @click="showLogs(course)">
                      {{ t("查看") }}
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </DataTable>
          </div>
        </section>
      </div>
    </template>
  </section>

  <Dialog v-model:open="logDialogOpen">
    <DialogContent
      :aria-describedby="undefined"
      class="max-h-[85vh] overflow-y-auto sm:max-w-5xl"
    >
      <DialogHeader>
        <DialogTitle>{{ t("执行日志") }}</DialogTitle>
      </DialogHeader>
      <Alert
        v-if="
          logCourse?.detail &&
          !visibleLogs.some((entry) => entry.detail === logCourse?.detail)
        "
        variant="destructive"
        class="m-4 w-auto"
      >
        <AlertCircle />
        <AlertTitle
          >{{ t("执行失败") }} ·
          {{ formatDateTime(logCourse.failedAt) }}</AlertTitle
        >
        <AlertDescription class="whitespace-pre-wrap break-words">{{
          logCourse.detail
        }}</AlertDescription>
      </Alert>
      <div
        class="flex min-h-48 max-h-[55vh] flex-col overflow-hidden rounded-lg border"
      >
        <DataTable v-if="visibleLogs.length" class="table-fixed">
          <TableHeader>
            <TableRow>
              <TableHead class="w-52">{{ t("时间") }}</TableHead>
              <TableHead class="w-16">{{ t("级别") }}</TableHead>
              <TableHead class="w-32">{{ t("阶段") }}</TableHead>
              <TableHead>{{ t("动作与结果") }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="entry in visibleLogs" :key="entry.id">
              <TableCell
                class="align-top font-mono text-xs tabular-nums whitespace-nowrap"
              >
                <time :datetime="entry.at">{{ formatLogTime(entry.at) }}</time>
              </TableCell>
              <TableCell class="align-top"
                ><Badge
                  :variant="entry.level === 'error' ? 'destructive' : 'outline'"
                  :class="entry.level === 'warn' ? 'text-warning' : ''"
                  >{{ t(logLevels[entry.level]) }}</Badge
                ></TableCell
              >
              <TableCell
                class="align-top whitespace-normal break-words text-xs"
              >
                <p v-if="entry.step" class="text-muted-foreground">
                  {{ t(executionStageLabels[entry.step]) }}
                </p>
              </TableCell>
              <TableCell
                class="align-top whitespace-normal break-words text-xs"
              >
                <p class="whitespace-pre-wrap [overflow-wrap:anywhere]">
                  {{ entry.message }}
                </p>
                <pre
                  v-if="entry.displayDetail"
                  class="mt-1 max-w-full whitespace-pre-wrap [overflow-wrap:anywhere] font-mono text-xs leading-5 text-muted-foreground"
                  >{{ entry.displayDetail }}</pre>
              </TableCell>
            </TableRow>
          </TableBody>
        </DataTable>
        <Empty v-else class="py-8"
          ><EmptyHeader
            ><EmptyTitle class="text-sm">{{
              t("暂无日志")
            }}</EmptyTitle></EmptyHeader
          ></Empty
        >
      </div>
    </DialogContent>
  </Dialog>

  <Dialog v-model:open="endDialogOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t("结束任务？") }}</DialogTitle>
        <DialogDescription>{{
          t("将结束当前编排，并尝试停止播放、保存已采集的内容。")
        }}</DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <Button variant="outline" @click="endDialogOpen = false">{{
          t("继续任务")
        }}</Button>
        <Button
          variant="destructive"
          :disabled="store.operationBusy"
          @click="endTask"
          >{{ t("结束任务") }}</Button
        >
      </DialogFooter>
    </DialogContent>
  </Dialog>

  <Dialog v-model:open="permissionDialogOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t("需要辅助功能权限") }}</DialogTitle>
        <DialogDescription>{{
          t("播放编排需要此权限来读取并操作 SzPlayer 界面。")
        }}</DialogDescription>
      </DialogHeader>
      <ol class="space-y-3 text-sm">
        <li class="flex gap-3">
          <span
            class="grid size-6 shrink-0 place-items-center rounded-full bg-muted text-xs"
            >1</span
          >
          <span class="pt-0.5">{{ t("打开系统设置中的“隐私与安全”。") }}</span>
        </li>
        <li class="flex gap-3">
          <span
            class="grid size-6 shrink-0 place-items-center rounded-full bg-muted text-xs"
            >2</span
          >
          <span class="pt-0.5">{{
            t("macOS 27 进入“设备控制和数据访问”；较早版本进入“辅助功能”。")
          }}</span>
        </li>
        <li class="flex gap-3">
          <span
            class="grid size-6 shrink-0 place-items-center rounded-full bg-muted text-xs"
            >3</span
          >
          <span class="pt-0.5">{{
            t("在列表中添加并开启“饼干大小姐”，返回后重新检测。")
          }}</span>
        </li>
      </ol>
      <Button
        v-if="store.status?.platform === 'darwin'"
        variant="outline"
        class="w-full"
        :disabled="permissionRevealOpening"
        @click="revealAppForPermission"
      >
        <Spinner v-if="permissionRevealOpening" /><FolderOpen v-else />{{
          t("在访达中显示应用")
        }}
      </Button>
      <DialogFooter>
        <Button
          variant="outline"
          :disabled="store.statusLoading"
          @click="refreshPermissionStatus"
        >
          <Spinner v-if="store.statusLoading" /><RefreshCw v-else />{{
            t("重新检测")
          }}
        </Button>
        <Button :disabled="permissionOpening" @click="openPermissionSettings">
          <Spinner v-if="permissionOpening" /><ExternalLink v-else />{{
            t("再次打开权限设置")
          }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
