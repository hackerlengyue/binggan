import test from "node:test";
import assert from "node:assert/strict";
import {
  presentExecutionLog,
  retainExecutionLogs,
  progressDetail,
  captureResultDetail,
  formatLogTime,
} from "../src/lib/orchestration-log";

test("execution records preserve messages and recorded details", () => {
  const source = {
    message: "已确认播放完成",
    course: "目录 / 第一讲",
  };
  assert.deepEqual(presentExecutionLog(source), {
    message: "已确认播放完成",
    displayDetail: undefined,
  });
  assert.equal(source.message, "已确认播放完成");
  assert.deepEqual(
    presentExecutionLog({
      message: "提取记录已保存",
      detail: "记录名称：第一讲",
    }),
    { message: "提取记录已保存", displayDetail: "记录名称：第一讲" },
  );
});

test("errors retain exact message, whitespace, multiline causes and long paths", () => {
  const message = "执行失败：目录 / 第一讲";
  const detail =
    "  native error -25200\nCaused by:\n\t/" +
    "very-long-path/".repeat(200) +
    "\n原始错误  ";
  assert.deepEqual(
    presentExecutionLog({
      message,
      detail,
      level: "error",
      course: "目录 / 第一讲",
    }),
    { message, displayDetail: detail },
  );
});

test("progress retention preserves lifecycle and failure records under a long run", () => {
  const entries = [
    { id: "start" },
    ...Array.from({ length: 600 }, (_, i) => ({
      id: String(i),
      kind: "progress",
    })),
    { id: "failure" },
  ];
  const kept = retainExecutionLogs(entries, 500);
  assert.equal(kept.length, 500);
  assert.equal(kept[0]!.id, "start");
  assert.equal(kept.at(-1)!.id, "failure");
  assert.equal(kept.at(-2)!.id, "599");
  assert.equal(entries.length, 602);
});

test("capture diagnostics distinguish an empty incomplete record from unknown quality", () => {
  assert.equal(captureResultDetail({ name: "第一讲" }), "记录名称：第一讲");
  assert.match(
    captureResultDetail({
      name: "第一讲",
      requestCount: 0,
      valid: false,
      issues: ["缺少课程密码"],
    }),
    /请求记录：0 条\n密钥完整性：未通过\n缺失或异常：缺少课程密码/,
  );
  assert.match(
    progressDetail({ seconds: 12, totalSeconds: 60, percent: 20 }, 30000),
    /00:00:12 \/ 00:01:00；进度：20.00%；已 30 秒未观察到进度变化/,
  );
  assert.match(formatLogTime("2026-10-03T03:00:01.007Z"), /01\.007$/);
});
