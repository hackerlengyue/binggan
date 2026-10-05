import {
  progressDetail,
  captureResultDetail,
  elapsedText,
  type ExecutionLogMeta,
} from "./orchestration-log";
import type { PlayerSnapshot } from "./player-automation";

export type Course = {
  id: string;
  name: string;
  targetText?: string;
  sourcePath?: string;
};
// AX identifiers such as _NS:26 are controls, not file identities. Keep
// filesystem-backed selections stable even when two roots share a label.
export function courseIdentity(course: Course) {
  return typeof course.sourcePath === "string" &&
    course.sourcePath.startsWith("/") &&
    /\.sz$/i.test(course.sourcePath)
    ? `file:${course.sourcePath.normalize("NFC")}`
    : course.id;
}

function courseLabel(value: string | undefined) {
  // The player may append a duration and use a middle dot as a separator.
  // Do not strip arbitrary punctuation or accept prefixes such as “补充”.
  return (value ?? "")
    .normalize("NFC")
    .trim()
    .replace(/\s+\d+(?:\.\d+)?\s*分钟$/, "")
    .replace(/\.sz$/i, "")
    .replace(/[\s·]+/gu, "");
}

function courseTargetText(course: Course) {
  // Catalog names omit the extension; the source filename retains it, even
  // for filenames whose actual stem also ends in .sz.
  return course.sourcePath?.startsWith("/") && /\.sz$/i.test(course.sourcePath)
    ? course.sourcePath.split("/").at(-1)!
    : (course.targetText ?? course.name.split(" / ").at(-1)!);
}

export type PlayerPage =
  "stopped" | "login" | "courses" | "playback" | "unknown";

export type OrchestrationConfig = {
  playbackVolumePercent: number;
  playbackRate: number | null;
  actionDelayMs: number;
  courseReadyTimeoutSeconds: number;
  pollIntervalMs: number;
  maxPlaybackMinutes: number;
};

export type OrchestrationStep =
  | "checkingPlayer"
  | "waitingCourse"
  | "startingCapture"
  | "selectingCourse"
  | "startingPlayback"
  | "configuringPlayback"
  | "waitingCompletion"
  | "stoppingCapture"
  | "savingCapture";

export type CaptureEntry = {
  id: string;
  name: string;
  valid?: boolean;
  issues?: string[];
  requestCount?: number;
};
export type CourseClickMatch = {
  ordinal: number;
  count: number;
  path: string[];
  expectedPid?: number;
};
export type PlaybackProgress = {
  seconds?: number;
  totalSeconds?: number;
  percent?: number;
};

export function courseCaptureName(course: Course) {
  const filename = /(?:^|[\\/])([^\\/]+)\.sz$/i.exec(
    course.sourcePath ?? "",
  )?.[1];
  return (
    filename ||
    course.targetText ||
    course.name.split(" / ").at(-1) ||
    course.name
  )
    .trim()
    .slice(0, 80);
}

export type CaptureCheckpoint = {
  name: string;
  playbackCompleted: boolean;
  entry?: CaptureEntry;
};

export type PlaybackRecoveryRequest = {
  message: string;
  progress: PlaybackProgress;
  expectedPid?: number;
  sampleAt?: string;
};

export type OrchestrationDependencies = {
  now?(): number;
  snapshot(): Promise<PlayerSnapshot>;
  playbackSnapshot(): Promise<PlayerSnapshot>;
  courseSnapshot(): Promise<PlayerSnapshot>;
  openCourse(path: string): Promise<void>;
  expandFolder(name: string): Promise<void>;
  revealCourse(path: string[], expectedPid?: number): Promise<PlayerSnapshot>;
  setVolume(percent: number): Promise<void>;
  setFullscreen(enabled: boolean): Promise<void>;
  setPlaybackRate(rate: number): Promise<void>;
  click(
    text: string,
    clickCount?: number,
    match?: CourseClickMatch,
  ): Promise<void>;
  startCapture(): Promise<void>;
  captureActive(): boolean;
  captureOwned?(): boolean;
  pausePlayback?(): Promise<void>;
  onPlaybackStarted?(): void;
  playbackContinued?(): boolean;
  stopCapture(): Promise<CaptureEntry | null>;
  saveCapture(entry: CaptureEntry, name: string): Promise<void>;
  onCaptureCheckpoint?(checkpoint: CaptureCheckpoint | null): void;
  sleep(milliseconds: number, signal: AbortSignal): Promise<void>;
  manualComplete(): boolean;
  checkpoint?(signal: AbortSignal): Promise<void>;
  recoverPlayback?(
    request: PlaybackRecoveryRequest,
    signal: AbortSignal,
  ): Promise<void>;
  onProgress?(progress: PlaybackProgress): void;
  onStep(step: OrchestrationStep): void;
  onLog(
    level: "info" | "warn" | "error",
    message: string,
    detail?: string,
    metadata?: ExecutionLogMeta,
  ): void;
};

export class CaptureFinalizeFailed extends Error {
  constructor(cause: unknown) {
    super(
      `播放已完成，提取收尾失败：${cause instanceof Error ? cause.message : String(cause)}`,
    );
    this.name = "CaptureFinalizeFailed";
  }
}

export class CaptureSaveFailed extends Error {
  constructor(
    readonly entry: CaptureEntry,
    readonly captureName: string,
    cause: unknown,
  ) {
    super(
      `播放已完成，密钥记录保存失败：${cause instanceof Error ? cause.message : String(cause)}`,
    );
    this.name = "CaptureSaveFailed";
  }
}

export class OrchestrationInterrupted extends Error {
  cleanupFailed = false;

  constructor() {
    super("播放编排已暂停");
    this.name = "OrchestrationInterrupted";
  }
}

function abortError(signal: AbortSignal) {
  if (signal.aborted) throw new OrchestrationInterrupted();
}

async function checkpoint(
  dependencies: OrchestrationDependencies,
  signal: AbortSignal,
) {
  abortError(signal);
  await dependencies.checkpoint?.(signal);
  abortError(signal);
}

function normalized(value: string | undefined) {
  return (value ?? "")
    .normalize("NFKC")
    .toLocaleLowerCase()
    .replace(/[\s\p{P}\p{S}]+/gu, "");
}

function playbackSliders(snapshot: PlayerSnapshot) {
  const sliders = snapshot.elements.filter(
    (element) => element.role === "AXSlider",
  );
  const verified = sliders.filter((element) => element.identifier === "_NS:26");
  return verified.length
    ? verified
    : sliders.filter((element) => (element.width ?? 0) >= 200);
}

function progressValues(snapshot: PlayerSnapshot) {
  // Reject malformed and out-of-range values instead of clamping them to 100.
  // Multiple disagreeing sliders are ambiguous, never proof of completion.
  const values = playbackSliders(snapshot).map((element) => {
    const raw = element.value?.trim();
    if (!raw || !/^\d+(?:\.\d+)?$/.test(raw)) return NaN;
    const value = Number(raw);
    return value >= 0 && value <= 100 ? value : NaN;
  });
  if (values.some((value) => !Number.isFinite(value))) return [];
  const unique = [...new Set(values)];
  return unique.length === 1 ? unique : [];
}

export function readPlaybackProgress(
  snapshot: PlayerSnapshot,
): PlaybackProgress {
  const labels = snapshot.elements.filter(
    (element) => element.role === "AXStaticText",
  );
  const verified = labels.filter((element) => element.identifier === "_NS:8");
  const clocks = (verified.length ? verified : labels).flatMap((element) =>
    [element.value, element.title].flatMap((value) => {
      const match =
        /^(\d{2,6}):(\d{2}):(\d{2})\s*\/\s*(\d{2,6}):(\d{2}):(\d{2})$/.exec(
          value?.trim() ?? "",
        );
      if (!match) return [];
      const parts = match.slice(1).map(Number);
      if ([1, 2, 4, 5].some((index) => parts[index]! >= 60)) return [];
      const seconds = parts[0]! * 3600 + parts[1]! * 60 + parts[2]!;
      const totalSeconds = parts[3]! * 3600 + parts[4]! * 60 + parts[5]!;
      return seconds <= totalSeconds ? [{ seconds, totalSeconds }] : [];
    }),
  );
  const unique = [
    ...new Map(
      clocks.map((clock) => [`${clock.seconds}/${clock.totalSeconds}`, clock]),
    ).values(),
  ];
  const clock = unique.length === 1 ? unique[0] : undefined;
  const percent =
    progressValues(snapshot)[0] ??
    (clock?.totalSeconds
      ? (clock.seconds / clock.totalSeconds) * 100
      : undefined);
  return {
    ...clock,
    ...(percent !== undefined ? { percent } : {}),
  };
}

function exactButtonText(snapshot: PlayerSnapshot, text: string) {
  const needle = normalized(text);
  return snapshot.elements.some(
    (element) =>
      normalized(element.role).includes("button") &&
      (normalized(element.title) === needle ||
        normalized(element.value) === needle),
  );
}

export function detectPlayerPage(snapshot: PlayerSnapshot): PlayerPage {
  if (!snapshot.running) return "stopped";
  if (
    ["登陆", "登录"].some((label) => exactButtonText(snapshot, label)) ||
    snapshot.elements.some(
      (element) =>
        normalized(element.role).includes("textfield") &&
        ["输入用户名", "用户名", "输入密码", "密码"].includes(
          normalized(element.placeholder),
        ),
    )
  )
    return "login";
  if (
    snapshot.elements.some(
      (element) => normalized(element.role) === "axoutline",
    )
  )
    return "courses";
  if (
    progressValues(snapshot).length > 0 ||
    snapshot.elements.some((element) =>
      /\b\d{2,}:\d{2}:\d{2}\/\d{2,}:\d{2}:\d{2}\b/.test(
        `${element.title ?? ""} ${element.value ?? ""}`,
      ),
    )
  )
    return "playback";
  return "unknown";
}

const rowMetadata = [
  /^进度[:：]?\s*\d+%$/,
  /^\d+\s*(分钟|节课|人学习|天到期)$/,
  /^\d+(\.\d+)?\s*(KB|MB|GB|TB)$/i,
  /^未知$/,
  /^\d{2,}:\d{2}:\d{2}$/,
];

function isCourseRowText(value: string) {
  const text = value.trim();
  return !!text && !rowMetadata.some((pattern) => pattern.test(text));
}

type VisibleCourseRow = {
  ordinal: number;
  labels: string[];
  parentPath: string;
  course?: Course;
};

function visibleCourseRows(snapshot: PlayerSnapshot): VisibleCourseRow[] {
  const rows = snapshot.elements
    .map((element, index) => ({ element, index }))
    .filter(({ element }) => normalized(element.role) === "axrow");
  const parents: string[] = [];
  const visible: VisibleCourseRow[] = [];
  for (let rowIndex = 0; rowIndex < rows.length; rowIndex++) {
    const { element: row, index: start } = rows[rowIndex]!;
    const rowDepth = row.depth ?? 0;
    let end = rows[rowIndex + 1]?.index ?? snapshot.elements.length;
    for (let index = start + 1; index < end; index++) {
      if ((snapshot.elements[index]?.depth ?? rowDepth + 1) <= rowDepth) {
        end = index;
        break;
      }
    }
    const childLabels = snapshot.elements
      .slice(start + 1, end)
      .filter(
        (element) =>
          (element.depth ?? rowDepth + 1) > rowDepth &&
          normalized(element.role).includes("statictext"),
      )
      .flatMap((element) => [element.title, element.value])
      .filter((value): value is string => !!value?.trim());
    const labels = [row.value, row.title, ...childLabels].filter(
      (value): value is string =>
        !!value?.trim() && normalized(value) !== "levelfoldicon",
    );
    const texts = labels.filter(isCourseRowText);
    const rowLabel = [row.value, row.title].find(
      (value) =>
        !!value?.trim() &&
        normalized(value) !== "levelfoldicon" &&
        isCourseRowText(value),
    );
    const rowName = (
      rowLabel ?? [...new Set(texts)].sort((a, b) => b.length - a.length)[0]
    )?.trim();
    const level = Math.max(0, row.level ?? 0);
    if (rowName) parents.length = level;
    const parentPath = parents.slice(0, level).filter(Boolean).join(" / ");
    let course: Course | undefined;
    if (rowName && row.canExpand) {
      parents[level] = rowName;
    } else if (rowName) {
      const name = [parentPath, rowName].filter(Boolean).join(" / ");
      course = {
        id: `${level}:${name}`,
        name,
        targetText: rowName,
        ...(row.identifier ? { sourcePath: row.identifier } : {}),
      };
      course.id = courseIdentity(course);
    }
    visible.push({
      ordinal: rowIndex,
      labels,
      parentPath,
      ...(course ? { course } : {}),
    });
  }
  return visible;
}

export function coursesFromSnapshot(snapshot: PlayerSnapshot): Course[] {
  const courses = visibleCourseRows(snapshot)
    .map((row) => row.course)
    .filter((course): course is Course => !!course);
  return courses.filter(
    (course, index) =>
      courses.findIndex((candidate) => candidate.id === course.id) === index,
  );
}

function findCourseTarget(snapshot: PlayerSnapshot, course: Course) {
  const needle = courseLabel(courseTargetText(course));
  if (!needle) return;
  const visibleRows = visibleCourseRows(snapshot);
  const rows = visibleRows.filter((row) => row.course);
  const candidates = rows.flatMap((row) =>
    row.labels
      .filter((text) => courseLabel(text) === needle)
      .map((text) => ({ row, text })),
  );
  const parentPath = course.name.split(" / ").slice(0, -1).join(" / ");
  const scoped = parentPath
    ? candidates.filter(
        ({ row }) => courseLabel(row.parentPath) === courseLabel(parentPath),
      )
    : [];
  const choices = parentPath ? scoped : candidates;
  const distinct = [
    ...new Map(choices.map((choice) => [choice.row.ordinal, choice])).values(),
  ];
  if (distinct.length > 1)
    throw new Error(
      `SzPlayer 中有多个课程路径完全相同：${course.name}，无法唯一定位`,
    );
  const target = distinct[0];
  if (!target) return;
  const matchingRows = visibleRows.filter((row) =>
    row.labels.some((text) => text.trim() === target.text.trim()),
  );
  if (matchingRows.length > 1) {
    const scopedMatches = matchingRows.filter(
      (row) => courseLabel(row.parentPath) === courseLabel(parentPath),
    );
    if (!parentPath || scopedMatches.length !== 1)
      throw new Error(
        `SzPlayer 中有多个可见课程名为“${target.text}”，请使用完整课程路径或收起其他同名课程目录后重试`,
      );
    return {
      text: target.text,
      match: {
        ordinal: matchingRows.findIndex(
          (row) => row.ordinal === target.row.ordinal,
        ),
        count: matchingRows.length,
        path: [
          ...target.row.parentPath.split(" / ").filter(Boolean),
          target.text.trim(),
        ],
        expectedPid: snapshot.pid,
      },
    };
  }
  return {
    text: target.text,
    match: {
      ordinal: 0,
      count: 1,
      path: [
        ...target.row.parentPath.split(" / ").filter(Boolean),
        target.text.trim(),
      ],
      expectedPid: snapshot.pid,
    },
  };
}

export function completionReached(
  snapshot: PlayerSnapshot,
  progressHasStarted: boolean,
) {
  const { seconds, totalSeconds } = readPlaybackProgress(snapshot);
  return (
    snapshot.running &&
    !snapshot.readIssue &&
    progressHasStarted &&
    progressValues(snapshot)[0] === 100 &&
    !!totalSeconds &&
    seconds === totalSeconds
  );
}

const fatalPlayerMessages = [
  "请不要在虚拟机中运行",
  "请授权播放器有文件访问权限后再播放",
  "深造加密蓝牙设备识别异常",
  "初始化异常",
  "版本验证不通过",
  "不是最新版本",
];

const playbackFailureMessages = [
  "该视频老师设置只能下载后播放",
  "该视频需下载完成后播放",
  "视频不存在",
  "网络异常",
  "连不上服务器",
  "上一个视频还在加载数据",
];

function playerMessageElements(snapshot: PlayerSnapshot) {
  let excludedDepth: number | undefined;
  return snapshot.elements.filter((element) => {
    if (excludedDepth !== undefined && element.depth !== undefined) {
      if (element.depth > excludedDepth) return false;
      excludedDepth = undefined;
    }
    if (
      ["AXOutline", "AXRow", "AXMenuBar", "AXMenuItem"].includes(element.role)
    ) {
      excludedDepth = element.depth;
      return false;
    }
    return true;
  });
}

function visibleText(snapshot: PlayerSnapshot) {
  return playerMessageElements(snapshot)
    .flatMap((element) => [element.title, element.value])
    .filter(Boolean)
    .join("\n");
}

function hasResumePrompt(snapshot: PlayerSnapshot) {
  return playerMessageElements(snapshot).some((element) =>
    [element.title, element.value].some((value) =>
      /是否继续(?:播放上次关闭的|上次播放的)视频位置/.test(value ?? ""),
    ),
  );
}

function assertNoBlockingState(snapshot: PlayerSnapshot, allowResume = false) {
  const text = visibleText(snapshot);
  const recordingAlert = playerMessageElements(snapshot)
    .flatMap((element) => [element.title, element.value])
    .find((value) => value?.includes("请关闭以下进程或者取消其录屏权限"));
  if (recordingAlert) {
    const process = /\b[A-Za-z][\w-]*(?:\.[A-Za-z][\w-]*){2,}\b/.exec(
      recordingAlert,
    )?.[0];
    throw new Error(
      `SzPlayer 检测到录屏进程${process ? ` ${process}` : ""}，已拒绝播放；请先退出播放器弹窗列出的进程，再重新打开 SzPlayer`,
    );
  }
  const fatal = fatalPlayerMessages.find((message) => text.includes(message));
  if (fatal) throw new Error(`SzPlayer 环境检查未通过：${fatal}`);
  if (text.includes("请输入验证码"))
    throw new Error("SzPlayer 需要人工完成验证码后才能继续");
  if (!allowResume && hasResumePrompt(snapshot))
    throw new Error("请先在 SzPlayer 中处理“是否继续播放”提示");
  if (detectPlayerPage(snapshot) === "login")
    throw new Error("请先在 SzPlayer 中完成登录");
  const failure = playbackFailureMessages.find((message) =>
    text.includes(message),
  );
  if (failure) throw new Error(`SzPlayer 无法继续播放：${failure}`);
}

// Recovery must keep the same media. In particular, never answer a resume
// history dialog with “否” or reopen a course while continuing an existing run.
export function validatePlaybackRecovery(
  snapshot: PlayerSnapshot,
  request: PlaybackRecoveryRequest,
) {
  if (!snapshot.running) throw new Error("SzPlayer 已退出，无法继续当前视频");
  if (request.expectedPid && snapshot.pid !== request.expectedPid)
    throw new Error("SzPlayer 进程已变化，无法确认原视频，任务保持暂停");
  assertNoBlockingState(snapshot);
  if (snapshot.readIssue) throw new Error(snapshot.readIssue.message);
  if (request.sampleAt && snapshot.at === request.sampleAt)
    throw new Error("播放器仍返回旧进度快照，任务保持暂停");
  const progress = readPlaybackProgress(snapshot);
  if (progress.seconds === undefined || !progress.totalSeconds)
    throw new Error("仍无法读取当前视频进度，任务保持暂停");
  if (
    request.progress.totalSeconds &&
    progress.totalSeconds !== request.progress.totalSeconds
  )
    throw new Error("视频总时长已变化，无法继续原课程，任务保持暂停");
  if (
    request.progress.seconds !== undefined &&
    progress.seconds + 1 < request.progress.seconds
  )
    throw new Error("视频位置已回退，无法确认续播位置，任务保持暂停");
  return progress;
}

// Only this known playback-history decision is automatic. A positive/negative
// pair must be unambiguous; uncertain clicks are never repeated.
async function dismissResumePrompt(
  dependencies: OrchestrationDependencies,
  signal: AbortSignal,
  snapshot: PlayerSnapshot,
  timed: <T>(operation: () => Promise<T>) => Promise<T> = (operation) =>
    operation(),
): Promise<PlayerSnapshot> {
  assertNoBlockingState(snapshot, true);
  if (!hasResumePrompt(snapshot)) return snapshot;
  if (snapshot.readIssue)
    throw new Error("续播提示读取不完整，无法确认“否”按钮");
  const buttons = playerMessageElements(snapshot).filter(
    (element) => element.role === "AXButton",
  );
  const matches = (word: string) =>
    buttons.filter((element) =>
      [element.title, element.value].some(
        (value) => normalized(value) === word,
      ),
    );
  const no = matches("否"),
    yes = matches("是");
  if (no.length !== 1 || yes.length !== 1)
    throw new Error("续播提示的“是/否”按钮不唯一或不可见，已停止自动操作");
  const label = [no[0]!.title, no[0]!.value].find(
    (value) => normalized(value) === "否",
  )!;
  await checkpoint(dependencies, signal);
  await timed(() => dependencies.click(label.trim(), 1));
  dependencies.onLog("info", "已自动选择“否”，从头播放，不沿用上次位置");
  for (let attempt = 0; attempt < 10; attempt++) {
    await checkpoint(dependencies, signal);
    await timed(() => dependencies.sleep(500, signal));
    await checkpoint(dependencies, signal);
    const current = await timed(() => dependencies.snapshot());
    await checkpoint(dependencies, signal);
    if (!current.running) throw new Error("SzPlayer 已退出");
    if (snapshot.pid && current.pid && snapshot.pid !== current.pid)
      throw new Error("SzPlayer 已重启，续播提示状态已失效");
    assertNoBlockingState(current, true);
    if (
      !current.readIssue &&
      current.elements.length &&
      !hasResumePrompt(current)
    )
      return current;
  }
  throw new Error("已选择“否”，但续播提示仍未消失；已停止重复点击");
}

export async function preparePlayer(
  dependencies: OrchestrationDependencies,
  config: OrchestrationConfig,
  signal: AbortSignal,
) {
  await checkpoint(dependencies, signal);
  dependencies.onStep("checkingPlayer");
  let snapshot = await dependencies.snapshot();
  if (!snapshot.running) throw new Error("SzPlayer 尚未打开，请先打开播放器");
  abortError(signal);
  if (!snapshot.elements.length)
    throw new Error("无法读取 SzPlayer 可见控件，请检查辅助功能权限后重试");
  snapshot = await dismissResumePrompt(dependencies, signal, snapshot);
  if (snapshot.windowMode === "minimized")
    throw new Error("请先恢复 SzPlayer 窗口，再选择课程");
  if (snapshot.windowMode === "fullscreen") {
    await checkpoint(dependencies, signal);
    await dependencies.setFullscreen(false);
    await checkpoint(dependencies, signal);
    snapshot = await dependencies.snapshot();
    if (!snapshot.running) throw new Error("SzPlayer 已退出");
    snapshot = await dismissResumePrompt(dependencies, signal, snapshot);
  }
  const page = detectPlayerPage(snapshot);
  if (page === "courses") return snapshot;
  if (page === "playback")
    throw new Error("请在 SzPlayer 中切回“本地文件”课程列表");
  throw new Error("请先在 SzPlayer 中进入课程列表");
}

export async function discoverCourses(
  dependencies: OrchestrationDependencies,
  _config: OrchestrationConfig,
  signal: AbortSignal,
) {
  abortError(signal);
  const snapshot = await dependencies.courseSnapshot();
  abortError(signal);
  if (!snapshot || !Array.isArray(snapshot.elements)) {
    throw new Error(
      snapshot?.running
        ? "无法读取 SzPlayer 课程控件，请检查辅助功能权限后重试"
        : "请先打开 SzPlayer 并加载课程目录，再获取课程",
    );
  }
  if (!snapshot.running)
    throw new Error("请先打开 SzPlayer 并加载课程目录，再获取课程");
  assertNoBlockingState(snapshot);
  if (snapshot.readIssue)
    throw new Error(snapshot.readIssue.message || "课程目录读取不完整，请重试");
  const courses = coursesFromSnapshot(snapshot);
  if (!courses.length && detectPlayerPage(snapshot) !== "courses")
    throw new Error("无法确认 SzPlayer 课程列表，请切回本地文件后重试");
  return courses;
}

export function createAbortableSleep(
  waitFor?: (milliseconds: number) => Promise<void>,
) {
  return (milliseconds: number, signal: AbortSignal) =>
    new Promise<void>((resolve, reject) => {
      if (signal.aborted) {
        reject(new OrchestrationInterrupted());
        return;
      }
      let timer: ReturnType<typeof setTimeout> | undefined;
      let settled = false;
      const finish = (error?: unknown) => {
        if (settled) return;
        settled = true;
        if (timer !== undefined) clearTimeout(timer);
        signal.removeEventListener("abort", onAbort);
        if (error !== undefined) reject(error);
        else resolve();
      };
      const onAbort = () => finish(new OrchestrationInterrupted());
      signal.addEventListener("abort", onAbort, { once: true });
      if (waitFor) {
        // Native IPC replies wake a background WKWebView; web timers are throttled.
        Promise.resolve()
          .then(() => waitFor(milliseconds))
          .then(() => finish(), finish);
      } else {
        timer = setTimeout(() => finish(), milliseconds);
      }
    });
}

// SzPlayer rounds its clock to seconds, and showing the controls after finish
// can replace 100 with the final decoded frame. This is only a candidate; the
// loop requires a started course and five seconds of fresh, unchanged samples.
function roundedFinalFrame(
  snapshot: PlayerSnapshot,
  playback: PlaybackProgress,
) {
  if (
    !snapshot.elements.some(
      (element) =>
        element.role === "AXSlider" && element.identifier === "_NS:26",
    ) ||
    !snapshot.elements.some(
      (element) =>
        element.role === "AXStaticText" && element.identifier === "_NS:8",
    )
  )
    return false;
  const percent = progressValues(snapshot)[0];
  return (
    percent !== undefined &&
    percent < 100 &&
    isFinalFrameProgress({ ...playback, percent })
  );
}

// A terminal-looking frame is enough to avoid a toggle that may restart the
// video. Automatic completion still requires fresh, stable native samples.
export function isFinalFrameProgress(playback: PlaybackProgress) {
  const { seconds, totalSeconds, percent } = playback;
  return (
    !!totalSeconds &&
    seconds === totalSeconds &&
    percent !== undefined &&
    percent > 0 &&
    percent <= 100 &&
    totalSeconds * (1 - percent / 100) <= 0.5
  );
}

// A course switch initializes a new media player after the early volume
// preset. Apply again only after fresh progress proves that media has started.
async function applyStartedPlaybackVolume(
  dependencies: OrchestrationDependencies,
  config: OrchestrationConfig,
  signal: AbortSignal,
  timed: <T>(operation: () => Promise<T>) => Promise<T>,
) {
  let lastError: unknown;
  const now = dependencies.now ?? (() => performance.now());
  const startedAt = now();
  for (let attempt = 1; attempt <= 3; attempt++) {
    await checkpoint(dependencies, signal);
    try {
      await timed(() => dependencies.setVolume(config.playbackVolumePercent));
      await checkpoint(dependencies, signal);
      dependencies.onLog(
        "info",
        "播放音量已设置并校验",
        `目标音量：${config.playbackVolumePercent}%${attempt > 1 ? `；第 ${attempt} 次尝试成功` : ""}`,
        { step: "configuringPlayback", elapsedMs: now() - startedAt },
      );
      return;
    } catch (error) {
      lastError = error;
      await checkpoint(dependencies, signal);
      if (attempt < 3) await timed(() => dependencies.sleep(500, signal));
    }
  }
  dependencies.onLog(
    "warn",
    "播放音量设置未成功，已尝试 3 次",
    String(lastError),
    { step: "configuringPlayback" },
  );
  return String(lastError);
}

const READ_RECOVERY_MS = 15_000;
const START_TIMEOUT_MS = 30_000;
const STALLED_PLAYBACK_MS = 120_000;

async function waitForCompletion(
  dependencies: OrchestrationDependencies,
  config: OrchestrationConfig,
  baseline: PlayerSnapshot,
  signal: AbortSignal,
) {
  const now = dependencies.now ?? Date.now;
  let previous = readPlaybackProgress(baseline);
  let activeElapsed = 0;
  let progressHasStarted = false;
  let startupSince = 0;
  let continuedAfterRecovery = false;
  let newCoursePromptDismissed = false;
  let resumeConfirmationAt: number | undefined;
  let lastChangedAt = 0;
  let unavailableSince: number | undefined;
  let stableCompletionSamples = 0;
  let completionDuration: number | undefined;
  let playingDuration: number | undefined;
  let lastProgressLogAt: number | undefined;
  let finalFrame: { key: string; since: number; samples: number } | undefined;
  let lastSampleAt: string | undefined;
  let fullscreenRecoveryAttempted = false;
  const timed = async <T>(operation: () => Promise<T>): Promise<T> => {
    const startedAt = now();
    try {
      return await operation();
    } finally {
      activeElapsed += Math.max(0, now() - startedAt);
    }
  };
  const maxElapsed = config.maxPlaybackMinutes * 60_000;
  let deadline = maxElapsed;
  let furthestSeconds = 0;
  let furthestPercent = 0;
  const requireCapture = () => {
    if (!dependencies.captureActive())
      throw new Error(
        "采集已停止或接收服务已断开，播放编排已中止；请检查接收服务后重试",
      );
  };
  const recover = async (message: string) => {
    await checkpoint(dependencies, signal);
    if (!dependencies.recoverPlayback) throw new Error(message);
    await dependencies.recoverPlayback(
      {
        message,
        progress: { ...previous },
        expectedPid: baseline.pid,
        sampleAt: lastSampleAt,
      },
      signal,
    );
    await checkpoint(dependencies, signal);
    requireCapture();
    // Recovery and manual pause are excluded from active watchdog time. Keep
    // the same baseline, course-start proof and capture; reset only timers and
    // terminal sampling so stale EOF evidence cannot complete the course.
    continuedAfterRecovery = true;
    resumeConfirmationAt = activeElapsed;
    startupSince = activeElapsed;
    lastChangedAt = activeElapsed;
    deadline = activeElapsed + maxElapsed;
    unavailableSince = undefined;
    stableCompletionSamples = 0;
    completionDuration = undefined;
    finalFrame = undefined;
    lastSampleAt = undefined;
  };
  const recoverFullscreen = async () => {
    // AX can lose the entire hidden fullscreen control bar. Posting mouseMoved
    // to the process does not deliver it to that view when the pointer is away.
    // Leave fullscreen once, while the media is still playing, so EOF remains
    // observable. Never turn a cleared 0/0 state into a completion signal.
    fullscreenRecoveryAttempted = true;
    await checkpoint(dependencies, signal);
    await timed(() => dependencies.setFullscreen(false));
    await checkpoint(dependencies, signal);
    requireCapture();
    stableCompletionSamples = 0;
    finalFrame = undefined;
    lastSampleAt = undefined;
    dependencies.onLog(
      "warn",
      "全屏进度无法持续读取，已自动切换窗口播放，继续监控本课",
    );
  };
  while (true) {
    await checkpoint(dependencies, signal);
    await timed(() => dependencies.sleep(config.pollIntervalMs, signal));
    await checkpoint(dependencies, signal);
    requireCapture();
    if (dependencies.manualComplete())
      return {
        manual: true,
        level: "warn" as const,
        message: "用户确认本课完成",
        detail: progressDetail(previous),
      };
    let current: PlayerSnapshot;
    try {
      current = await timed(() => dependencies.playbackSnapshot());
    } catch (error) {
      await recover(
        `播放进度读取失败：${error instanceof Error ? error.message : String(error)}`,
      );
      continue;
    }
    await checkpoint(dependencies, signal);
    requireCapture();
    if (!current.running) throw new Error("SzPlayer 已退出");
    if (baseline.pid && current.pid && baseline.pid !== current.pid)
      throw new Error("SzPlayer 已重启，当前课程状态已失效，请重新开始任务");
    if (
      hasResumePrompt(current) &&
      (continuedAfterRecovery ||
        dependencies.playbackContinued?.() ||
        newCoursePromptDismissed)
    ) {
      await recover(
        newCoursePromptDismissed
          ? "历史播放提示再次出现，请确认当前课程和播放位置；任务不会自动选择从头播放"
          : "续播过程中出现历史播放提示，请确认继续上次位置；任务不会自动选择从头播放",
      );
      continue;
    }
    if (hasResumePrompt(current)) {
      // Opening a fresh course may start its clock before the history dialog
      // arrives. Progress alone does not mean the task has resumed playback.
      // Decline only the first new-course decision; continuation never rewinds.
      current = await dismissResumePrompt(dependencies, signal, current, timed);
      newCoursePromptDismissed = true;
      previous = {};
      progressHasStarted = false;
      startupSince = activeElapsed;
      stableCompletionSamples = 0;
      finalFrame = undefined;
      lastSampleAt = undefined;
      unavailableSince = undefined;
      lastChangedAt = activeElapsed;
      requireCapture();
    } else {
      assertNoBlockingState(current);
    }
    const playback = readPlaybackProgress(current);
    const repeated = !!current.at && current.at === lastSampleAt;
    lastSampleAt = current.at || undefined;
    const canRecoverFullscreen =
      current.windowMode === "fullscreen" &&
      !current.readIssue &&
      !repeated &&
      !fullscreenRecoveryAttempted;
    if (
      canRecoverFullscreen &&
      (playback.seconds === undefined || progressValues(current).length === 0)
    ) {
      await recoverFullscreen();
      continue;
    }
    const unavailable =
      current.readIssue?.message ||
      (repeated
        ? "SzPlayer 返回了重复的进度快照"
        : playback.seconds === undefined && progressValues(current).length === 0
          ? "SzPlayer 暂时未暴露有效播放进度"
          : undefined);
    if (unavailable) {
      stableCompletionSamples = 0;
      finalFrame = undefined;
      if (unavailableSince === undefined) {
        unavailableSince = activeElapsed;
        dependencies.onLog(
          "warn",
          "暂时无法读取播放进度，正在等待恢复",
          unavailable,
        );
      }
      if (activeElapsed - unavailableSince >= READ_RECOVERY_MS)
        await recover(`播放进度连续 15 秒无法读取：${unavailable}`);
      continue;
    }
    if (unavailableSince !== undefined) {
      dependencies.onLog(
        "info",
        "播放进度读取已恢复",
        `中断读取时长：${elapsedText(activeElapsed - unavailableSince)}`,
      );
      unavailableSince = undefined;
    }
    const slider = progressValues(current)[0];
    if (
      playingDuration !== undefined &&
      playback.totalSeconds &&
      playback.totalSeconds !== playingDuration
    ) {
      throw new Error(
        "播放总时长发生变化，可能切换了课程；已停止并保留当前提取记录，请重新确认课程",
      );
    }
    dependencies.onProgress?.(playback);
    const changed =
      (playback.seconds !== undefined &&
        playback.seconds !== previous.seconds) ||
      (slider !== undefined &&
        Math.abs(slider - (previous.percent ?? 0)) > 0.0001);
    const beforeClockEnd =
      !playback.totalSeconds ||
      playback.seconds === undefined ||
      playback.seconds < playback.totalSeconds;
    const moving =
      (beforeClockEnd &&
        slider !== undefined &&
        slider > 0 &&
        slider < 100 &&
        changed) ||
      (playback.seconds !== undefined &&
        !!playback.totalSeconds &&
        playback.seconds > (previous.seconds ?? 0) &&
        playback.seconds < playback.totalSeconds);
    // A new course can initially expose the prior video's larger EOF clock.
    // Only observed course playback may establish the forward-progress record
    // used by both the inactivity watchdog and recovery confirmation.
    const observingCourse = progressHasStarted || moving;
    // A saved ten-minute watchdog must not cut off a healthy longer lesson.
    // Renew only on fresh forward progress, preferring the media clock over
    // slider jitter. Native read time still counts when progress is stationary.
    const advanced =
      observingCourse &&
      (playback.seconds !== undefined
        ? playback.seconds > furthestSeconds
        : slider !== undefined && slider > furthestPercent);
    if (advanced) {
      deadline = activeElapsed + maxElapsed;
      if (resumeConfirmationAt !== undefined) {
        dependencies.onLog("info", "播放恢复已确认", progressDetail(playback));
        resumeConfirmationAt = undefined;
      }
    }
    if (observingCourse) {
      furthestSeconds = Math.max(furthestSeconds, playback.seconds ?? 0);
      furthestPercent = Math.max(furthestPercent, slider ?? 0);
    }
    if (changed) lastChangedAt = activeElapsed;
    if (
      canRecoverFullscreen &&
      progressHasStarted &&
      activeElapsed - lastChangedAt >= 3000 &&
      !completionReached(current, true) &&
      !roundedFinalFrame(current, playback)
    ) {
      await recoverFullscreen();
      continue;
    }
    if (!progressHasStarted && moving) {
      progressHasStarted = true;
      dependencies.onPlaybackStarted?.();
      dependencies.onLog("info", "已确认播放启动", progressDetail(playback), {
        step: "startingPlayback",
        elapsedMs: activeElapsed,
      });
      dependencies.onStep("configuringPlayback");
      const volumeError = await applyStartedPlaybackVolume(
        dependencies,
        config,
        signal,
        timed,
      );
      if (volumeError) {
        previous = playback;
        await recover(`新视频的音量连续 3 次设置失败：${volumeError}`);
      }
      if (config.playbackRate !== null) {
        dependencies.onStep("configuringPlayback");
        await checkpoint(dependencies, signal);
        const rateAt = now();
        await timed(() => dependencies.setPlaybackRate(config.playbackRate!));
        dependencies.onLog(
          "info",
          "播放倍速已设置",
          `目标倍速：${config.playbackRate.toFixed(1)}×`,
          { elapsedMs: now() - rateAt },
        );
        await checkpoint(dependencies, signal);
      }
      dependencies.onStep("waitingCompletion");
    }
    if (
      progressHasStarted &&
      playback.totalSeconds &&
      playingDuration === undefined
    )
      playingDuration = playback.totalSeconds;
    if (
      progressHasStarted &&
      (lastProgressLogAt === undefined ||
        activeElapsed - lastProgressLogAt >= 15_000)
    ) {
      dependencies.onLog(
        "info",
        "播放进度",
        progressDetail(playback, activeElapsed - lastChangedAt),
        { kind: "progress", step: "waitingCompletion" },
      );
      lastProgressLogAt = activeElapsed;
    }
    previous = playback;
    if (
      !progressHasStarted &&
      activeElapsed - startupSince >= START_TIMEOUT_MS
    ) {
      await recover(
        "双击课程后 30 秒内未检测到播放启动，请检查 SzPlayer 是否打开课程或出现提示框",
      );
      continue;
    }
    if (progressHasStarted && roundedFinalFrame(current, playback)) {
      const key = `${playback.totalSeconds}/${slider}`;
      if (!finalFrame || finalFrame.key !== key)
        finalFrame = { key, since: activeElapsed, samples: 0 };
      finalFrame.samples++;
      if (finalFrame.samples >= 3 && activeElapsed - finalFrame.since >= 5000) {
        return {
          level: "info" as const,
          message: "已确认播放完成",
          detail: `${progressDetail(playback)}\n确认依据：时间到达终点，末帧在半秒余量内连续 5 秒保持稳定`,
        };
      }
    } else finalFrame = undefined;
    if (completionReached(current, progressHasStarted)) {
      if (completionDuration !== playback.totalSeconds)
        stableCompletionSamples = 0;
      completionDuration = playback.totalSeconds;
      if (++stableCompletionSamples >= 3)
        return {
          level: "info" as const,
          message: "已确认播放完成",
          detail: `${progressDetail(playback)}\n确认依据：连续 3 次新鲜采样均显示播放结束`,
        };
    } else {
      stableCompletionSamples = 0;
      completionDuration = undefined;
    }
    if (
      resumeConfirmationAt !== undefined &&
      activeElapsed - resumeConfirmationAt >= START_TIMEOUT_MS
    ) {
      await recover("恢复播放后 30 秒内仍未观察到进度推进");
      continue;
    }
    if (
      progressHasStarted &&
      activeElapsed - lastChangedAt >= STALLED_PLAYBACK_MS
    ) {
      await recover(
        "连续 120 秒未观察到播放进度变化，请检查播放器是否暂停或正在缓冲",
      );
      continue;
    }
    if (activeElapsed >= deadline)
      await recover(
        `连续 ${config.maxPlaybackMinutes} 分钟未观察到有效播放推进，已暂停本课`,
      );
  }
}

async function waitForCourse(
  dependencies: OrchestrationDependencies,
  config: OrchestrationConfig,
  course: Course,
  signal: AbortSignal,
  initial?: PlayerSnapshot,
) {
  const now = dependencies.now ?? Date.now;
  let elapsed = 0;
  while (elapsed < config.courseReadyTimeoutSeconds * 1000) {
    await checkpoint(dependencies, signal);
    let tick = now();
    let current = initial ?? (await dependencies.snapshot());
    initial = undefined;
    elapsed += Math.max(0, now() - tick);
    await checkpoint(dependencies, signal);
    if (!current.running) throw new Error("SzPlayer 已退出");
    current = await dismissResumePrompt(dependencies, signal, current);
    const target = findCourseTarget(current, course);
    if (target) return target;
    tick = now();
    await dependencies.sleep(config.pollIntervalMs, signal);
    elapsed += Math.max(0, now() - tick);
  }
  throw new Error(
    `等待 ${config.courseReadyTimeoutSeconds} 秒仍未看到课程：${course.name}`,
  );
}

export async function executeCourse(
  dependencies: OrchestrationDependencies,
  config: OrchestrationConfig,
  course: Course,
  signal: AbortSignal,
) {
  let captureStarted = false;
  let captureStartAttempted = false;
  let playbackCompleted = false;
  const now = dependencies.now ?? (() => performance.now());
  let actionAt = now();
  try {
    const prepared = await preparePlayer(dependencies, config, signal);
    dependencies.onLog(
      "info",
      "播放器检查通过",
      "已确认播放器正在运行，课程列表可读取",
      { elapsedMs: now() - actionAt },
    );
    actionAt = now();
    dependencies.onStep("waitingCourse");
    await checkpoint(dependencies, signal);
    const path = course.name.split(" / ");
    path[path.length - 1] = courseTargetText(course);
    const revealed = await dependencies.revealCourse(path, prepared.pid);
    await checkpoint(dependencies, signal);
    if (prepared.pid && revealed.pid !== prepared.pid)
      throw new Error("SzPlayer 进程已变化，请重新检查播放器后重试");
    await waitForCourse(dependencies, config, course, signal, revealed);
    dependencies.onLog("info", "课程定位成功", undefined, {
      elapsedMs: now() - actionAt,
    });
    actionAt = now();

    dependencies.onStep("startingCapture");
    await checkpoint(dependencies, signal);
    if (dependencies.captureActive())
      throw new Error("已有提取任务正在运行，请先停止");
    captureStartAttempted = true;
    await dependencies.startCapture();
    captureStarted = true;
    if (!dependencies.captureActive())
      throw new Error("抓包 API 未进入运行状态，已停止播放");
    await checkpoint(dependencies, signal);
    dependencies.onLog("info", "提取已启动", "接收服务已确认就绪", {
      elapsedMs: now() - actionAt,
    });

    dependencies.onStep("selectingCourse");
    let baseline = await dependencies.snapshot();
    if (!baseline.running) throw new Error("SzPlayer 已退出");
    baseline = await dismissResumePrompt(dependencies, signal, baseline);
    const courseTarget = findCourseTarget(baseline, course);
    if (!courseTarget)
      throw new Error(
        `开始采集后 SzPlayer 课程列表已变化，请重新选择课程：${course.name}`,
      );
    dependencies.onProgress?.(readPlaybackProgress(baseline));
    await checkpoint(dependencies, signal);
    await dependencies.click(courseTarget.text, 2, courseTarget.match);
    dependencies.onStep("startingPlayback");
    await checkpoint(dependencies, signal);
    await dependencies.sleep(config.actionDelayMs, signal);

    await checkpoint(dependencies, signal);
    try {
      actionAt = now();
      await dependencies.setVolume(config.playbackVolumePercent);
      dependencies.onLog(
        "info",
        "已预设播放音量",
        `目标音量：${config.playbackVolumePercent}%；新视频启动后再次校验`,
        { step: "configuringPlayback", elapsedMs: now() - actionAt },
      );
    } catch (error) {
      dependencies.onLog(
        "warn",
        "音量预设未成功，待新视频启动后重试",
        String(error),
      );
    }

    const completion = await waitForCompletion(
      dependencies,
      config,
      baseline,
      signal,
    );
    if ("manual" in completion && completion.manual)
      await dependencies.pausePlayback?.();
    playbackCompleted = true;
    dependencies.onCaptureCheckpoint?.({
      name: courseCaptureName(course),
      playbackCompleted: true,
    });
    dependencies.onLog(
      completion.level,
      completion.message,
      completion.detail,
      { step: "waitingCompletion" },
    );

    dependencies.onStep("stoppingCapture");
    actionAt = now();
    const entry = await dependencies.stopCapture();
    captureStarted = false;
    if (!entry) throw new Error("停止采集后没有生成密钥记录");

    dependencies.onLog(
      "info",
      "提取已结束",
      entry.requestCount === undefined
        ? undefined
        : `本课共记录 ${entry.requestCount} 条请求`,
      { elapsedMs: now() - actionAt },
    );
    dependencies.onStep("savingCapture");
    const captureName = courseCaptureName(course);
    dependencies.onCaptureCheckpoint?.({
      entry,
      name: captureName,
      playbackCompleted: true,
    });
    try {
      actionAt = now();
      await dependencies.saveCapture(entry, captureName);
    } catch (error) {
      throw new CaptureSaveFailed(entry, captureName, error);
    }
    dependencies.onLog(
      entry.valid === false ? "warn" : "info",
      entry.valid === false ? "记录已保存，密钥完整性未通过" : "提取记录已保存",
      captureResultDetail({ ...entry, name: captureName }),
      { elapsedMs: now() - actionAt },
    );
    return entry;
  } catch (error) {
    if (playbackCompleted) {
      if (error instanceof CaptureSaveFailed) throw error;
      throw new CaptureFinalizeFailed(error);
    }
    // Every failed playback exit must quiesce the video before archiving its
    // capture. A failed pause must not prevent the record from being saved.
    try {
      await dependencies.pausePlayback?.();
    } catch (pauseError) {
      const pauseMessage =
        pauseError instanceof Error ? pauseError.message : String(pauseError);
      const detail = `无法确认视频已暂停，请在 SzPlayer 中检查：${pauseMessage}`;
      dependencies.onLog("error", "中断收尾未能确认播放停止", detail);
      if (error instanceof OrchestrationInterrupted) {
        error.cleanupFailed = true;
        error.message = detail;
      } else {
        const originalMessage =
          error instanceof Error ? error.message : String(error);
        error = new Error(
          originalMessage === pauseMessage
            ? detail
            : `${originalMessage}；${detail}`,
        );
      }
    }
    if (
      captureStarted ||
      (captureStartAttempted &&
        (dependencies.captureOwned?.() || dependencies.captureActive()))
    ) {
      const name = `${courseCaptureName(course).slice(0, 76)}（中断）`;
      dependencies.onCaptureCheckpoint?.({ name, playbackCompleted: false });
      let savingPartial = false;
      try {
        const partial = await dependencies.stopCapture();
        captureStarted = false;
        if (partial) {
          dependencies.onCaptureCheckpoint?.({
            entry: partial,
            name,
            playbackCompleted: false,
          });
          savingPartial = true;
          await dependencies.saveCapture(partial, name);
          dependencies.onCaptureCheckpoint?.(null);
          dependencies.onLog(
            "warn",
            "中断记录已保存",
            captureResultDetail({ ...partial, name }),
            { step: "savingCapture" },
          );
        } else {
          dependencies.onCaptureCheckpoint?.(null);
          dependencies.onLog("warn", "提取已停止，未生成中断记录", undefined, {
            step: "stoppingCapture",
          });
        }
      } catch (stopError) {
        dependencies.onLog(
          "error",
          savingPartial ? "中断记录保存失败" : "中断后停止采集失败",
          String(stopError),
        );
        if (error instanceof OrchestrationInterrupted) {
          error.cleanupFailed = true;
          error.message =
            "任务已中断，但采集停止或记录保存失败，请检查密钥管理";
        } else {
          const message =
            error instanceof Error ? error.message : String(error);
          throw new Error(
            `${message}；采集停止或中断记录保存失败：${String(stopError)}`,
          );
        }
      }
    }
    throw error;
  }
}
