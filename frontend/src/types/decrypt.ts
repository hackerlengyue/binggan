export interface VideoChoice {
  id: string;
  name: string;
}
export interface KeyChoice extends VideoChoice {
  source: string;
}
export interface DecryptJob {
  id: string;
  taskId?: string;
  name: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  status: "queued" | "running" | "completed" | "failed";
  error?: string;
  retryable?: boolean;
  attempt?: number;
  progress?: {
    stage: string;
    done: number;
    total: number;
    elapsed: number;
    note?: string;
  };
}
export interface DecryptTask {
  id: string;
  name: string;
  createdAt: string;
  total: number;
  completed: number;
  failed: number;
  status: "queued" | "running" | "completed" | "failed" | "partial_failed";
  children: DecryptJob[];
}
export const stageLabels: Record<string, string> = {
  decrypt: "解密视频",
  scan: "音频检查",
  restore: "声音还原",
  validate: "最终校验",
  complete: "保存结果",
};
export const statusLabels: Record<string, string> = {
  queued: "排队中",
  running: "进行中",
  completed: "已完成",
  failed: "失败",
  partial_failed: "部分失败",
};
export async function decryptResponse(response: Response) {
  let data;
  try {
    data = await response.json();
  } catch {
    throw new Error(`服务响应异常（${response.status}），请稍后重试。`);
  }
  if (!data || typeof data !== "object" || Array.isArray(data))
    throw new Error(`服务返回的数据格式无效（${response.status}）`);
  if (!response.ok)
    throw new Error(
      typeof data.message === "string" ? data.message : "请求失败，请重试",
    );
  return data;
}
export function activeTask(task: DecryptTask) {
  return task.status === "running" || task.status === "queued";
}
export function taskMatches(task: DecryptTask, filter: string) {
  return (
    filter === "all" ||
    (filter === "running"
      ? activeTask(task)
      : filter === "failed"
        ? ["failed", "partial_failed"].includes(task.status)
        : task.status === filter)
  );
}

/** Stage-weighted task progress; duration varies by stage and video. */
export function taskProgress(task: DecryptTask): number {
  const stages = ["decrypt", "scan", "restore", "validate"];
  const units = task.children.reduce((sum, job) => {
    if (job.status === "completed" || job.status === "failed") return sum + 1;
    if (job.status !== "running") return sum;
    if (job.progress?.stage === "complete") return sum + 0.99;
    const index = Math.max(0, stages.indexOf(job.progress?.stage || "decrypt"));
    const fraction = job.progress?.total
      ? Math.min(1, job.progress.done / job.progress.total)
      : 0;
    return sum + Math.min(0.99, (index + fraction) / 4);
  }, 0);
  return task.total ? Math.floor((units / task.total) * 100) : 0;
}
