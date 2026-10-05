import test from "node:test";
import assert from "node:assert/strict";
import { createPinia, setActivePinia } from "pinia";
import { playerAutomation } from "../src/lib/player-automation";
import { usePlayerOrchestrationStore } from "../src/stores/player-orchestration";
import { useTraceStore } from "../src/stores/trace";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";

function queueFixture() {
  const storage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const original = { ...playerAutomation };
  globalThis.localStorage = memoryStorage();
  setActivePinia(createPinia());
  const trace = useTraceStore(),
    store = usePlayerOrchestrationStore();
  store.hydrate();
  let clicks = 0,
    captures = 0,
    reads = 0,
    failSave = true;
  const status = {
    platform: "darwin",
    installed: true,
    running: true,
    accessibilityReady: true,
    verifiedProfile: true,
    version: "26.06.54",
    message: "ready",
  };
  Object.assign(playerAutomation, {
    status: async () => status,
    revealCourse: async () => playerAutomation.snapshot(),
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        { role: "AXRow", depth: 2, value: "第一讲" },
        { role: "AXRow", depth: 2, value: "第二讲" },
        { role: "AXSlider", identifier: "_NS:26", width: 624, value: "0" },
        {
          role: "AXStaticText",
          identifier: "_NS:8",
          value: "00:00:00/00:01:00",
        },
      ],
    }),
    playbackSnapshot: async () => ({
      running: true,
      at: "",
      elements: [
        {
          role: "AXSlider",
          identifier: "_NS:26",
          width: 624,
          value: ++reads === 1 ? "20" : "100",
        },
        {
          role: "AXStaticText",
          identifier: "_NS:8",
          value: reads === 1 ? "00:00:12/00:01:00" : "00:01:00/00:01:00",
        },
      ],
    }),
    click: async () => {
      clicks++;
      reads = 0;
    },
    setVolume: async () => {},
  });
  trace.historyReady = true;
  trace.connected = true;
  Object.defineProperty(trace, "currentHistoryId", {
    configurable: true,
    get: () => (captures ? `capture-${captures}` : ""),
  });
  trace.startCapture = async () => {
    captures++;
    trace.keyHistory.push({
      id: `capture-${captures}`,
      name: "draft",
    } as never);
    trace.captureEnabled = true;
  };
  trace.stopCapture = async () => {
    trace.captureEnabled = false;
    return { id: `capture-${captures}`, name: "draft" };
  };
  trace.renameHistory = () => {};
  trace.flushKeyHistory = async () => {
    if (failSave) {
      failSave = false;
      throw new Error("disk unavailable");
    }
  };
  trace.completeCapture = () => {};
  store.status = status;
  store.selectedCourses = [{ id: "one", name: "第一讲" }];
  store.availableCourses = [
    { id: "one", name: "第一讲" },
    { id: "two", name: "第二讲" },
  ];
  store.courseCatalogReady = true;
  store.config.actionDelayMs = 1;
  store.config.pollIntervalMs = 1;
  return {
    store,
    trace,
    status,
    counts: () => ({ clicks, captures }),
    setSaveFailure: (value: boolean) => {
      failSave = value;
    },
    restore: () => {
      Object.assign(playerAutomation, original);
      trace.dispose();
      if (storage) Object.defineProperty(globalThis, "localStorage", storage);
      else Reflect.deleteProperty(globalThis, "localStorage");
    },
  };
}

test("retrying a completed course save never clicks or captures the lesson again", async () => {
  const task = queueFixture();
  try {
    await task.store.run();
    assert.equal(task.store.runState, "paused");
    assert.match(task.store.courses[0]!.detail!, /disk unavailable/);
    await task.store.resume();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("restoring a pending save keeps completed progress and can save with the player closed", async () => {
  const task = queueFixture();
  let recoveredTrace: ReturnType<typeof useTraceStore> | undefined;
  try {
    await task.store.run();
    assert.ok(task.store.courses[0]?.pendingCapture);
    setActivePinia(createPinia());
    recoveredTrace = useTraceStore();
    recoveredTrace.historyReady = true;
    recoveredTrace.renameHistory = () => {};
    recoveredTrace.flushKeyHistory = async () => {};
    recoveredTrace.completeCapture = () => {};
    const recovered = usePlayerOrchestrationStore();
    recovered.hydrate();
    assert.equal(recovered.runState, "paused");
    assert.equal(recovered.canStart, true);
    task.status.running = false;
    await recovered.resume();
    assert.equal(recovered.runState, "completed");
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
    setActivePinia(createPinia());
    const completed = usePlayerOrchestrationStore();
    completed.hydrate();
    assert.equal(completed.runState, "completed");
    assert.equal(completed.processedCount, 1);
    assert.equal(completed.canStart, false);
  } finally {
    recoveredTrace?.dispose();
    task.restore();
  }
});

test("a failed capture finalization retries only the owned capture", async () => {
  const task = queueFixture();
  let stops = 0;
  task.trace.stopCapture = async () => {
    if (++stops === 1) throw new Error("archive write failed");
    task.trace.captureEnabled = false;
    return { id: "capture-1", name: "draft" };
  };
  task.setSaveFailure(false);
  try {
    await task.store.run();
    assert.equal(stops, 1);
    assert.equal(task.store.courses[0]?.pendingCapture?.needsStop, true);
    await task.store.resume();
    assert.equal(task.store.runState, "completed");
    assert.equal(stops, 2);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("a queued run freezes its selection and settings across courses", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  task.store.selectedCourses.push({ id: "two", name: "第二讲" });
  const volumes: number[] = [];
  playerAutomation.setVolume = async (volume) => {
    volumes.push(volume);
    task.store.config.playbackVolumePercent = 90;
    task.store.setCourseSelected(task.store.selectedCourses[1]!, false);
  };
  try {
    await task.store.run();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(volumes, [35, 35, 35, 35]);
    assert.equal(task.store.courses.length, 2);
    assert.equal(task.store.selectedCourses.length, 2);
  } finally {
    task.restore();
  }
});

test("ending a failed save preserves the retry and prevents dropping the record", async () => {
  const task = queueFixture();
  try {
    await task.store.run();
    await task.store.endTask();
    assert.equal(task.store.runState, "paused");
    const id = task.store.courses[0]?.pendingCapture?.id;
    assert.ok(id);
    task.store.resetQueue();
    assert.equal(task.store.courses[0]?.pendingCapture?.id, id);
    await task.store.resume();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("stable decoded end frames save each course and automatically advance the queue", async (t) => {
  let now = 0;
  t.mock.method(performance, "now", () => (now += 250));
  const task = queueFixture();
  task.setSaveFailure(false);
  const sample = playerAutomation.playbackSnapshot;
  playerAutomation.playbackSnapshot = async () => {
    const current = await sample();
    const ended = current.elements[0]!.value === "100";
    if (ended) current.elements[0]!.value = "99.98220825195312";
    current.elements[1]!.value = ended
      ? "00:29:41/00:29:41"
      : "00:05:56/00:29:41";
    return current;
  };
  task.store.selectedCourses = [
    { id: "one", name: "第一讲" },
    { id: "two", name: "第二讲" },
  ];
  try {
    await task.store.run();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(
      task.store.courses.map((course) => course.captureId),
      ["capture-1", "capture-2"],
    );
    assert.deepEqual(task.counts(), { clicks: 2, captures: 2 });
  } finally {
    task.restore();
  }
});

test("two background courses recover hidden controls, save, and advance without entering fullscreen", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  const sample = playerAutomation.playbackSnapshot;
  const click = playerAutomation.click;
  let hideNext = false,
    recovered = false,
    exits = 0;
  playerAutomation.click = async (...args) => {
    hideNext = false;
    recovered = false;
    return click(...args);
  };
  playerAutomation.playbackSnapshot = async () => {
    if (hideNext && !recovered)
      return { running: true, at: "", windowMode: "fullscreen", elements: [] };
    const current = await sample();
    if (current.elements[0]!.value === "20") hideNext = true;
    return { ...current, windowMode: "windowed" };
  };
  playerAutomation.setFullscreen = async (enabled) => {
    assert.equal(
      enabled,
      false,
      "background work must never enter a fullscreen Space",
    );
    recovered = true;
    exits++;
  };
  task.store.selectedCourses = [
    { id: "one", name: "第一讲" },
    { id: "two", name: "第二讲" },
  ];
  try {
    await task.store.run();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(
      task.store.courses.map((course) => course.captureId),
      ["capture-1", "capture-2"],
    );
    assert.deepEqual(task.counts(), { clicks: 2, captures: 2 });
    assert.equal(exits, 2);
  } finally {
    task.restore();
  }
});

function timeoutFixture(t: import("node:test").TestContext) {
  const task = queueFixture();
  const native = new FakeMyGo().install();
  let now = 0;
  t.mock.method(performance, "now", () => now);
  playerAutomation.wait = async (ms) => {
    now += ms;
    await new Promise<void>((resolve) => setImmediate(resolve));
  };
  task.setSaveFailure(false);
  task.store.config.pollIntervalMs = 500;
  const events: string[] = [];
  let toggles = 0,
    stops = 0,
    samples = 0;
  let allowRecovery = true;
  const originalStop = task.trace.stopCapture;
  playerAutomation.togglePlayback = async () => {
    events.push(++toggles % 2 ? "video:pause" : "video:resume");
  };
  task.trace.pauseCapture = async () => {
    events.push("capture:pause");
    task.trace.captureEnabled = false;
  };
  task.trace.resumeCapture = async () => {
    events.push("capture:resume");
    task.trace.captureEnabled = true;
  };
  task.trace.stopCapture = async (...args) => {
    stops++;
    return originalStop(...args);
  };
  playerAutomation.playbackSnapshot = async () => {
    if (samples++ > 0 && (toggles === 0 || !allowRecovery))
      return { running: true, at: String(now), elements: [] };
    const ended = toggles >= 2;
    return {
      running: true,
      at: `${now}:${samples}`,
      elements: [
        { role: "AXSlider", identifier: "_NS:26", value: ended ? "100" : "20" },
        {
          role: "AXStaticText",
          identifier: "_NS:8",
          value: ended ? "00:01:00/00:01:00" : "00:00:12/00:01:00",
        },
      ],
    };
  };
  return {
    ...task,
    events,
    toggles: () => toggles,
    stops: () => stops,
    allowRecovery: (value: boolean) => {
      allowRecovery = value;
    },
    restore: () => {
      native.restore();
      task.restore();
    },
  };
}

async function until(check: () => boolean) {
  for (let i = 0; i < 2000; i++) {
    if (check()) return;
    await new Promise<void>((resolve) => setImmediate(resolve));
  }
  assert.fail("expected recovery state was not reached");
}

test("timeout pauses then automatically resumes the same video and capture", async (t) => {
  const task = timeoutFixture(t);
  try {
    await task.store.run();
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(task.events, [
      "video:pause",
      "capture:pause",
      "capture:resume",
      "video:resume",
    ]);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
    assert.equal(task.stops(), 1);
    assert.match(
      task.store.logs.map((entry) => entry.message).join("\n"),
      /尝试自动恢复 1 \/ 3/,
    );
  } finally {
    task.restore();
  }
});

test("three failed recovery checks hold the live run for manual continuation without replay", async (t) => {
  const task = timeoutFixture(t);
  task.allowRecovery(false);
  const run = task.store.run();
  try {
    await until(() =>
      task.store.logs.some((entry) =>
        entry.message.includes("自动恢复次数已用尽"),
      ),
    );
    assert.equal(task.store.runState, "paused");
    assert.equal(task.store.recovering, false);
    assert.equal(
      task.store.logs.filter((entry) =>
        entry.message.startsWith("尝试自动恢复"),
      ).length,
      3,
    );
    assert.equal(task.toggles(), 1);
    assert.equal(task.stops(), 0);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
    // Failed manual preflight must also retain the paused context.
    await assert.rejects(task.store.resume(), /仍无法读取/);
    assert.equal(task.store.runState, "paused");
    assert.equal(task.stops(), 0);
    task.allowRecovery(true);
    await task.store.resume();
    await run;
    assert.equal(task.store.runState, "completed");
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
    assert.equal(task.stops(), 1);
  } finally {
    if (task.store.runState === "paused" || task.store.runState === "running")
      await task.store.endTask();
    await run;
    task.restore();
  }
});

test("ending during automatic recovery cancels retries and never resumes playback", async (t) => {
  const task = timeoutFixture(t);
  task.allowRecovery(false);
  const run = task.store.run();
  try {
    await until(
      () =>
        task.store.recovering &&
        task.store.logs.some((entry) => entry.message === "尝试自动恢复 1 / 3"),
    );
    await task.store.endTask();
    await run;
    assert.equal(task.store.runState, "ended");
    assert.equal(task.store.recovering, false);
    assert.equal(task.toggles(), 1);
    assert.equal(task.events.includes("video:resume"), false);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("a lost execution context never turns Continue into a second course launch", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  playerAutomation.playbackSnapshot = async () => ({
    running: false,
    at: "",
    elements: [],
  });
  try {
    await task.store.run();
    assert.equal(task.store.runState, "paused");
    await assert.rejects(task.store.resume(), /不会重新打开视频/);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("an uncertain course click cannot be delivered again by Continue", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  let attempts = 0;
  playerAutomation.click = async () => {
    attempts++;
    throw new Error("AX reply timed out after delivery");
  };
  try {
    await task.store.run();
    assert.equal(task.store.runState, "paused");
    await assert.rejects(task.store.resume(), /不会重新打开视频/);
    assert.equal(attempts, 1);
  } finally {
    task.restore();
  }
});

test("an interrupted record save retries the record without marking unfinished video complete", async () => {
  const task = queueFixture();
  playerAutomation.playbackSnapshot = async () => ({
    running: false,
    at: "",
    elements: [],
  });
  try {
    await task.store.run();
    assert.equal(task.store.runState, "paused");
    assert.ok(
      task.store.activeCourse?.pendingCapture,
      "partial save failure must remain recoverable",
    );
    await task.store.resume();
    assert.equal(task.store.runState, "ended");
    assert.equal(task.store.processedCount, 0);
    assert.equal(task.store.courses[0]?.pendingCapture, undefined);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("repeated stalls share three recovery attempts for the course, then stay paused", async (t) => {
  const task = timeoutFixture(t);
  let samples = 0;
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: String(++samples),
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "20" },
      { role: "AXStaticText", identifier: "_NS:8", value: "00:00:12/00:01:00" },
    ],
  });
  const run = task.store.run();
  try {
    await until(() =>
      task.store.logs.some((entry) =>
        entry.message.includes("自动恢复次数已用尽"),
      ),
    );
    assert.equal(task.store.runState, "paused");
    assert.equal(
      task.store.logs.filter((entry) =>
        entry.message.startsWith("尝试自动恢复"),
      ).length,
      3,
    );
    assert.equal(
      task.toggles(),
      7,
      "initial pause + three resumes and three failed-progress pauses",
    );
    assert.equal(
      task.store.logs.some((entry) => entry.message === "播放恢复已确认"),
      false,
    );
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
    assert.equal(task.stops(), 0);
  } finally {
    await task.store.endTask();
    await run;
    task.restore();
  }
});

test("a failed pause is never followed by an automatic opposite toggle", async (t) => {
  const task = timeoutFixture(t);
  let toggles = 0;
  playerAutomation.togglePlayback = async () => {
    toggles++;
    throw new Error("uncertain AX reply");
  };
  try {
    await task.store.run();
    assert.equal(toggles, 1);
    assert.equal(task.store.runState, "ended");
    assert.equal(task.events.includes("capture:resume"), false);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    task.restore();
  }
});

test("a persisted partial-save checkpoint cannot complete the interrupted course on reload", async () => {
  const task = queueFixture();
  let restoredTrace: ReturnType<typeof useTraceStore> | undefined;
  playerAutomation.playbackSnapshot = async () => ({
    running: false,
    at: "",
    elements: [],
  });
  try {
    await task.store.run();
    setActivePinia(createPinia());
    restoredTrace = useTraceStore();
    restoredTrace.historyReady = true;
    restoredTrace.renameHistory = () => {};
    restoredTrace.flushKeyHistory = async () => {};
    restoredTrace.completeCapture = () => {};
    const recovered = usePlayerOrchestrationStore();
    recovered.hydrate();
    await recovered.resume();
    assert.equal(recovered.runState, "ended");
    assert.equal(recovered.processedCount, 0);
    assert.deepEqual(task.counts(), { clicks: 1, captures: 1 });
  } finally {
    restoredTrace?.dispose();
    task.restore();
  }
});

test("a fatal playback configuration failure pauses the video before saving the partial capture", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  const actions: string[] = [];
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "fresh",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "20" },
      { role: "AXStaticText", identifier: "_NS:8", value: "00:00:12/00:01:00" },
    ],
  });
  task.store.config.playbackRate = 1.5;
  playerAutomation.setPlaybackRate = async () => {
    throw new Error("rate menu unavailable");
  };
  playerAutomation.togglePlayback = async () => {
    actions.push("pause");
  };
  const stop = task.trace.stopCapture;
  task.trace.stopCapture = async (...args) => {
    actions.push("stop");
    return stop(...args);
  };
  try {
    await task.store.run();
    assert.deepEqual(actions, ["pause", "stop"]);
    assert.equal(task.store.processedCount, 0);
    assert.match(task.store.courses[0]!.detail!, /rate menu unavailable/);
  } finally {
    task.restore();
  }
});

test("capture ownership changes cannot archive or rename another task's record", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  const read = playerAutomation.playbackSnapshot;
  let stopped = 0,
    renamed = 0;
  playerAutomation.playbackSnapshot = async () => {
    Object.defineProperty(task.trace, "currentHistoryId", {
      configurable: true,
      value: "foreign-capture",
    });
    return read();
  };
  playerAutomation.togglePlayback = async () => {};
  task.trace.stopCapture = async () => {
    stopped++;
    return { id: "foreign-capture", name: "other" };
  };
  task.trace.renameHistory = () => {
    renamed++;
  };
  try {
    await task.store.run();
    assert.equal(stopped, 0);
    assert.equal(renamed, 0);
    assert.equal(task.store.processedCount, 0);
    assert.equal(task.store.courses[0]?.pendingCapture?.id, "capture-1");
    assert.match(task.store.courses[0]!.detail!, /提取记录.*变化|不一致/);
  } finally {
    task.restore();
  }
});

test("an archive committed before its queue checkpoint can finish without stopping another capture", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  task.store.courses = [
    {
      id: "one",
      name: "第一讲",
      state: "error",
      playbackAttempted: true,
      pendingCapture: {
        id: "archived",
        name: "第一讲",
        needsStop: true,
        playbackCompleted: true,
      },
    },
  ];
  task.store.runState = "paused";
  task.trace.keyHistory = [
    {
      id: "archived",
      name: "draft",
      stoppedAt: new Date().toISOString(),
    } as never,
  ];
  task.trace.captureEnabled = true;
  Object.defineProperty(task.trace, "currentHistoryId", {
    configurable: true,
    value: "another-live-capture",
  });
  let stopped = 0;
  task.trace.stopCapture = async () => {
    stopped++;
    throw new Error("must not stop foreign capture");
  };
  try {
    await task.store.resume();
    assert.equal(task.store.runState, "completed");
    assert.equal(stopped, 0);
    assert.deepEqual(task.counts(), { clicks: 0, captures: 0 });
    assert.equal(task.trace.captureEnabled, true);
  } finally {
    task.restore();
  }
});

test("restoring an interrupted queue preserves valid last progress without replaying", async () => {
  const task = queueFixture();
  task.store.courses = [
    {
      id: "one",
      name: "第一讲",
      state: "error",
      playbackAttempted: true,
      playback: { seconds: 12, totalSeconds: 60, percent: 20 },
      detail: "read failed",
    },
  ];
  task.store.runState = "paused";
  task.store.updateConfig();
  try {
    setActivePinia(createPinia());
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    assert.deepEqual(restored.courses[0]?.playback, {
      seconds: 12,
      totalSeconds: 60,
      percent: 20,
    });
    useTraceStore().historyReady = true;
    useTraceStore().connected = true;
    restored.status = task.status;
    await assert.rejects(restored.resume(), /不会重新打开视频/);
    assert.deepEqual(task.counts(), { clicks: 0, captures: 0 });
  } finally {
    task.restore();
  }
});

test("storage failure before course launch is visible and prevents opening a new video", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  localStorage.setItem = () => {
    throw new Error("quota exceeded");
  };
  try {
    await task.store.run();
    assert.deepEqual(task.counts(), { clicks: 0, captures: 0 });
    assert.match(
      task.store.courses[0]?.detail ?? task.store.statusError,
      /保存.*执行状态|执行状态.*保存/,
    );
  } finally {
    task.restore();
  }
});

test("failed emergency pause is not retried and does not prevent saving the interrupted capture", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  let pauses = 0,
    stops = 0;
  task.store.config.playbackRate = 1.5;
  playerAutomation.setPlaybackRate = async () => {
    throw new Error("rate failed");
  };
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "20" },
      { role: "AXStaticText", identifier: "_NS:8", value: "00:00:12/00:01:00" },
    ],
  });
  playerAutomation.togglePlayback = async () => {
    pauses++;
    throw new Error("pause result unknown");
  };
  const stop = task.trace.stopCapture;
  task.trace.stopCapture = async (...args) => {
    stops++;
    return stop(...args);
  };
  try {
    await task.store.run();
    assert.equal(pauses, 1);
    assert.equal(stops, 1);
    assert.match(
      task.store.courses[0]!.detail!,
      /rate failed.*无法确认视频已暂停/,
    );
    assert.ok(task.store.logs.some((e) => e.message === "中断记录已保存"));
    await task.store.endTask();
    assert.equal(pauses, 1);
  } finally {
    task.restore();
  }
});

test("a capture session created before startup fails is still archived as interrupted", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  const start = task.trace.startCapture,
    stop = task.trace.stopCapture;
  let stops = 0;
  task.trace.startCapture = async () => {
    await start();
    task.trace.captureEnabled = false;
    throw new Error("receiver enable failed");
  };
  task.trace.stopCapture = async (...args) => {
    stops++;
    return stop(...args);
  };
  try {
    await task.store.run();
    assert.equal(stops, 1);
    assert.deepEqual(task.counts(), { clicks: 0, captures: 1 });
    assert.ok(task.store.logs.some((e) => e.message === "中断记录已保存"));
  } finally {
    task.restore();
  }
});

test("an interrupted course never pauses a restarted player process", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  const snapshot = playerAutomation.snapshot;
  playerAutomation.snapshot = async () => ({ ...(await snapshot()), pid: 10 });
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "",
    pid: 20,
    elements: [],
  });
  let pauses = 0;
  playerAutomation.togglePlayback = async () => {
    pauses++;
  };
  try {
    await task.store.run();
    assert.equal(pauses, 0);
    assert.match(
      task.store.courses[0]!.detail!,
      /SzPlayer 已重启.*无法确认视频已暂停/,
    );
    assert.ok(task.store.logs.some((e) => e.message === "中断记录已保存"));
  } finally {
    task.restore();
  }
});

test("a replaced paused capture cannot be resumed by the original course", async (t) => {
  const task = timeoutFixture(t);
  task.allowRecovery(false);
  const run = task.store.run();
  let resumes = 0;
  task.trace.resumeCapture = async () => {
    resumes++;
  };
  try {
    await until(() =>
      task.store.logs.some((e) => e.message.includes("自动恢复次数已用尽")),
    );
    Object.defineProperty(task.trace, "currentHistoryId", {
      configurable: true,
      value: "foreign",
    });
    task.allowRecovery(true);
    await assert.rejects(task.store.resume(), /当前提取记录已变化/);
    assert.equal(resumes, 0);
    assert.equal(task.store.runState, "paused");
    await task.store.endTask();
    await run;
    assert.equal(task.store.courses[0]?.pendingCapture?.id, "capture-1");
    assert.equal(task.stops(), 0);
  } finally {
    await task.store.endTask();
    await run;
    task.restore();
  }
});

test("manual completion of one course cannot suppress emergency pause in the next course", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  task.store.selectedCourses.push({ id: "two", name: "第二讲" });
  task.store.config.playbackRate = 1.5;
  let pauses = 0;
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "20" },
      { role: "AXStaticText", identifier: "_NS:8", value: "00:00:12/00:01:00" },
    ],
  });
  playerAutomation.setPlaybackRate = async () => {
    if (task.counts().clicks === 2) throw new Error("second rate failure");
  };
  playerAutomation.togglePlayback = async () => {
    pauses++;
  };
  const run = task.store.run();
  try {
    await until(() => task.store.currentStep === "waitingCompletion");
    task.store.markPlaybackComplete();
    await run;
    assert.equal(task.store.processedCount, 1);
    assert.equal(pauses, 2);
    assert.deepEqual(task.counts(), { clicks: 2, captures: 2 });
  } finally {
    await task.store.endTask();
    await run;
    task.restore();
  }
});

test("an app restart turns its in-flight owned capture into a partial-save checkpoint", async () => {
  const task = queueFixture();
  task.store.courses = [
    {
      id: "one",
      name: "第一讲",
      state: "running",
      captureId: "owned",
      playbackAttempted: true,
      playback: { seconds: 12, totalSeconds: 60, percent: 20 },
    },
  ];
  task.store.runState = "running";
  task.store.updateConfig();
  let restoredTrace: ReturnType<typeof useTraceStore> | undefined;
  try {
    setActivePinia(createPinia());
    restoredTrace = useTraceStore();
    restoredTrace.historyReady = true;
    Object.defineProperty(restoredTrace, "currentHistoryId", {
      configurable: true,
      value: "owned",
    });
    let stops = 0;
    restoredTrace.stopCapture = async () => {
      stops++;
      return { id: "owned", name: "draft" };
    };
    restoredTrace.renameHistory = () => {};
    restoredTrace.flushKeyHistory = async () => {};
    restoredTrace.completeCapture = () => {};
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    assert.equal(restored.courses[0]?.pendingCapture?.playbackCompleted, false);
    assert.equal(restored.courses[0]?.pendingCapture?.id, "owned");
    await restored.resume();
    assert.equal(restored.runState, "ended");
    assert.equal(restored.processedCount, 0);
    assert.equal(stops, 1);
    assert.deepEqual(task.counts(), { clicks: 0, captures: 0 });
  } finally {
    restoredTrace?.dispose();
    task.restore();
  }
});

test("a detached partial record can be saved without stopping another live capture", async () => {
  const task = queueFixture();
  task.setSaveFailure(false);
  task.store.courses = [
    {
      id: "one",
      name: "第一讲",
      state: "error",
      playbackAttempted: true,
      pendingCapture: {
        id: "owned",
        name: "第一讲（中断）",
        needsStop: true,
        playbackCompleted: false,
      },
    },
  ];
  task.store.runState = "paused";
  task.trace.keyHistory = [{ id: "owned", name: "draft" } as never];
  task.trace.captureEnabled = true;
  Object.defineProperty(task.trace, "currentHistoryId", {
    configurable: true,
    value: "foreign",
  });
  let stops = 0,
    saved = "";
  task.trace.stopCapture = async () => {
    stops++;
    throw new Error("foreign stop");
  };
  task.trace.renameHistory = (id) => {
    saved = id;
  };
  try {
    await task.store.resume();
    assert.equal(task.store.runState, "ended");
    assert.equal(saved, "owned");
    assert.equal(stops, 0);
    assert.equal(task.store.processedCount, 0);
    assert.equal(task.trace.captureEnabled, true);
  } finally {
    task.restore();
  }
});

test("ending a task whose detached record was deleted reports loss without trapping the queue", async () => {
  const task = queueFixture();
  task.store.courses = [
    {
      id: "one",
      name: "第一讲",
      state: "error",
      captureId: "deleted",
      playbackAttempted: true,
      pendingCapture: {
        id: "deleted",
        name: "第一讲（中断）",
        needsStop: true,
        playbackCompleted: false,
      },
    },
  ];
  task.store.runState = "paused";
  task.trace.keyHistory = [];
  Object.defineProperty(task.trace, "currentHistoryId", {
    configurable: true,
    value: "foreign",
  });
  task.trace.flushKeyHistory = async () => {};
  try {
    await task.store.endTask();
    assert.equal(task.store.runState, "ended");
    assert.equal(task.store.courses[0]?.pendingCapture, undefined);
    assert.equal(task.store.processedCount, 0);
    assert.match(task.store.courses[0]!.detail!, /记录.*不存在.*未保存/);
    assert.equal(
      task.store.logs.some((e) => e.message === "中断记录已保存"),
      false,
    );
    task.store.resetQueue();
    assert.equal(task.store.runState, "idle");
  } finally {
    task.restore();
  }
});
