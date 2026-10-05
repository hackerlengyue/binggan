import test from "node:test";
import assert from "node:assert/strict";
import {
  defaultTaskName,
  decryptLogEvents,
  conciseDecryptError,
} from "../src/lib/decrypt-presentation";
test("batch names use date; single videos use their own name, not key names", () => {
  assert.equal(
    defaultTaskName(["one.sz", "two.sz"], "2026-09-25 13:00:00"),
    "多任务 2026-09-25 13:00:00",
  );
  assert.equal(defaultTaskName(["视频.sz"], "now"), "视频");
  assert.equal(
    Array.from(defaultTaskName(["🙂".repeat(110) + ".sz"], "now")).length,
    100,
  );
});
test("summary keeps warnings/failure and milestones, hides decoder chatter and JSON", () => {
  const input = `[2026-09-25 13:00:00] [INFO] 子任务已创建，等待执行。任务 id
[2026-09-25 13:00:01] [INFO] 开始视频解密
[2026-09-25 13:00:01] [PROGRESS] 视频解密进度：50%
[2026-09-25 13:00:02] [STDERR] [aac @ xxx] error
[2026-09-25 13:00:02] [WARN] 局部有损修复：23 毫秒
[2026-09-25 13:00:03] [RESULT] 完整校验数据
{"secret-looking-detail":123}
[2026-09-25 13:00:04] [ERROR] 处理失败：音频检查失败；FFmpeg 解码诊断：[aac @ x] overflow`;
  const events = decryptLogEvents(input);
  assert.equal(events.length, 4);
  assert.equal(events[0]?.message, "已加入队列");
  assert.equal(events[2]?.level, "WARN");
  assert.equal(events[3]?.message, "处理失败：音频检查失败");
  assert.equal(events[3]?.time, "13:00:04");
  assert.equal(conciseDecryptError("文件不存在"), "文件不存在");
});
