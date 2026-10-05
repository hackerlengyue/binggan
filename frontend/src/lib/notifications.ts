import { NotificationService, type Finished } from "@/mygo";
import { isMyGo } from "mygo-runtime";
import { t } from "@/i18n";

export type NotificationGroup = "capture" | "player" | "decrypt";
export type NotificationKind =
  | "capture.start"
  | "capture.saved"
  | "capture.attention"
  | "player.start"
  | "player.courseSaved"
  | "player.pauseResume"
  | "player.attention"
  | "player.ended"
  | "player.completed"
  | "decrypt.completed"
  | "decrypt.attention";

export const notificationOptions: {
  kind: NotificationKind;
  group: NotificationGroup;
  label: string;
  description: string;
  defaultEnabled: boolean;
}[] = [
  {
    kind: "capture.start",
    group: "capture",
    label: "提取开始",
    description: "接收端确认开始后通知",
    defaultEnabled: true,
  },
  {
    kind: "capture.saved",
    group: "capture",
    label: "提取结束并保存",
    description: "记录写入秘钥管理后通知",
    defaultEnabled: true,
  },
  {
    kind: "capture.attention",
    group: "capture",
    label: "提取需要处理",
    description: "启动或停止异常、未生成记录时通知",
    defaultEnabled: true,
  },
  {
    kind: "player.start",
    group: "player",
    label: "编排开始",
    description: "运行条件通过并开始课程队列后通知",
    defaultEnabled: true,
  },
  {
    kind: "player.courseSaved",
    group: "player",
    label: "单课完成",
    description: "每门课程的密钥保存后通知；最后一门与队列完成合并",
    defaultEnabled: false,
  },
  {
    kind: "player.pauseResume",
    group: "player",
    label: "暂停与继续",
    description: "主动暂停或成功继续队列后通知",
    defaultEnabled: true,
  },
  {
    kind: "player.attention",
    group: "player",
    label: "编排需要处理",
    description: "课程失败并自动暂停，或结束时保存失败",
    defaultEnabled: true,
  },
  {
    kind: "player.ended",
    group: "player",
    label: "手动结束",
    description: "结束当前队列后通知",
    defaultEnabled: true,
  },
  {
    kind: "player.completed",
    group: "player",
    label: "全部完成",
    description: "所有课程的密钥记录保存后通知",
    defaultEnabled: true,
  },
  {
    kind: "decrypt.completed",
    group: "decrypt",
    label: "解密成功",
    description: "任务中的全部视频处理完成后通知",
    defaultEnabled: true,
  },
  {
    kind: "decrypt.attention",
    group: "decrypt",
    label: "解密失败",
    description: "任务部分或全部失败后通知",
    defaultEnabled: true,
  },
];

export function notificationEnabled(kind: NotificationKind): boolean {
  const option = notificationOptions.find((item) => item.kind === kind);
  if (!option) return false;
  try {
    const saved = localStorage.getItem(`szjm.notifications.${kind}.v2`);
    if (saved === "true" || saved === "false") return saved === "true";
  } catch {
    // Keep the workflow usable when WebView storage is unavailable.
  }
  return option.defaultEnabled;
}

export function setNotificationEnabled(
  kind: NotificationKind,
  enabled: boolean,
): boolean {
  if (!notificationOptions.some((item) => item.kind === kind)) return false;
  try {
    localStorage.setItem(`szjm.notifications.${kind}.v2`, String(enabled));
    return true;
  } catch {
    return false;
  }
}

async function showWhenEnabled(
  kind: NotificationKind,
  title: string,
  body: string,
) {
  if (!notificationEnabled(kind) || !isMyGo()) return;
  const route = kind.startsWith("capture.")
    ? "/capture"
    : kind.startsWith("player.")
      ? "/player-orchestration"
      : "/decrypt";
  await NotificationService.show(title, body, route);
}

export const desktopNotifications = {
  captureStarted(): Promise<void> {
    return showWhenEnabled(
      "capture.start",
      t("秘钥提取已开始"),
      t("正在接收请求，停止后保存本次记录"),
    );
  },
  captureSaved(): Promise<void> {
    return showWhenEnabled(
      "capture.saved",
      t("秘钥提取已停止"),
      t("提取记录已保存到秘钥管理"),
    );
  },
  captureAttention(): Promise<void> {
    return showWhenEnabled(
      "capture.attention",
      t("秘钥提取需要处理"),
      t("请返回提取页面检查当前记录"),
    );
  },
  playerStarted(total: number): Promise<void> {
    return showWhenEnabled(
      "player.start",
      t("播放编排已开始"),
      t("{count} 门课程已进入执行队列", { count: total }),
    );
  },
  playerCourseSaved(completed: number, total: number): Promise<void> {
    if (completed === total && notificationEnabled("player.completed"))
      return Promise.resolve();
    return showWhenEnabled(
      "player.courseSaved",
      t("单课密钥已保存"),
      t("已完成 {completed}/{total} 门课程", { completed, total }),
    );
  },
  playerPaused(completed: number, total: number): Promise<void> {
    return showWhenEnabled(
      "player.pauseResume",
      t("播放编排已暂停"),
      t("队列已暂停，已完成 {completed}/{total} 门", { completed, total }),
    );
  },
  playerResumed(completed: number, total: number): Promise<void> {
    return showWhenEnabled(
      "player.pauseResume",
      t("播放编排已继续"),
      t("队列已继续，已完成 {completed}/{total} 门", { completed, total }),
    );
  },
  playerAttention(
    completed: number,
    total: number,
    ended = false,
  ): Promise<void> {
    return showWhenEnabled(
      "player.attention",
      t("播放编排需要处理"),
      ended
        ? t(
            "任务已结束，已完成 {completed}/{total} 门；请检查运行日志与中断记录",
            { completed, total },
          )
        : t("队列已暂停，已完成 {completed}/{total} 门；请查看运行日志", {
            completed,
            total,
          }),
    );
  },
  playerEnded(completed: number, total: number): Promise<void> {
    return showWhenEnabled(
      "player.ended",
      t("播放编排已结束"),
      t("已完成 {completed}/{total} 门课程，请核对中断记录", {
        completed,
        total,
      }),
    );
  },
  playerCompleted(count: number): Promise<void> {
    return showWhenEnabled(
      "player.completed",
      t("播放编排任务完成"),
      t("{count} 门课程已完成", { count }),
    );
  },
  decryptTaskFinished(result: Finished): Promise<void> {
    return result.failed > 0
      ? showWhenEnabled(
          "decrypt.attention",
          t("视频解密需要处理"),
          t("{completed} 个完成，{failed} 个失败", {
            completed: result.completed,
            failed: result.failed,
          }),
        )
      : showWhenEnabled(
          "decrypt.completed",
          t("视频解密任务完成"),
          t("{count} 个视频已完成", { count: result.completed }),
        );
  },
  async sendTest(): Promise<void> {
    if (!isMyGo()) throw new Error("请打开桌面软件");
    await NotificationService.show(
      t("测试通知"),
      t("饼干大小姐通知功能已就绪"),
      "",
    );
  },
};
