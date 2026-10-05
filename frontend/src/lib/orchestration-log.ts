import type {
  OrchestrationStep,
  PlaybackProgress,
} from "./player-orchestration";
import { formatDateTime } from "./datetime";
import { formatPlaybackProgress } from "./orchestration-display";

export type ExecutionLogMeta = {
  step?: OrchestrationStep;
  kind?: "progress";
  elapsedMs?: number;
};

export const executionStageLabels: Record<OrchestrationStep, string> = {
  checkingPlayer: "播放器检查",
  waitingCourse: "课程定位",
  startingCapture: "启动提取",
  selectingCourse: "打开课程",
  startingPlayback: "启动播放",
  configuringPlayback: "播放设置",
  waitingCompletion: "播放监控",
  stoppingCapture: "结束提取",
  savingCapture: "保存记录",
};

export const executionStepMessages: Partial<
  Record<OrchestrationStep, [string, string?]>
> = {
  checkingPlayer: ["开始检查播放器", "确认播放器进程和课程列表可读取"],
  waitingCourse: ["开始定位课程", "按目录路径查找，并核对课程是否可操作"],
  startingCapture: ["开始启动提取", "等待接收服务确认就绪"],
  selectingCourse: ["开始打开课程", "再次核对课程位置，再发送双击请求"],
  startingPlayback: ["播放请求已发送", "等待播放进度推进，最长等待 30 秒"],
  configuringPlayback: ["开始设置播放参数"],
  stoppingCapture: ["正在结束提取", "停止接收并整理本课记录"],
  savingCapture: ["开始保存记录"],
};

export function elapsedText(milliseconds: number) {
  const ms = Math.max(0, milliseconds);
  return ms < 1000 ? `${Math.round(ms)} 毫秒` : `${(ms / 1000).toFixed(1)} 秒`;
}

export function progressDetail(playback: PlaybackProgress, unchangedMs = 0) {
  const parts = [`播放位置：${formatPlaybackProgress(playback)}`];
  if (playback.percent !== undefined)
    parts.push(`进度：${playback.percent.toFixed(2)}%`);
  if (unchangedMs >= 15_000)
    parts.push(`已 ${Math.floor(unchangedMs / 1000)} 秒未观察到进度变化`);
  return parts.join("；");
}

export function captureResultDetail(entry: {
  name: string;
  valid?: boolean;
  issues?: string[];
  requestCount?: number;
}) {
  const facts = [`记录名称：${entry.name}`];
  if (entry.requestCount !== undefined)
    facts.push(`请求记录：${entry.requestCount} 条`);
  if (entry.valid !== undefined)
    facts.push(`密钥完整性：${entry.valid ? "通过" : "未通过"}`);
  if (entry.valid === false && entry.issues?.length)
    facts.push(`缺失或异常：${entry.issues.join("；")}`);
  return facts.join("\n");
}

// Progress samples are less valuable than action results and errors. Keep the
// same storage bound, but discard old samples before discarding lifecycle events.
export function retainExecutionLogs<T extends { kind?: string }>(
  entries: T[],
  limit: number,
): T[] {
  const excess = Math.max(0, entries.length - limit);
  if (!excess) return entries;
  let removed = 0;
  const kept = entries.filter((entry) => {
    if (entry.kind === "progress" && removed < excess) {
      removed++;
      return false;
    }
    return true;
  });
  return kept.slice(-limit);
}

export function formatLogTime(at: string) {
  const date = new Date(at);
  if (!Number.isFinite(date.getTime())) return formatDateTime(at);
  return `${formatDateTime(date)}.${String(date.getMilliseconds()).padStart(3, "0")}`;
}

// Display only recorded details and durations.
export function presentExecutionLog(entry: {
  message: string;
  detail?: string;
  course?: string;
  elapsedMs?: number;
  level?: string;
}) {
  if (entry.level === "error")
    return { message: entry.message, displayDetail: entry.detail };
  const message = entry.message;
  let detail = entry.detail;
  if (Number.isFinite(entry.elapsedMs))
    detail = [detail, `耗时：${elapsedText(entry.elapsedMs!)}`]
      .filter(Boolean)
      .join("\n");
  return { message, displayDetail: detail };
}
