import test from "node:test";
import assert from "node:assert/strict";
import {
  courseActivity,
  formatPlaybackProgress,
} from "../src/lib/orchestration-display";

test("course activity prioritizes final and paused states over stale steps", () => {
  assert.equal(
    courseActivity("completed", "completed", "savingCapture"),
    "已完成",
  );
  assert.equal(
    courseActivity("running", "running", "waitingCompletion"),
    "播放中",
  );
  assert.equal(
    courseActivity("running", "paused", "waitingCompletion"),
    "已暂停",
  );
  assert.equal(courseActivity("pending", "running"), "等待执行");
  assert.equal(
    courseActivity("error", "paused", undefined, true),
    "保存待重试",
  );
  assert.equal(courseActivity("ended", "ended"), "已结束");
});

test("playback time includes hours and does not invent progress before playback", () => {
  assert.equal(formatPlaybackProgress(), "尚未播放");
  assert.equal(
    formatPlaybackProgress({ seconds: 3723, totalSeconds: 5400 }),
    "01:02:03 / 01:30:00",
  );
  assert.equal(formatPlaybackProgress({ percent: 99.8 }), "99.8%");
});
