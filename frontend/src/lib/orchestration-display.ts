import type {
  OrchestrationStep,
  PlaybackProgress,
} from "./player-orchestration";

export const orchestrationStepLabels: Record<OrchestrationStep, string> = {
  checkingPlayer: "检查播放器中",
  waitingCourse: "定位课程中",
  startingCapture: "启动提取中",
  selectingCourse: "打开课程中",
  startingPlayback: "正在启动播放",
  configuringPlayback: "调整播放设置中",
  waitingCompletion: "播放中",
  stoppingCapture: "结束提取中",
  savingCapture: "保存中",
};

export function courseActivity(
  state: string,
  runState: string,
  step?: OrchestrationStep,
  pendingSave = false,
): string {
  if (state === "completed") return "已完成";
  if (state === "ended") return "已结束";
  if (state === "error") return pendingSave ? "保存待重试" : "执行失败";
  if (state === "pending") return "等待执行";
  if (runState === "paused") return "已暂停";
  return step ? orchestrationStepLabels[step] : "准备中";
}

export function formatPlaybackProgress(playback?: PlaybackProgress): string {
  if (!playback) return "尚未播放";
  const time = (seconds: number) => {
    const whole = Math.max(0, Math.floor(seconds));
    return [
      Math.floor(whole / 3600),
      Math.floor((whole % 3600) / 60),
      whole % 60,
    ]
      .map((part) => String(part).padStart(2, "0"))
      .join(":");
  };
  if (playback.seconds !== undefined && playback.totalSeconds !== undefined)
    return `${time(playback.seconds)} / ${time(playback.totalSeconds)}`;
  if (playback.percent !== undefined) return `${playback.percent.toFixed(1)}%`;
  return "进度暂不可用";
}
