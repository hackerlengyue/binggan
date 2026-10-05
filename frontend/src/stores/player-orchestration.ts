import { computed, reactive, ref } from "vue";
import { defineStore } from "pinia";
import { isMyGo } from "mygo-runtime";
import {
  playerAutomation,
  type PlayerAutomationStatus,
} from "@/lib/player-automation";
import {
  createAbortableSleep,
  courseIdentity,
  courseCaptureName,
  CaptureSaveFailed,
  discoverCourses,
  executeCourse,
  isFinalFrameProgress,
  OrchestrationInterrupted,
  readPlaybackProgress,
  validatePlaybackRecovery,
  type PlaybackRecoveryRequest,
  type CaptureCheckpoint,
  type Course,
  type CourseClickMatch,
  type OrchestrationConfig,
  type OrchestrationStep,
  type PlaybackProgress,
} from "@/lib/player-orchestration";
import { useTraceStore } from "@/stores/trace";
import { formatPlaybackProgress } from "@/lib/orchestration-display";
import {
  executionStepMessages,
  retainExecutionLogs,
  captureResultDetail,
  type ExecutionLogMeta,
} from "@/lib/orchestration-log";
import { desktopNotifications } from "@/lib/notifications";

const STORAGE_KEY = "szjm.player-orchestration.v1";
const MAX_LOGS = 500;

export type QueueState =
  "pending" | "running" | "completed" | "error" | "ended";
export type QueueCourse = Course & {
  state: QueueState;
  detail?: string;
  failedAt?: string;
  captureId?: string;
  playbackAttempted?: boolean;
  playback?: PlaybackProgress;
  pendingCapture?: {
    id: string;
    name: string;
    needsStop?: boolean;
    playbackCompleted?: boolean;
  };
};
export type OrchestrationLog = ExecutionLogMeta & {
  id: string;
  at: string;
  level: "info" | "warn" | "error";
  course?: string;
  courseId?: string;
  step?: OrchestrationStep;
  message: string;
  detail?: string;
};

const defaultConfig: OrchestrationConfig = {
  playbackVolumePercent: 35,
  playbackRate: null,
  actionDelayMs: 1500,
  courseReadyTimeoutSeconds: 45,
  pollIntervalMs: 500,
  maxPlaybackMinutes: 180,
};

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

function playbackToggleUndelivered(error: unknown) {
  const message = errorMessage(error);
  return (
    message.includes("未暴露播放控制栏") ||
    message.includes("播放控件读取未完成") ||
    message.includes("无法确认 SzPlayer 播放控件位置") ||
    message.includes("播放窗口不可见") ||
    message.includes("播放窗口已最小化")
  );
}

function restoredPlayback(value: unknown): PlaybackProgress | undefined {
  if (!value || typeof value !== "object") return;
  const source = value as PlaybackProgress;
  const result: PlaybackProgress = {};
  for (const key of ["seconds", "totalSeconds", "percent"] as const) {
    const number = source[key];
    if (
      typeof number === "number" &&
      Number.isFinite(number) &&
      number >= 0 &&
      (key !== "percent" || number <= 100)
    )
      result[key] = number;
  }
  if (
    result.seconds !== undefined &&
    result.totalSeconds !== undefined &&
    result.seconds > result.totalSeconds
  )
    return;
  return Object.keys(result).length ? result : undefined;
}

function validNumber(
  value: unknown,
  fallback: number,
  min: number,
  max: number,
) {
  if (
    (typeof value !== "number" && typeof value !== "string") ||
    (typeof value === "string" && !value.trim())
  )
    return fallback;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= min && parsed <= max
    ? parsed
    : fallback;
}

function validPlaybackRate(value: unknown): number | null {
  if (value === null || value === undefined || value === "") return null;
  if (typeof value !== "number" && typeof value !== "string") return null;
  const rate = Number(value);
  if (
    !Number.isFinite(rate) ||
    rate < 0.5 ||
    rate > 2 ||
    Math.abs(rate * 10 - Math.round(rate * 10)) > 0.000001
  )
    return null;
  return Number(rate.toFixed(1));
}

function restoredRunConfig(
  value: unknown,
  fallback: OrchestrationConfig,
): OrchestrationConfig {
  const saved =
    value && typeof value === "object"
      ? (value as Partial<OrchestrationConfig>)
      : {};
  return {
    playbackVolumePercent: validNumber(
      saved.playbackVolumePercent,
      fallback.playbackVolumePercent,
      0,
      100,
    ),
    playbackRate:
      saved.playbackRate === undefined
        ? fallback.playbackRate
        : validPlaybackRate(saved.playbackRate),
    actionDelayMs: validNumber(
      saved.actionDelayMs,
      fallback.actionDelayMs,
      300,
      15000,
    ),
    courseReadyTimeoutSeconds: validNumber(
      saved.courseReadyTimeoutSeconds,
      fallback.courseReadyTimeoutSeconds,
      5,
      600,
    ),
    pollIntervalMs: validNumber(
      saved.pollIntervalMs,
      fallback.pollIntervalMs,
      500,
      30000,
    ),
    maxPlaybackMinutes: validNumber(
      saved.maxPlaybackMinutes,
      fallback.maxPlaybackMinutes,
      1,
      1440,
    ),
  };
}

function restoredCourses(value: unknown): Course[] {
  if (!Array.isArray(value)) return [];
  const seen = new Set<string>();
  return value.flatMap((item) => {
    if (
      !item ||
      typeof item.id !== "string" ||
      !item.id.trim() ||
      typeof item.name !== "string" ||
      !item.name.trim()
    )
      return [];
    const course: Course = {
      id: item.id,
      name: item.name.trim(),
      ...(typeof item.sourcePath === "string"
        ? { sourcePath: item.sourcePath }
        : {}),
      ...(typeof item.targetText === "string"
        ? { targetText: item.targetText }
        : {}),
    };
    course.id = courseIdentity(course);
    if (seen.has(course.id)) return [];
    seen.add(course.id);
    return [course];
  });
}

export const usePlayerOrchestrationStore = defineStore(
  "player-orchestration",
  () => {
    const trace = useTraceStore();
    const config = reactive<OrchestrationConfig>({ ...defaultConfig });
    const availableCourses = ref<Course[]>([]);
    const selectedCourses = ref<Course[]>([]);
    const courses = ref<QueueCourse[]>([]);
    let runConfig: OrchestrationConfig | undefined;
    const logs = ref<OrchestrationLog[]>([]);
    const runState = ref<"idle" | "running" | "paused" | "completed" | "ended">(
      "idle",
    );
    const currentIndex = ref(0);
    const currentStep = ref<OrchestrationStep>();
    const status = ref<PlayerAutomationStatus>();
    const statusLoading = ref(false);
    const statusError = ref("");
    const courseLoading = ref(false);
    const courseCatalogReady = ref(false);
    const courseError = ref("");
    const manualComplete = ref(false);
    const startingRun = ref(false);
    let controller: AbortController | undefined;
    let runFinished: Promise<void> | undefined;
    let finishRun: (() => void) | undefined;
    let pauseRequested = false;
    let capturePausedForTask = false;
    let playbackPausedForTask = false;
    let playbackPausePending: Promise<void> | undefined;
    let playbackPauseFailure: Error | undefined;
    let playbackPid: number | undefined;
    let playbackStartedForCourse = false;
    let playbackContinuedForCourse = false;
    let ownedCaptureId: string | undefined;
    let queuePersistenceError = "";
    let playbackClickInFlight: Promise<void> | undefined;
    let playbackClickDelivered = false;
    let stopRequested = false;
    let endNeedsAttention = false;
    let releasePause: (() => void) | undefined;
    let pauseWait: Promise<void> | undefined;
    const transitioning = ref(false);
    const recovering = ref(false);
    let recoveryRequest: PlaybackRecoveryRequest | undefined;
    let automaticRecoveryAttempts = 0;
    let hydrated = false;
    let courseSelectionInitialized = false;

    const activeCourse = computed(() => courses.value[currentIndex.value]);
    const processedCount = computed(
      () =>
        courses.value.filter((course) => course.state === "completed").length,
    );
    const progress = computed(() =>
      courses.value.length
        ? Math.round((processedCount.value / courses.value.length) * 100)
        : 0,
    );
    const captureActive = computed(() => trace.captureEnabled);
    const receiverConnected = computed(() => trace.connected);
    const historyReady = computed(() => trace.historyReady);
    const operationBusy = computed(
      () => startingRun.value || transitioning.value,
    );
    const canEditSelection = computed(
      () =>
        runState.value === "idle" &&
        !controller &&
        !operationBusy.value &&
        !courseLoading.value,
    );
    const canContinueSelection = computed(() => {
      if (
        !courseCatalogReady.value ||
        courseLoading.value ||
        courseError.value ||
        !selectedCourses.value.length
      )
        return false;
      const availableIds = new Set(availableCourses.value.map(courseIdentity));
      return selectedCourses.value.every((course) =>
        availableIds.has(courseIdentity(course)),
      );
    });
    const canStart = computed(
      () =>
        !startingRun.value &&
        !transitioning.value &&
        !controller &&
        !courseLoading.value &&
        ((runState.value === "idle" && canContinueSelection.value) ||
          (runState.value === "paused" &&
            currentIndex.value < courses.value.length)) &&
        historyReady.value &&
        (!captureActive.value ||
          (activeCourse.value?.pendingCapture &&
            (!activeCourse.value.pendingCapture.needsStop ||
              archivedCapture(activeCourse.value.pendingCapture.id) ||
              storedPartialCapture(activeCourse.value.pendingCapture))) ||
          (activeCourse.value?.pendingCapture?.needsStop === true &&
            activeCourse.value.pendingCapture.id === trace.currentHistoryId)) &&
        ((runState.value === "paused" &&
          !!activeCourse.value?.pendingCapture) ||
          (status.value?.installed === true &&
            status.value?.running === true &&
            status.value?.verifiedProfile === true &&
            status.value?.accessibilityReady === true &&
            receiverConnected.value)),
    );

    function appendLog(
      level: OrchestrationLog["level"],
      message: string,
      detail?: string,
      course = activeCourse.value?.name,
      metadata: ExecutionLogMeta = {},
    ) {
      logs.value.push({
        id: crypto.randomUUID(),
        at: new Date().toISOString(),
        level,
        course,
        courseId:
          course === activeCourse.value?.name
            ? activeCourse.value?.id
            : undefined,
        step: currentStep.value,
        ...metadata,
        message,
        detail: detail || undefined,
      });
      logs.value = retainExecutionLogs(logs.value, MAX_LOGS);
      persist();
    }

    function setCurrentStep(step: OrchestrationStep) {
      if (currentStep.value === step) return;
      currentStep.value = step;
      const entry = executionStepMessages[step];
      if (entry) appendLog("info", entry[0], entry[1]);
    }

    function notify(result: Promise<void>) {
      void result.catch((error) =>
        appendLog("warn", "系统通知发送失败", errorMessage(error)),
      );
    }

    function persist() {
      if (!hydrated) return false;
      try {
        localStorage.setItem(
          STORAGE_KEY,
          JSON.stringify({
            version: 10,
            config,
            courseSelectionInitialized,
            availableCourses: availableCourses.value,
            selectedCourses: selectedCourses.value.map(
              ({ id, name, targetText, sourcePath }) => ({
                id,
                name,
                targetText,
                sourcePath,
              }),
            ),
            queue:
              runState.value === "idle"
                ? undefined
                : {
                    courses: courses.value,
                    runState: runState.value,
                    currentIndex: currentIndex.value,
                    config: runConfig,
                  },
            logs: logs.value,
          }),
        );
        queuePersistenceError = "";
        return true;
      } catch {
        queuePersistenceError =
          "无法保存任务执行状态，请释放存储空间后重试；当前任务不会继续打开课程";
        return false;
      }
    }

    function requirePersistedQueue() {
      if (!persist()) throw new Error(queuePersistenceError);
    }

    function archivedCapture(id: string) {
      return (
        !trace.historyError &&
        trace.currentHistoryId !== id &&
        trace.keyHistory.some(
          (entry) =>
            entry.id === id &&
            !!entry.stoppedAt &&
            Number.isFinite(Date.parse(entry.stoppedAt)),
        )
      );
    }

    function requireOwnedCapture() {
      if (!ownedCaptureId || trace.currentHistoryId !== ownedCaptureId)
        throw new Error(
          "当前提取记录已变化，与本课记录不一致；不会操作其他提取任务",
        );
    }

    function storedPartialCapture(pending: QueueCourse["pendingCapture"]) {
      return (
        !!pending &&
        pending.playbackCompleted === false &&
        !trace.historyError &&
        trace.currentHistoryId !== pending.id &&
        trace.keyHistory.some((entry) => entry.id === pending.id)
      );
    }

    function hydrate() {
      if (hydrated) return;
      try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (raw) {
          const saved = JSON.parse(raw) as Record<string, unknown>;
          if (saved.version !== 10) {
            hydrated = true;
            return;
          }
          const savedConfig = (saved.config ??
            {}) as Partial<OrchestrationConfig>;
          Object.assign(config, {
            playbackVolumePercent: validNumber(
              savedConfig.playbackVolumePercent,
              defaultConfig.playbackVolumePercent,
              0,
              100,
            ),
            playbackRate: validPlaybackRate(savedConfig.playbackRate),
            actionDelayMs: validNumber(
              savedConfig.actionDelayMs,
              1500,
              300,
              15000,
            ),
            courseReadyTimeoutSeconds: validNumber(
              savedConfig.courseReadyTimeoutSeconds,
              45,
              5,
              600,
            ),
            pollIntervalMs: validNumber(
              savedConfig.pollIntervalMs,
              500,
              500,
              30000,
            ),
            maxPlaybackMinutes: validNumber(
              savedConfig.maxPlaybackMinutes,
              180,
              1,
              1440,
            ),
          });
          availableCourses.value = restoredCourses(saved.availableCourses);
          if (Array.isArray(saved.selectedCourses)) {
            selectedCourses.value = restoredCourses(saved.selectedCourses).map(
              (course) => ({ ...course, state: "pending" }),
            );
          }
          courseSelectionInitialized =
            saved.courseSelectionInitialized === true;
          if (saved.queue && typeof saved.queue === "object") {
            const queue = saved.queue as {
              courses?: QueueCourse[];
              runState?: string;
              config?: OrchestrationConfig;
            };
            if (Array.isArray(queue.courses)) {
              const valid = restoredCourses(queue.courses);
              courses.value = valid.map((course) => {
                const savedCourse = queue.courses!.find(
                  (item) => item && courseIdentity(item) === course.id,
                )!;
                const captureId =
                  typeof savedCourse.captureId === "string"
                    ? savedCourse.captureId
                    : undefined;
                const pending =
                  savedCourse.pendingCapture ??
                  (savedCourse.state === "running" && captureId
                    ? {
                        id: captureId,
                        name: `${courseCaptureName(course).slice(0, 76)}（中断）`,
                        needsStop: true,
                        playbackCompleted: false,
                      }
                    : undefined);
                return {
                  ...course,
                  state:
                    savedCourse.state === "completed" && captureId
                      ? "completed"
                      : savedCourse.state === "ended"
                        ? "ended"
                        : savedCourse.state === "pending"
                          ? "pending"
                          : "error",
                  captureId,
                  playback: restoredPlayback(savedCourse.playback),
                  playbackAttempted:
                    typeof savedCourse.playbackAttempted === "boolean"
                      ? savedCourse.playbackAttempted
                      : !!savedCourse.playback ||
                        savedCourse.state === "running" ||
                        savedCourse.state === "error",
                  ...(typeof savedCourse.failedAt === "string"
                    ? { failedAt: savedCourse.failedAt }
                    : {}),
                  detail:
                    savedCourse.state === "running"
                      ? "应用已重新打开，原播放执行上下文已结束；已保留最近进度，不会自动重播"
                      : typeof savedCourse.detail === "string"
                        ? savedCourse.detail
                        : undefined,
                  ...(pending &&
                  typeof pending.id === "string" &&
                  typeof pending.name === "string"
                    ? {
                        pendingCapture: {
                          id: pending.id,
                          name: pending.name,
                          needsStop: pending.needsStop === true,
                          playbackCompleted:
                            pending.playbackCompleted !== false,
                        },
                      }
                    : {}),
                };
              });
              currentIndex.value = courses.value.findIndex(
                (course) => course.state !== "completed",
              );
              if (currentIndex.value < 0)
                currentIndex.value = courses.value.length;
              runState.value =
                currentIndex.value === courses.value.length
                  ? "completed"
                  : queue.runState === "ended"
                    ? "ended"
                    : "paused";
              runConfig = restoredRunConfig(queue.config, config);
            }
          }
          if (Array.isArray(saved.logs))
            logs.value = (saved.logs as OrchestrationLog[]).slice(-MAX_LOGS);
        }
      } catch {
        statusError.value = "播放编排设置读取失败，已恢复默认设置";
      }
      hydrated = true;
    }

    function setCoursesSelected(targets: Course[], selected: boolean) {
      if (!canEditSelection.value || !courseCatalogReady.value) return;
      const selectedById = new Map(
        selectedCourses.value.map((course) => [course.id, course]),
      );
      for (const course of targets) {
        if (
          selected &&
          availableCourses.value.some(
            (available) => courseIdentity(available) === courseIdentity(course),
          )
        )
          selectedById.set(course.id, { ...course });
        else selectedById.delete(course.id);
      }
      const inCatalog = availableCourses.value.flatMap((course) => {
        const selectedCourse = selectedById.get(course.id);
        if (!selectedCourse) return [];
        selectedById.delete(course.id);
        return [selectedCourse];
      });
      selectedCourses.value = inCatalog;
      courseSelectionInitialized = true;
      currentIndex.value = 0;
      runState.value = "idle";
      persist();
    }

    function setCourseSelected(course: Course, selected: boolean) {
      setCoursesSelected([course], selected);
    }

    function isCourseSelected(id: string) {
      return selectedCourses.value.some((course) => course.id === id);
    }

    function setCourseText(value: string) {
      if (!canEditSelection.value) return;
      const names = value
        .split(/\r?\n/)
        .map((name) => name.trim())
        .filter(Boolean);
      selectedCourses.value = names.map((name) => ({
        id: crypto.randomUUID(),
        name,
        state: "pending",
      }));
      courseSelectionInitialized = true;
      currentIndex.value = 0;
      currentStep.value = undefined;
      runState.value = "idle";
      persist();
    }

    function updateConfig() {
      config.playbackVolumePercent = validNumber(
        config.playbackVolumePercent,
        35,
        0,
        100,
      );
      config.playbackRate = validPlaybackRate(config.playbackRate);
      config.actionDelayMs = validNumber(
        config.actionDelayMs,
        1500,
        300,
        15000,
      );
      config.courseReadyTimeoutSeconds = validNumber(
        config.courseReadyTimeoutSeconds,
        45,
        5,
        600,
      );
      config.pollIntervalMs = validNumber(
        config.pollIntervalMs,
        500,
        500,
        30000,
      );
      config.maxPlaybackMinutes = validNumber(
        config.maxPlaybackMinutes,
        180,
        1,
        1440,
      );
      persist();
    }

    async function refreshStatus() {
      statusLoading.value = true;
      statusError.value = "";
      try {
        status.value = await playerAutomation.status();
      } catch (error) {
        status.value = undefined;
        statusError.value = errorMessage(error);
      } finally {
        statusLoading.value = false;
      }
    }

    function dependencies() {
      return {
        now: () => performance.now(),
        snapshot: playerAutomation.snapshot,
        playbackSnapshot: playerAutomation.playbackSnapshot,
        courseSnapshot: playerAutomation.courseSnapshot,
        openCourse: playerAutomation.openCourse,
        expandFolder: playerAutomation.expandFolder,
        revealCourse: playerAutomation.revealCourse,
        setVolume: playerAutomation.setVolume,
        setFullscreen: playerAutomation.setFullscreen,
        setPlaybackRate: playerAutomation.setPlaybackRate,
        click: async (
          text: string,
          count?: number,
          match?: CourseClickMatch,
        ) => {
          if (controller?.signal.aborted) throw new OrchestrationInterrupted();
          if (pauseRequested && pauseWait) {
            await pauseWait;
            if (controller?.signal.aborted)
              throw new OrchestrationInterrupted();
          }
          // Dialog decisions are not course launches and must not arm the
          // pause-on-end playback toggle during preflight.
          if (count !== 2) return playerAutomation.click(text, count, match);
          // A timed-out reply can still mean the double-click reached SzPlayer.
          // Record the attempt before dispatch; Continue may not replay it.
          const course = activeCourse.value;
          const previousAttempt = course?.playbackAttempted;
          if (course) course.playbackAttempted = true;
          try {
            requirePersistedQueue();
          } catch (error) {
            if (course) course.playbackAttempted = previousAttempt;
            throw error;
          }
          playbackPid = match?.expectedPid;
          const delivered = playerAutomation
            .click(text, count, match)
            .then(() => {
              playbackClickDelivered = true;
            });
          playbackClickInFlight = delivered;
          try {
            await delivered;
          } finally {
            if (playbackClickInFlight === delivered)
              playbackClickInFlight = undefined;
          }
        },
        startCapture: async () => {
          requirePersistedQueue();
          if (trace.busy || trace.captureEnabled)
            throw new Error("已有其他提取操作，当前课程尚未启动");
          const previousId = trace.currentHistoryId;
          try {
            await trace.startCapture((id) => {
              ownedCaptureId = id;
              if (activeCourse.value) activeCourse.value.captureId = id;
              requirePersistedQueue();
            });
          } finally {
            if (
              trace.currentHistoryId &&
              trace.currentHistoryId !== previousId
            ) {
              ownedCaptureId = trace.currentHistoryId;
              if (activeCourse.value)
                activeCourse.value.captureId = ownedCaptureId;
              persist();
            }
          }
          requireOwnedCapture();
        },
        captureOwned: () => !!ownedCaptureId,
        captureActive: () => {
          if (ownedCaptureId) requireOwnedCapture();
          return trace.captureEnabled && trace.connected;
        },
        onPlaybackStarted: () => {
          playbackStartedForCourse = true;
        },
        playbackContinued: () => playbackContinuedForCourse,
        pausePlayback: async () => {
          if (playbackClickDelivered) await pausePlaybackIfNeeded();
          else if (activeCourse.value?.playbackAttempted)
            throw new Error("打开课程的结果不确定，不能安全切换播放状态");
        },
        stopCapture: async () => {
          requireOwnedCapture();
          const entry = await trace.stopCapture({ deferReset: true });
          if (entry && entry.id !== ownedCaptureId)
            throw new Error("停止提取返回了其他记录，不会替换本课记录");
          return (
            entry && {
              id: entry.id,
              name: entry.name,
              valid: entry.valid,
              issues: entry.issues,
              requestCount: entry.traces?.length,
            }
          );
        },
        onCaptureCheckpoint: (checkpoint: CaptureCheckpoint | null) => {
          if (activeCourse.value)
            activeCourse.value.pendingCapture = checkpoint
              ? {
                  id: checkpoint.entry?.id ?? ownedCaptureId ?? "",
                  name: checkpoint.name,
                  needsStop: !checkpoint.entry,
                  playbackCompleted: checkpoint.playbackCompleted,
                }
              : undefined;
          persist();
        },
        saveCapture: async (entry: { id: string }, name: string) => {
          trace.renameHistory(entry.id, name);
          await trace.flushKeyHistory();
          trace.completeCapture(entry.id);
        },
        sleep: createAbortableSleep(
          isMyGo() ? playerAutomation.wait : undefined,
        ),
        manualComplete: () => manualComplete.value,
        checkpoint: async (signal: AbortSignal) => {
          if (signal.aborted) throw new OrchestrationInterrupted();
          if (queuePersistenceError) requirePersistedQueue();
          if (pauseRequested && playbackMayBeActive()) {
            try {
              await pausePlaybackIfNeeded();
            } catch (error) {
              if (!playbackToggleUndelivered(error)) throw error;
              // The player never received a pause. Keep monitoring so the user
              // can retry after the control bar is visible.
              return;
            }
          }
          if (pauseRequested && trace.captureEnabled && !capturePausedForTask) {
            requireOwnedCapture();
            capturePausedForTask = true;
            try {
              await trace.pauseCapture();
            } catch (error) {
              capturePausedForTask = false;
              throw error;
            }
          }
          if (pauseRequested && pauseWait) {
            let rejectAbort!: (reason: OrchestrationInterrupted) => void;
            const aborted = new Promise<never>((_, reject) => {
              rejectAbort = reject;
            });
            const onAbort = () => rejectAbort(new OrchestrationInterrupted());
            signal.addEventListener("abort", onAbort, { once: true });
            try {
              if (signal.aborted) throw new OrchestrationInterrupted();
              await Promise.race([pauseWait, aborted]);
            } finally {
              signal.removeEventListener("abort", onAbort);
            }
          }
          if (signal.aborted) throw new OrchestrationInterrupted();
        },
        recoverPlayback,
        onProgress: (playback: PlaybackProgress) => {
          if (activeCourse.value) activeCourse.value.playback = playback;
        },
        onStep: (step: OrchestrationStep) => setCurrentStep(step),
        onLog: (
          level: "info" | "warn" | "error",
          message: string,
          detail?: string,
          metadata?: ExecutionLogMeta,
        ) => appendLog(level, message, detail, undefined, metadata),
      };
    }

    async function refreshCourses() {
      if (!canEditSelection.value) return;
      hydrate();
      if (!canEditSelection.value) return;
      const previousSelection = selectedCourses.value.slice();
      courseCatalogReady.value = false;
      availableCourses.value = [];
      selectedCourses.value = [];
      courseLoading.value = true;
      courseError.value = "";
      const discoveryController = new AbortController();
      try {
        const detected = await discoverCourses(
          dependencies(),
          { ...config },
          discoveryController.signal,
        );
        const selectedIds = new Set(previousSelection.map(courseIdentity));
        availableCourses.value = detected;
        selectedCourses.value = detected.filter(
          (course) =>
            !courseSelectionInitialized ||
            selectedIds.has(courseIdentity(course)),
        );
        if (detected.length) courseSelectionInitialized = true;
        courseCatalogReady.value = true;
        currentIndex.value = 0;
        runState.value = "idle";
        appendLog(
          "info",
          detected.length
            ? `已从 SzPlayer 识别到 ${detected.length} 个可播放课时`
            : "SzPlayer 当前没有可用课程",
        );
        persist();
      } catch (error) {
        courseError.value = errorMessage(error);
        appendLog("error", "读取课程失败", courseError.value);
        persist();
        throw error;
      } finally {
        currentStep.value = undefined;
        courseLoading.value = false;
      }
    }

    async function checkRunRequirements() {
      const playerStatus = await playerAutomation.status();
      status.value = playerStatus;
      if (!playerStatus.installed) throw new Error("未找到 SzPlayer");
      if (!playerStatus.verifiedProfile)
        throw new Error(
          `SzPlayer ${playerStatus.version || "未知版本"} 尚未完成静态适配验证，已停止执行`,
        );
      if (!playerStatus.accessibilityReady)
        throw new Error("请先在系统设置中授予辅助功能权限");
      if (!playerStatus.running)
        throw new Error("SzPlayer 尚未打开，请先手动打开播放器");
      if (!trace.historyReady) throw new Error("密钥历史尚未就绪，请稍后重试");
      if (!trace.connected) throw new Error("接收服务尚未连接，请先恢复连接");
      if (trace.captureEnabled)
        throw new Error("已有提取任务正在运行，请先停止");
    }

    async function run() {
      hydrate();
      if (!canStart.value || transitioning.value) return;
      const continuing = runState.value === "paused";
      if (
        continuing &&
        activeCourse.value?.playbackAttempted &&
        !activeCourse.value.pendingCapture
      )
        throw new Error(
          "原执行上下文已结束，无法安全续播；已保留当前课程与记录，不会重新打开视频。请先结束此任务后再新建任务。",
        );
      startingRun.value = true;
      const newQueue = !continuing;
      const firstNeedsPlayback =
        !continuing || !activeCourse.value?.pendingCapture;
      const plannedCourses = restoredCourses(selectedCourses.value).map(
        (course) => ({ ...course, state: "pending" as const }),
      );
      const plannedConfig = { ...config };
      appendLog(
        "info",
        continuing ? "正在检查继续执行条件" : "正在检查任务启动条件",
      );
      try {
        if (firstNeedsPlayback) await checkRunRequirements();
        else if (
          !trace.historyReady ||
          (trace.captureEnabled &&
            activeCourse.value?.pendingCapture?.needsStop &&
            !archivedCapture(activeCourse.value?.pendingCapture?.id ?? "") &&
            !storedPartialCapture(activeCourse.value?.pendingCapture) &&
            !(
              activeCourse.value?.pendingCapture?.needsStop &&
              activeCourse.value.pendingCapture.id === trace.currentHistoryId
            ))
        )
          throw new Error(
            "密钥历史尚未就绪或已有其他提取任务，暂时无法重试保存",
          );
        controller = new AbortController();
        unblockPause();
        appendLog("info", "运行条件检查通过");
      } catch (error) {
        appendLog("error", "任务未能启动", errorMessage(error));
        throw error;
      } finally {
        startingRun.value = false;
      }
      if (newQueue) {
        courses.value = plannedCourses;
        runConfig = plannedConfig;
        currentIndex.value = 0;
      }
      runFinished = new Promise<void>((resolve) => {
        finishRun = resolve;
      });
      stopRequested = false;
      endNeedsAttention = false;
      capturePausedForTask = false;
      playbackPausedForTask = false;
      playbackClickDelivered = false;
      playbackClickInFlight = undefined;
      runState.value = "running";
      statusError.value = "";
      appendLog(
        "info",
        `开始执行课程队列，共 ${courses.value.length} 门`,
        `音量 ${(runConfig ?? config).playbackVolumePercent}% · 倍速 ${(runConfig ?? config).playbackRate ?? "保持当前"} · 检查间隔 ${(runConfig ?? config).pollIntervalMs}ms`,
      );
      notify(
        continuing
          ? desktopNotifications.playerResumed(
              processedCount.value,
              courses.value.length,
            )
          : desktopNotifications.playerStarted(courses.value.length),
      );

      const precheckedIndex = currentIndex.value;
      while (
        currentIndex.value < courses.value.length &&
        !controller.signal.aborted &&
        !stopRequested
      ) {
        const course = courses.value[currentIndex.value]!;
        if (course.state === "completed") {
          currentIndex.value++;
          continue;
        }
        course.state = "running";
        course.playbackAttempted ??= false;
        course.detail = undefined;
        if (!course.pendingCapture) course.playback = undefined;
        course.failedAt = undefined;
        manualComplete.value = false;
        playbackClickDelivered = false;
        appendLog(
          "info",
          `开始处理第 ${currentIndex.value + 1} / ${courses.value.length} 节课程`,
          `课程：${course.name}
音量：${(runConfig ?? config).playbackVolumePercent}%
倍速：${(runConfig ?? config).playbackRate ?? "保持当前"}`,
        );
        persist();
        const courseStartedAt = performance.now();
        automaticRecoveryAttempts = 0;
        recoveryRequest = undefined;
        ownedCaptureId = undefined;
        playbackPid = undefined;
        playbackStartedForCourse = false;
        playbackContinuedForCourse = false;
        playbackPauseFailure = undefined;
        try {
          let entry: { id: string };
          if (course.pendingCapture) {
            setCurrentStep("savingCapture");
            let pending = course.pendingCapture;
            if (
              pending.needsStop &&
              (archivedCapture(pending.id) || storedPartialCapture(pending))
            ) {
              pending = { ...pending, needsStop: false };
              course.pendingCapture = pending;
              persist();
            }
            if (pending.needsStop) {
              setCurrentStep("stoppingCapture");
              if (!pending.id || trace.currentHistoryId !== pending.id)
                throw new Error(
                  "待收尾的提取记录与当前记录不一致，请先在密钥管理确认；不会重新播放课程",
                );
              const stopped = await trace.stopCapture({ deferReset: true });
              if (!stopped || stopped.id !== pending.id)
                throw new Error("没有取得当前课程的提取记录，已保留待收尾状态");
              pending = { ...pending, id: stopped.id, needsStop: false };
              course.pendingCapture = pending;
              persist();
            }
            setCurrentStep("savingCapture");
            appendLog(
              "info",
              pending.playbackCompleted === false
                ? "重试保存中断记录，不重新播放"
                : "重试保存已完成课程，不重新播放",
              undefined,
              course.name,
            );
            const savingAt = performance.now();
            await dependencies().saveCapture(pending, pending.name);
            const saved = trace.keyHistory.find(
              (item) => item.id === pending.id,
            );
            appendLog(
              saved?.valid === false ? "warn" : "info",
              "提取记录已保存",
              captureResultDetail({
                name: pending.name,
                valid: saved?.valid,
                issues: saved?.issues,
                requestCount: saved?.traces?.length,
              }),
              course.name,
              { elapsedMs: performance.now() - savingAt },
            );
            if (pending.playbackCompleted === false) {
              course.pendingCapture = undefined;
              course.captureId = pending.id;
              course.state = "ended";
              course.detail = "中断记录已保存，当前课程未播放完成";
              runState.value = "ended";
              currentStep.value = undefined;
              appendLog("warn", course.detail);
              controller = undefined;
              unblockPause();
              persist();
              finishRun?.();
              return;
            }
            entry = pending;
          } else {
            if (!firstNeedsPlayback || currentIndex.value !== precheckedIndex)
              await checkRunRequirements();
            if (controller.signal.aborted) throw new OrchestrationInterrupted();
            entry = await executeCourse(
              {
                ...dependencies(),
                onLog: (level, message, detail, metadata) =>
                  appendLog(level, message, detail, course.name, metadata),
              },
              { ...(runConfig ?? config) },
              course,
              controller.signal,
            );
          }
          if (playbackPausePending) await playbackPausePending;
          // Pause flags belong to the media/capture just finalized. A manual
          // completion must not suppress pausing the next course.
          playbackPausedForTask = false;
          capturePausedForTask = false;
          course.pendingCapture = undefined;
          course.state = "completed";
          course.captureId = entry.id;
          appendLog(
            "info",
            "本课程执行完成",
            currentIndex.value + 1 < courses.value.length
              ? `下一课：${courses.value[currentIndex.value + 1]!.name}`
              : "队列中的全部课程已处理完成",
            course.name,
            { elapsedMs: performance.now() - courseStartedAt },
          );
          currentIndex.value++;
          currentStep.value = undefined;
          persist();
          notify(
            desktopNotifications.playerCourseSaved(
              currentIndex.value,
              courses.value.length,
            ),
          );
        } catch (error) {
          if (error instanceof CaptureSaveFailed)
            course.pendingCapture = {
              id: error.entry.id,
              name: error.captureName,
            };
          const interrupted = error instanceof OrchestrationInterrupted;
          if (
            stopRequested &&
            (!interrupted ||
              (error instanceof OrchestrationInterrupted &&
                error.cleanupFailed))
          )
            endNeedsAttention = true;
          course.state = interrupted
            ? stopRequested
              ? "ended"
              : "pending"
            : "error";
          course.detail = errorMessage(error);
          course.failedAt = new Date().toISOString();
          runState.value = stopRequested ? "ended" : "paused";
          appendLog(
            interrupted ? "warn" : "error",
            interrupted
              ? stopRequested
                ? "任务已结束"
                : "队列已暂停"
              : "课程执行失败，队列已停止推进",
            [
              course.detail,
              course.playback
                ? `最近进度：${formatPlaybackProgress(course.playback)}`
                : undefined,
              course.pendingCapture
                ? course.pendingCapture.playbackCompleted === false
                  ? "课程已中断，记录尚未保存成功，可重试保存"
                  : "播放已完成，记录尚未保存成功，可重试保存"
                : undefined,
            ]
              .filter(Boolean)
              .join("\n"),
            course.name,
          );
          currentStep.value = undefined;
          persist();
          controller = undefined;
          unblockPause();
          finishRun?.();
          if (!interrupted && !stopRequested)
            notify(
              desktopNotifications.playerAttention(
                processedCount.value,
                courses.value.length,
              ),
            );
          return;
        }
      }
      if (currentIndex.value >= courses.value.length) {
        runState.value = "completed";
        currentStep.value = undefined;
        appendLog(
          "info",
          "任务已完成，全部课程已播放并保存",
          undefined,
          undefined,
        );
        persist();
        notify(desktopNotifications.playerCompleted(courses.value.length));
      } else {
        runState.value = "paused";
      }
      persist();
      controller = undefined;
      finishRun?.();
    }

    function unblockPause() {
      pauseRequested = false;
      releasePause?.();
      releasePause = undefined;
      pauseWait = undefined;
    }

    function playbackMayBeActive() {
      return (
        currentStep.value === "startingPlayback" ||
        currentStep.value === "configuringPlayback" ||
        currentStep.value === "waitingCompletion"
      );
    }

    async function pausePlaybackIfNeeded() {
      if (playbackPausedForTask) return;
      if (playbackPausePending) return playbackPausePending;
      if (playbackPauseFailure) throw playbackPauseFailure;
      playbackPausePending = (async () => {
        // The last monitor sample may precede EOF. Re-read before a toggle so
        // pausing at the boundary cannot restart a just-completed video.
        let snapshot;
        try {
          snapshot = await playerAutomation.playbackSnapshot();
        } catch (error) {
          if (playbackPid) throw error;
          /* The bounded native pause may still locate the control. */
        }
        if (snapshot) {
          if (!snapshot.running)
            throw new Error("SzPlayer 已退出，未发送暂停操作");
          if (
            (playbackPid ?? recoveryRequest?.expectedPid) &&
            snapshot.pid !== (playbackPid ?? recoveryRequest?.expectedPid)
          )
            throw new Error("SzPlayer 进程已变化，未发送暂停操作");
          const fresh = readPlaybackProgress(snapshot);
          const previous = activeCourse.value?.playback;
          if (
            playbackStartedForCourse &&
            !snapshot.readIssue &&
            previous?.totalSeconds &&
            fresh.totalSeconds &&
            previous.totalSeconds !== fresh.totalSeconds
          )
            throw new Error("视频总时长已变化，未切换其他视频的播放状态");
          if (!snapshot.readIssue && fresh.seconds !== undefined) {
            if (activeCourse.value) activeCourse.value.playback = fresh;
            if (isFinalFrameProgress(fresh)) return;
          }
          recoveryRequest ??= {
            message: "暂停后继续",
            progress: previous ?? fresh,
            expectedPid: playbackPid ?? snapshot.pid,
            sampleAt: snapshot.at || undefined,
          };
        }
        try {
          await playerAutomation.togglePlayback();
        } catch (error) {
          if (!playbackToggleUndelivered(error)) throw error;
          if (snapshot?.windowMode !== "fullscreen") throw error;
          await playerAutomation.setFullscreen(false);
          await playerAutomation.togglePlayback();
        }
        playbackPausedForTask = true;
        appendLog("info", "播放器暂停操作已确认");
      })();
      try {
        await playbackPausePending;
      } catch (error) {
        if (!playbackToggleUndelivered(error))
          playbackPauseFailure = new Error(errorMessage(error));
        throw error;
      } finally {
        playbackPausePending = undefined;
      }
    }

    async function pause() {
      if (runState.value !== "running" || transitioning.value) return;
      transitioning.value = true;
      pauseRequested = true;
      appendLog("info", "收到暂停请求，正在暂停播放与提取");
      pauseWait = new Promise<void>((resolve) => {
        releasePause = resolve;
      });
      const playbackActive = playbackMayBeActive();
      try {
        if (playbackActive) {
          try {
            await pausePlaybackIfNeeded();
          } catch (error) {
            if (playbackPausedForTask) {
              /* A concurrent checkpoint delivered the pause. */
            } else if (playbackToggleUndelivered(error)) {
              unblockPause();
              appendLog(
                "error",
                "暂停未完成，任务继续运行",
                errorMessage(error),
              );
              throw error;
            } else {
              throw error;
            }
          }
        }
        if (trace.captureEnabled) {
          requireOwnedCapture();
          capturePausedForTask = true;
          await trace.pauseCapture();
        }
        if (playbackActive) {
          try {
            const snapshot = await playerAutomation.playbackSnapshot();
            const observed = readPlaybackProgress(snapshot);
            const progress =
              !snapshot.readIssue && observed.seconds !== undefined
                ? observed
                : (activeCourse.value?.playback ?? {});
            if (activeCourse.value) activeCourse.value.playback = progress;
            recoveryRequest ??= {
              message: "用户暂停后继续",
              progress,
              expectedPid: snapshot.pid,
            };
          } catch {
            // Retain the last displayed time if the player hides its control bar.
          }
        }
        if (!controller) {
          unblockPause();
          return;
        }
        runState.value = "paused";
        appendLog("info", "任务已暂停");
        notify(
          desktopNotifications.playerPaused(
            processedCount.value,
            courses.value.length,
          ),
        );
      } catch (error) {
        // A failed native operation may already have taken effect. Never
        // compensate with another toggle or let the queue continue blindly.
        if (!controller) {
          unblockPause();
          throw error;
        }
        if (playbackToggleUndelivered(error) && !playbackPausedForTask) {
          throw error;
        }
        stopRequested = true;
        runState.value = "ended";
        controller?.abort();
        unblockPause();
        appendLog("error", "暂停失败，任务已停止", errorMessage(error));
        throw error;
      } finally {
        transitioning.value = false;
      }
    }

    async function recoverPlayback(
      request: PlaybackRecoveryRequest,
      signal: AbortSignal,
    ) {
      // A user pause wins over automatic recovery. Wait for their continuation.
      if (pauseRequested) {
        await dependencies().checkpoint(signal);
        return;
      }
      recoveryRequest = request;
      appendLog("warn", "播放监控超时，正在暂停视频与提取", request.message);
      await pause();
      if (signal.aborted) throw new OrchestrationInterrupted();
      if (activeCourse.value) {
        activeCourse.value.detail = request.message;
        activeCourse.value.failedAt = new Date().toISOString();
      }
      recovering.value = true;
      try {
        while (automaticRecoveryAttempts < 3) {
          const attempt = ++automaticRecoveryAttempts;
          appendLog(
            "info",
            `尝试自动恢复 ${attempt} / 3`,
            "保留当前视频位置和提取记录",
          );
          await dependencies().sleep([1000, 3000, 5000][attempt - 1]!, signal);
          if (signal.aborted) throw new OrchestrationInterrupted();
          try {
            await resume(true);
            if (runState.value === "running") return;
          } catch (error) {
            if (signal.aborted || runState.value !== "paused") throw error;
            appendLog(
              "warn",
              `自动恢复第 ${attempt} 次未成功`,
              errorMessage(error),
            );
          }
        }
        appendLog(
          "error",
          "自动恢复次数已用尽，任务保持暂停",
          "当前课程和提取记录已保留；处理问题后点击继续执行，从当前位置继续。",
        );
        notify(
          desktopNotifications.playerAttention(
            processedCount.value,
            courses.value.length,
          ),
        );
      } finally {
        recovering.value = false;
      }
      // Do not unwind executeCourse: that would archive/reset the live capture
      // and turn the next Continue into another double-click from the beginning.
      await dependencies().checkpoint(signal);
    }

    async function resume(automatic = false) {
      if (
        runState.value !== "paused" ||
        transitioning.value ||
        (recovering.value && !automatic)
      )
        return;
      if (!controller) {
        await run();
        return;
      }
      transitioning.value = true;
      appendLog("info", "收到继续请求，正在恢复播放与提取");
      let resumingStarted = false;
      try {
        if (capturePausedForTask) requireOwnedCapture();
        if (recoveryRequest) {
          const currentStatus = await playerAutomation.status();
          status.value = currentStatus;
          if (
            !currentStatus.running ||
            !currentStatus.verifiedProfile ||
            !currentStatus.accessibilityReady
          )
            throw new Error(
              "播放器运行状态或辅助功能权限尚未恢复，任务保持暂停",
            );
          if (!trace.connected)
            throw new Error("提取接收服务尚未连接，任务保持暂停");
          const playback = validatePlaybackRecovery(
            await playerAutomation.playbackSnapshot(),
            recoveryRequest,
          );
          if (activeCourse.value) activeCourse.value.playback = playback;
          await playerAutomation.setVolume(
            (runConfig ?? config).playbackVolumePercent,
          );
        }
        if (controller.signal.aborted) throw new OrchestrationInterrupted();
        resumingStarted = true;
        if (playbackPausePending) await playbackPausePending;
        if (capturePausedForTask) {
          await trace.resumeCapture();
          capturePausedForTask = false;
        }
        if (playbackPausedForTask) {
          if (
            !activeCourse.value?.playback ||
            !isFinalFrameProgress(activeCourse.value.playback)
          )
            try {
              await playerAutomation.togglePlayback();
            } catch (error) {
              playbackPausedForTask = false;
              playbackPauseFailure = new Error(
                `继续播放结果不确定：${errorMessage(error)}`,
              );
              throw error;
            }
          playbackPausedForTask = false;
        }
        if (!controller) {
          unblockPause();
          return;
        }
        runState.value = "running";
        playbackContinuedForCourse ||= playbackClickDelivered;
        recoveryRequest = undefined;
        if (activeCourse.value) {
          activeCourse.value.detail = undefined;
          activeCourse.value.failedAt = undefined;
        }
        unblockPause();
        appendLog("info", "任务已继续", "沿用当前视频位置与提取记录");
        notify(
          desktopNotifications.playerResumed(
            processedCount.value,
            courses.value.length,
          ),
        );
      } catch (error) {
        if (!resumingStarted && controller && !controller.signal.aborted) {
          appendLog(
            "warn",
            "续播条件未满足，任务保持暂停",
            errorMessage(error),
          );
          throw error;
        }
        if (!controller) {
          unblockPause();
          throw error;
        }
        stopRequested = true;
        runState.value = "ended";
        controller?.abort();
        unblockPause();
        appendLog("error", "继续失败，任务已停止", errorMessage(error));
        throw error;
      } finally {
        transitioning.value = false;
      }
    }

    async function endTask() {
      if (
        (runState.value !== "running" && runState.value !== "paused") ||
        transitioning.value
      )
        return;
      transitioning.value = true;
      stopRequested = true;
      appendLog("info", "收到结束请求，正在停止并保存已提取内容");
      endNeedsAttention = false;
      const shouldPause =
        playbackMayBeActive() ||
        (currentStep.value === "selectingCourse" &&
          (playbackClickDelivered || !!playbackClickInFlight));
      const pendingPause = playbackPausePending;
      controller?.abort();
      unblockPause();
      try {
        if (playbackClickInFlight) {
          try {
            await playbackClickInFlight;
          } catch {
            // executeCourse reports a failed click and stops its capture.
          }
        }
        if (pendingPause) {
          try {
            await pendingPause;
          } catch (error) {
            endNeedsAttention = true;
            appendLog("warn", "结束任务时未能暂停播放器", errorMessage(error));
          }
        } else if (
          shouldPause &&
          playbackClickDelivered &&
          !playbackPausedForTask
        ) {
          try {
            await pausePlaybackIfNeeded();
          } catch (error) {
            endNeedsAttention = true;
            appendLog("warn", "结束任务时未能暂停播放器", errorMessage(error));
          }
        }
        controller?.abort();
        unblockPause();
        await runFinished;
        if (currentIndex.value >= courses.value.length) return;
        const pending = activeCourse.value?.pendingCapture;
        if (
          pending &&
          trace.historyReady &&
          !trace.historyError &&
          trace.currentHistoryId !== pending.id
        ) {
          await trace.flushKeyHistory();
          if (!trace.keyHistory.some((entry) => entry.id === pending.id)) {
            const course = activeCourse.value!;
            course.pendingCapture = undefined;
            course.state = "ended";
            course.detail = "本课提取记录已不存在，任务已结束；未保存新的记录";
            course.failedAt = new Date().toISOString();
            runState.value = "ended";
            appendLog("error", course.detail);
            notify(
              desktopNotifications.playerAttention(
                processedCount.value,
                courses.value.length,
                true,
              ),
            );
            return;
          }
        }
        if (activeCourse.value?.pendingCapture) {
          runState.value = "paused";
          appendLog(
            "warn",
            "任务已停止推进，仍有记录等待保存，请重试保存后再新建任务",
          );
          notify(
            desktopNotifications.playerAttention(
              processedCount.value,
              courses.value.length,
              true,
            ),
          );
          persist();
          return;
        }
        runState.value = "ended";
        notify(
          endNeedsAttention
            ? desktopNotifications.playerAttention(
                processedCount.value,
                courses.value.length,
                true,
              )
            : desktopNotifications.playerEnded(
                processedCount.value,
                courses.value.length,
              ),
        );
      } finally {
        transitioning.value = false;
        persist();
      }
    }

    async function prepareQuit() {
      hydrate();
      if (operationBusy.value)
        throw new Error("任务正在切换状态，请稍后再退出");
      await endTask();
      if (courses.value.some((course) => !!course.pendingCapture))
        throw new Error("仍有记录等待保存，请重试保存后退出");
      if (endNeedsAttention)
        throw new Error("播放器暂停或任务收尾未能确认，请检查执行日志");
      if (!persist()) throw new Error("任务状态保存失败，请处理后再退出");
    }

    function markPlaybackComplete() {
      if (
        runState.value !== "running" ||
        currentStep.value !== "waitingCompletion"
      )
        return;
      manualComplete.value = true;
      appendLog("warn", "已手动确认当前课程播放完成");
    }

    function resetQueue() {
      if (
        controller ||
        operationBusy.value ||
        courseLoading.value ||
        courses.value.some((course) => !!course.pendingCapture) ||
        runState.value === "running"
      )
        return;
      courses.value = [];
      runConfig = undefined;
      currentIndex.value = 0;
      currentStep.value = undefined;
      runState.value = "idle";
      persist();
    }

    function clearLogs() {
      logs.value = [];
      persist();
    }

    return {
      config,
      availableCourses,
      selectedCourses,
      courses,
      operationBusy,
      canEditSelection,
      logs,
      runState,
      currentIndex,
      currentStep,
      status,
      statusLoading,
      statusError,
      courseLoading,
      courseCatalogReady,
      canContinueSelection,
      courseError,
      recovering,
      activeCourse,
      processedCount,
      progress,
      captureActive,
      receiverConnected,
      historyReady,
      canStart,
      hydrate,
      setCourseText,
      setCoursesSelected,
      setCourseSelected,
      isCourseSelected,
      updateConfig,
      refreshStatus,
      refreshCourses,
      run,
      pause,
      resume,
      endTask,
      prepareQuit,
      markPlaybackComplete,
      resetQueue,
      clearLogs,
    };
  },
);
