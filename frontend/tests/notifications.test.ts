import test from "node:test";
import assert from "node:assert/strict";
import {
  desktopNotifications,
  notificationEnabled,
  setNotificationEnabled,
} from "../src/lib/notifications";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";

function restoreStorage(original: PropertyDescriptor | undefined) {
  if (original) Object.defineProperty(globalThis, "localStorage", original);
  else Reflect.deleteProperty(globalThis, "localStorage");
}

test("notification defaults and individual choices apply independently", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  globalThis.localStorage = memoryStorage();
  try {
    assert.equal(notificationEnabled("capture.start"), true);
    assert.equal(notificationEnabled("capture.saved"), true);
    assert.equal(notificationEnabled("player.courseSaved"), false);
    assert.equal(notificationEnabled("player.attention"), true);
    assert.equal(notificationEnabled("decrypt.attention"), true);

    assert.equal(setNotificationEnabled("capture.start", false), true);
    assert.equal(notificationEnabled("capture.start"), false);
    assert.equal(notificationEnabled("capture.saved"), true);
    assert.equal(setNotificationEnabled("player.courseSaved", true), true);
    assert.equal(notificationEnabled("player.courseSaved"), true);
    assert.equal(notificationEnabled("player.completed"), true);
    assert.equal(setNotificationEnabled("decrypt.completed", false), true);
    assert.equal(notificationEnabled("decrypt.completed"), false);
    assert.equal(notificationEnabled("decrypt.attention"), true);
  } finally {
    restoreStorage(original);
  }
});

test("confirmed workflow events route to their pages without duplicate final-course alerts", async () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  globalThis.localStorage = memoryStorage();
  const native = new FakeMyGo().install();
  native.handler = async (method) => {
    assert.equal(method, "NotificationService.Show");
  };
  try {
    await desktopNotifications.captureStarted();
    await desktopNotifications.captureSaved();
    await desktopNotifications.captureAttention();
    assert.deepEqual(
      native.calls.map((call) => call.args[2]),
      ["/capture", "/capture", "/capture"],
    );

    await desktopNotifications.playerStarted(2);
    await desktopNotifications.playerCourseSaved(1, 2);
    assert.equal(native.calls.length, 4); // Frequent milestones start disabled.
    setNotificationEnabled("player.courseSaved", true);
    await desktopNotifications.playerCourseSaved(1, 2);
    await desktopNotifications.playerCourseSaved(2, 2);
    assert.equal(native.calls.length, 5); // The queue result owns the last lesson.
    await desktopNotifications.playerPaused(1, 2);
    await desktopNotifications.playerResumed(1, 2);
    await desktopNotifications.playerAttention(1, 2);
    await desktopNotifications.playerEnded(1, 2);
    await desktopNotifications.playerCompleted(2);
    assert.ok(
      native.calls
        .slice(3)
        .every((call) => call.args[2] === "/player-orchestration"),
    );

    setNotificationEnabled("player.completed", false);
    await desktopNotifications.playerCourseSaved(2, 2);
    assert.match(String(native.calls.at(-1)?.args[1]), /2\/2/);

    await desktopNotifications.decryptTaskFinished({
      id: "task-1",
      status: "completed",
      total: 2,
      completed: 2,
      failed: 0,
    });
    setNotificationEnabled("decrypt.attention", false);
    await desktopNotifications.decryptTaskFinished({
      id: "task-2",
      status: "partial_failed",
      total: 2,
      completed: 1,
      failed: 1,
    });
    assert.equal(native.calls.at(-1)?.args[2], "/decrypt");
    assert.match(String(native.calls.at(-1)?.args[1]), /2/);

    const beforeTest = native.calls.length;
    setNotificationEnabled("capture.start", false);
    await desktopNotifications.captureStarted();
    assert.equal(native.calls.length, beforeTest);
    await desktopNotifications.sendTest();
    assert.equal(native.calls.at(-1)?.args[2], "");
  } finally {
    native.restore();
    restoreStorage(original);
  }
});
