import test from "node:test";
import assert from "node:assert/strict";
import {
  taskMatches,
  taskProgress,
  type DecryptTask,
} from "../src/types/decrypt";
test("queued tasks stay in active filter; partial failure is in failed filter", () => {
  const task = { status: "queued" } as DecryptTask;
  assert.equal(taskMatches(task, "running"), true);
  task.status = "partial_failed";
  assert.equal(taskMatches(task, "failed"), true);
  assert.equal(taskMatches(task, "completed"), false);
});
test("task progress includes running video stages and counts failed children as ended", () => {
  const task = {
    total: 2,
    children: [
      { status: "failed" },
      {
        status: "running",
        progress: { stage: "restore", done: 50, total: 100 },
      },
    ],
  } as DecryptTask;
  assert.equal(taskProgress(task), 81);
  task.children[1]!.status = "completed";
  assert.equal(taskProgress(task), 100);
});
