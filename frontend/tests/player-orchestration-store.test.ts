import test from "node:test";
import assert from "node:assert/strict";
import { createPinia, setActivePinia } from "pinia";
import { playerAutomation } from "../src/lib/player-automation";
import { usePlayerOrchestrationStore } from "../src/stores/player-orchestration";
import { useTraceStore } from "../src/stores/trace";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";
import { setNotificationEnabled } from "../src/lib/notifications";

test("course discovery selects all once and preserves later manual exclusions", async () => {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const originalAutomation = { ...playerAutomation };
  globalThis.localStorage = memoryStorage();
  Object.assign(playerAutomation, {
    revealCourse: async () => playerAutomation.snapshot(),
    courseSnapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, value: "第一讲" },
        { role: "AXRow", depth: 2, level: 0, value: "第二讲" },
      ],
    }),
  });
  try {
    setActivePinia(createPinia());
    const store = usePlayerOrchestrationStore();
    store.hydrate();
    await store.refreshCourses();
    assert.deepEqual(
      store.selectedCourses.map((course) => course.name),
      ["第一讲", "第二讲"],
    );

    store.setCourseSelected(store.availableCourses[1]!, false);
    await store.refreshCourses();
    assert.deepEqual(
      store.selectedCourses.map((course) => course.name),
      ["第一讲"],
    );
    store.setCourseSelected(store.availableCourses[0]!, false);
    await store.refreshCourses();
    assert.equal(store.selectedCourses.length, 0);

    setActivePinia(createPinia());
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    await restored.refreshCourses();
    assert.equal(restored.selectedCourses.length, 0);
  } finally {
    Object.assign(playerAutomation, originalAutomation);
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("checkbox selection survives refresh and follows catalog order", async () => {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const originalAutomation = { ...playerAutomation };
  globalThis.localStorage = memoryStorage();
  Object.assign(playerAutomation, {
    revealCourse: async () => playerAutomation.snapshot(),
    courseSnapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, value: "一", canExpand: true },
        { role: "AXRow", depth: 3, level: 1, value: "第一讲" },
        { role: "AXRow", depth: 3, level: 1, value: "第二讲" },
        { role: "AXRow", depth: 2, level: 0, value: "二", canExpand: true },
        { role: "AXRow", depth: 3, level: 1, value: "第三讲" },
      ],
    }),
  });
  try {
    setActivePinia(createPinia());
    const store = usePlayerOrchestrationStore();
    store.hydrate();
    await store.refreshCourses();
    assert.deepEqual(
      store.selectedCourses.map((course) => course.name),
      ["一 / 第一讲", "一 / 第二讲", "二 / 第三讲"],
    );

    store.setCoursesSelected(store.availableCourses.slice(1), false);
    assert.deepEqual(
      store.selectedCourses.map((course) => course.name),
      ["一 / 第一讲"],
    );
    store.setCourseSelected(store.availableCourses[0]!, false);
    store.setCourseSelected(store.availableCourses[2]!, true);
    await store.refreshCourses();
    assert.deepEqual(
      store.selectedCourses.map((course) => course.name),
      ["二 / 第三讲"],
    );

    setActivePinia(createPinia());
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    await restored.refreshCourses();
    assert.deepEqual(
      restored.selectedCourses.map((course) => course.name),
      ["二 / 第三讲"],
    );

    restored.setCoursesSelected(
      [restored.availableCourses[1]!, restored.availableCourses[0]!],
      true,
    );
    assert.deepEqual(
      restored.selectedCourses.map((course) => course.name),
      ["一 / 第一讲", "一 / 第二讲", "二 / 第三讲"],
    );
    restored.setCoursesSelected(restored.availableCourses.slice(0, 2), false);
    assert.deepEqual(
      restored.selectedCourses.map((course) => course.name),
      ["二 / 第三讲"],
    );
  } finally {
    Object.assign(playerAutomation, originalAutomation);
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("unsupported configuration fields are discarded while speed persists safely", () => {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  globalThis.localStorage = memoryStorage();
  try {
    setActivePinia(createPinia());
    const first = usePlayerOrchestrationStore();
    first.hydrate();
    first.config.playbackRate = 1.5;
    first.updateConfig();

    setActivePinia(createPinia());
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    assert.equal("enterFullscreen" in restored.config, false);
    assert.equal(restored.config.playbackRate, 1.5);

    const key = "szjm.player-orchestration.v1";
    const saved = JSON.parse(localStorage.getItem(key)!);
    saved.config.playbackRate = 2.05;
    saved.config.enterFullscreen = true;
    localStorage.setItem(key, JSON.stringify(saved));
    setActivePinia(createPinia());
    const invalid = usePlayerOrchestrationStore();
    invalid.hydrate();
    assert.equal("enterFullscreen" in invalid.config, false);
    assert.equal(invalid.config.playbackRate, null);
  } finally {
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("saved preferences validate values without starting playback", () => {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  globalThis.localStorage = memoryStorage();
  try {
    localStorage.setItem(
      "szjm.player-orchestration.v1",
      JSON.stringify({
        version: 10,
        config: {
          playbackVolumePercent: null,
          playbackRate: true,
          actionDelayMs: true,
          courseReadyTimeoutSeconds: " ",
          pollIntervalMs: [],
          maxPlaybackMinutes: "30",
        },
        courseSelectionInitialized: true,
        selectedCourses: [{ id: "a", name: "第一讲" }],
      }),
    );
    setActivePinia(createPinia());
    const restored = usePlayerOrchestrationStore();
    restored.hydrate();
    assert.equal(restored.config.playbackVolumePercent, 35);
    assert.equal(restored.config.playbackRate, null);
    assert.equal(restored.config.actionDelayMs, 1500);
    assert.equal(restored.config.courseReadyTimeoutSeconds, 45);
    assert.equal(restored.config.pollIntervalMs, 500);
    assert.equal(restored.config.maxPlaybackMinutes, 30);
    assert.equal(restored.runState, "idle");
    assert.equal(restored.selectedCourses[0]?.state, "pending");
  } finally {
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("an unsupported storage schema leaves a blank queue and default preferences", () => {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  globalThis.localStorage = memoryStorage();
  try {
    for (const version of [1, 3, 8, 9, 11]) {
      localStorage.setItem(
        "szjm.player-orchestration.v1",
        JSON.stringify({
          version,
          config: { playbackRate: 2, actionDelayMs: 500 },
          selectedCourses: [{ id: "a", name: "第一讲" }],
          queue: {
            runState: "running",
            courses: [{ id: "a", name: "第一讲", state: "running" }],
          },
        }),
      );
      setActivePinia(createPinia());
      const restored = usePlayerOrchestrationStore();
      restored.hydrate();
      assert.equal(restored.config.playbackRate, null);
      assert.equal(restored.config.actionDelayMs, 1500);
      assert.deepEqual(restored.selectedCourses, []);
      assert.deepEqual(restored.courses, []);
      assert.equal(restored.runState, "idle");
    }
  } finally {
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

function pendingCourseClick(withNotifications = false) {
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const originalAutomation = { ...playerAutomation };
  globalThis.localStorage = memoryStorage();
  const native = withNotifications ? new FakeMyGo().install() : undefined;
  if (native)
    native.handler = async (method, ...args) =>
      method === "NotificationService.Show"
        ? undefined
        : native.defaultCall(method, ...args);
  setActivePinia(createPinia());
  const trace = useTraceStore();
  const store = usePlayerOrchestrationStore();
  let releaseClick!: () => void;
  let clickEntered!: () => void;
  const clickGate = new Promise<void>((resolve) => {
    releaseClick = resolve;
  });
  const clickStarted = new Promise<void>((resolve) => {
    clickEntered = resolve;
  });
  let toggles = 0;
  Object.assign(playerAutomation, {
    revealCourse: async () => playerAutomation.snapshot(),
    status: async () => ({
      platform: "darwin",
      installed: true,
      running: true,
      accessibilityReady: true,
      verifiedProfile: true,
      version: "26.06.54",
      message: "ready",
    }),
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        { role: "AXRow", depth: 2, value: "第一讲" },
        { role: "AXSlider", width: 624, value: "0" },
        {
          role: "AXStaticText",
          identifier: "_NS:8",
          value: "00:00:00/00:01:00",
        },
      ],
    }),
    playbackSnapshot: () => playerAutomation.snapshot(),
    click: async () => {
      clickEntered();
      await clickGate;
    },
    togglePlayback: async () => {
      toggles++;
    },
    setVolume: async () => {},
  });
  trace.historyReady = true;
  trace.connected = true;
  let fixtureCaptureId = "";
  Object.defineProperty(trace, "currentHistoryId", {
    configurable: true,
    get: () => fixtureCaptureId,
  });
  trace.startCapture = async () => {
    fixtureCaptureId = "capture-1";
    trace.captureEnabled = true;
  };
  trace.pauseCapture = async () => {
    trace.captureEnabled = false;
  };
  trace.resumeCapture = async () => {
    trace.captureEnabled = true;
  };
  trace.stopCapture = async () => {
    trace.captureEnabled = false;
    return { id: "capture-1", name: "first" };
  };
  trace.renameHistory = () => {};
  trace.flushKeyHistory = async () => {};
  trace.completeCapture = () => {};
  store.status = {
    platform: "darwin",
    installed: true,
    running: true,
    accessibilityReady: true,
    verifiedProfile: true,
    version: "26.06.54",
    message: "ready",
  };
  store.selectedCourses = [{ id: "course-1", name: "第一讲" }];
  store.availableCourses = store.selectedCourses.slice();
  store.courseCatalogReady = true;
  store.config.actionDelayMs = 1;
  store.config.pollIntervalMs = 1;
  store.config.maxPlaybackMinutes = 1;
  const run = store.run();
  return {
    store,
    native,
    clickStarted,
    releaseClick,
    run,
    toggleCount: () => toggles,
    cleanup: async () => {
      releaseClick();
      if (store.runState === "running" || store.runState === "paused")
        await store.endTask();
      await run;
      Object.assign(playerAutomation, originalAutomation);
      trace.dispose();
      native?.restore();
      if (originalStorage)
        Object.defineProperty(globalThis, "localStorage", originalStorage);
      else Reflect.deleteProperty(globalThis, "localStorage");
    },
  };
}

test("pausing while a native course click is pending pauses playback after delivery", async () => {
  const task = pendingCourseClick();
  try {
    await task.clickStarted;
    assert.equal(task.store.currentStep, "selectingCourse");
    await task.store.pause();
    assert.equal(task.store.runState, "paused");
    task.releaseClick();
    for (let attempt = 0; attempt < 20 && task.toggleCount() === 0; attempt++)
      await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(task.toggleCount(), 1);
    await task.store.resume();
    assert.equal(task.store.runState, "running");
    assert.equal(task.toggleCount(), 2);
  } finally {
    await task.cleanup();
  }
});

test("manual continuation holds a later history prompt without choosing restart", async () => {
  const task = pendingCourseClick();
  let showPrompt = false,
    negativePresses = 0;
  const originalClick = playerAutomation.click;
  playerAutomation.click = async (...args) => {
    if (args[0] === "否") negativePresses++;
    else await originalClick(...args);
  };
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "10" },
      {
        role: "AXStaticText",
        identifier: "_NS:8",
        value: "00:00:06/00:01:00",
      },
      ...(showPrompt
        ? [
            { role: "AXStaticText", value: "是否继续上次播放的视频位置" },
            { role: "AXButton", title: "否" },
            { role: "AXButton", title: "是" },
          ]
        : []),
    ],
  });
  try {
    await task.clickStarted;
    task.releaseClick();
    for (
      let attempt = 0;
      attempt < 40 &&
      !task.store.logs.some((entry) => entry.message === "已确认播放启动");
      attempt++
    )
      await new Promise((resolve) => setTimeout(resolve, 5));
    assert.ok(
      task.store.logs.some((entry) => entry.message === "已确认播放启动"),
    );
    await task.store.pause();
    await task.store.resume();
    assert.equal(task.store.runState, "running");
    showPrompt = true;
    for (
      let attempt = 0;
      attempt < 40 &&
      !task.store.logs.some((entry) => entry.detail?.includes("续播过程中"));
      attempt++
    )
      await new Promise((resolve) => setTimeout(resolve, 5));
    assert.ok(
      task.store.logs.some((entry) => entry.detail?.includes("续播过程中")),
    );
    assert.equal(negativePresses, 0);
  } finally {
    await task.cleanup();
  }
});

test("a late pause acknowledgement cannot turn a completed queue back into paused", async () => {
  const task = pendingCourseClick();
  const trace = useTraceStore();
  let releaseStop!: () => void, enteredStop!: () => void;
  let releasePause!: () => void, enteredPause!: () => void;
  const stopGate = new Promise<void>((resolve) => {
    releaseStop = resolve;
  });
  const stopEntered = new Promise<void>((resolve) => {
    enteredStop = resolve;
  });
  const pauseGate = new Promise<void>((resolve) => {
    releasePause = resolve;
  });
  const pauseEntered = new Promise<void>((resolve) => {
    enteredPause = resolve;
  });
  let pause: Promise<void> | undefined;
  try {
    await task.clickStarted;
    let reads = 0;
    playerAutomation.snapshot = async () => ({
      running: true,
      at: "",
      elements: [
        {
          role: "AXSlider",
          identifier: "_NS:26",
          value: ++reads === 1 ? "25" : "100",
        },
        {
          role: "AXStaticText",
          identifier: "_NS:8",
          value: reads === 1 ? "00:00:01/00:00:04" : "00:00:04/00:00:04",
        },
      ],
    });
    trace.stopCapture = async () => {
      enteredStop();
      await stopGate;
      trace.captureEnabled = false;
      return { id: "capture-1", name: "first" };
    };
    trace.pauseCapture = async () => {
      enteredPause();
      await pauseGate;
      trace.captureEnabled = false;
    };
    task.releaseClick();
    await stopEntered;
    pause = task.store.pause();
    await pauseEntered;
    releaseStop();
    await task.run;
    assert.equal(task.store.runState, "completed");
    releasePause();
    await pause;
    assert.equal(task.store.runState, "completed");
    assert.equal(task.store.progress, 100);
    const courseLogs = task.store.logs.filter(
      (entry) => entry.courseId === task.store.courses[0]!.id,
    );
    for (const message of [
      "开始打开课程",
      "已确认播放启动",
      "正在结束提取",
      "开始保存记录",
      "本课程执行完成",
    ]) {
      assert.ok(
        courseLogs.some(
          (entry) => entry.level === "info" && entry.message === message,
        ),
        message,
      );
    }
    assert.ok(
      task.store.logs.every((entry) => Number.isFinite(Date.parse(entry.at))),
    );
    assert.equal(task.toggleCount(), 0);
  } finally {
    releaseStop();
    releasePause();
    await pause;
    await task.cleanup();
  }
});

for (const pauseFirst of [false, true]) {
  test(`ending while a native click is pending pauses delivered playback${pauseFirst ? " after a task pause" : ""}`, async () => {
    const task = pendingCourseClick();
    try {
      await task.clickStarted;
      if (pauseFirst) await task.store.pause();
      const ended = task.store.endTask();
      task.releaseClick();
      await ended;
      assert.equal(task.store.runState, "ended");
      assert.equal(task.toggleCount(), 1);
    } finally {
      await task.cleanup();
    }
  });
}

for (const failBeforeCompletion of [false, true]) {
  test(`queue automatically ends after all videos and saves${failBeforeCompletion ? " after retry" : " without manual intervention"}`, async () => {
    const originalStorage = Object.getOwnPropertyDescriptor(
      globalThis,
      "localStorage",
    );
    const originalAutomation = { ...playerAutomation };
    globalThis.localStorage = memoryStorage();
    const native = new FakeMyGo().install();
    native.handler = async (method, ...args) =>
      method === "NotificationService.Show"
        ? undefined
        : native.defaultCall(method, ...args);
    setActivePinia(createPinia());
    const trace = useTraceStore();
    const store = usePlayerOrchestrationStore();
    let playing = false;
    let playingReads = 0;
    let failSecond = failBeforeCompletion;
    let toggles = 0;
    const played: string[] = [];
    let captureNumber = 0;
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
      revealCourse: async (path: string[]) => {
        if (path.at(-1) === "第二讲" && failSecond) {
          failSecond = false;
          throw new Error("课程没有启动");
        }
        return playerAutomation.snapshot();
      },
      status: async () => status,
      snapshot: async () => ({
        running: true,
        at: "",
        elements: [
          { role: "AXOutline" },
          { role: "AXRow", depth: 2, value: "第一讲" },
          { role: "AXRow", depth: 2, value: "第二讲" },
          { role: "AXSlider", width: 624, value: playing ? "100" : "0" },
          ...(playing
            ? [{ role: "AXStaticText", value: "00:01:00/00:01:00" }]
            : []),
        ],
      }),
      playbackSnapshot: async () => {
        const current = await playerAutomation.snapshot();
        if (playing && ++playingReads === 1) {
          current.elements = current.elements.map((element) =>
            element.role === "AXSlider"
              ? { ...element, value: "20" }
              : element.role === "AXStaticText"
                ? { ...element, value: "00:00:12/00:01:00" }
                : element,
          );
        }
        return current;
      },
      click: async (text: string) => {
        played.push(text);
        playing = true;
        playingReads = 0;
      },
      setVolume: async () => {},
      togglePlayback: async () => {
        toggles++;
      },
    });
    trace.historyReady = true;
    trace.connected = true;
    let fixtureCaptureId = "";
    Object.defineProperty(trace, "currentHistoryId", {
      configurable: true,
      get: () => fixtureCaptureId,
    });
    trace.startCapture = async () => {
      fixtureCaptureId = `capture-${captureNumber + 1}`;
      trace.captureEnabled = true;
    };
    trace.stopCapture = async () => {
      trace.captureEnabled = false;
      playing = false;
      return { id: `capture-${++captureNumber}`, name: "saved" };
    };
    trace.renameHistory = () => {};
    trace.flushKeyHistory = async () => {};
    trace.completeCapture = () => {};
    store.status = status;
    store.selectedCourses = [
      { id: "first", name: "第一讲" },
      { id: "second", name: "第二讲" },
    ];
    store.availableCourses = store.selectedCourses.slice();
    store.courseCatalogReady = true;
    store.config.actionDelayMs = 1;
    store.config.pollIntervalMs = 1;
    store.config.maxPlaybackMinutes = 1;
    setNotificationEnabled("player.courseSaved", true);
    try {
      await store.run();
      if (failBeforeCompletion) {
        assert.equal(store.runState, "paused");
        assert.equal(store.currentIndex, 1);
        const failure = store.logs.findLast((entry) => entry.level === "error");
        assert.equal(failure?.step, "waitingCourse");
        assert.equal(failure?.courseId, store.courses[1]!.id);
        assert.deepEqual(
          native.calls
            .filter((call) => call.method === "NotificationService.Show")
            .map((call) => call.args[0]),
          ["播放编排已开始", "单课密钥已保存", "播放编排需要处理"],
        );

        await store.resume();
      }
      assert.equal(store.runState, "completed");
      assert.equal(store.currentStep, undefined);
      assert.equal(store.activeCourse, undefined);
      assert.equal(store.progress, 100);
      assert.equal(trace.captureEnabled, false);
      assert.equal(store.canStart, false);
      assert.ok(
        store.courses.every(
          (course) => course.state === "completed" && course.captureId,
        ),
      );
      assert.ok(
        store.logs.some(
          (log) => log.message === "任务已完成，全部课程已播放并保存",
        ),
      );
      assert.deepEqual(played, ["第一讲", "第二讲"]);
      await store.run();
      await store.resume();
      await store.endTask();
      assert.equal(store.runState, "completed");
      assert.equal(toggles, 0, "finishing must not restart the last video");
      assert.deepEqual(played, ["第一讲", "第二讲"]);
      assert.deepEqual(
        native.calls
          .filter((call) => call.method === "NotificationService.Show")
          .map((call) => call.args[0]),
        [
          "播放编排已开始",
          "单课密钥已保存",
          ...(failBeforeCompletion
            ? ["播放编排需要处理", "播放编排已继续"]
            : []),
          "播放编排任务完成",
        ],
      );
    } finally {
      Object.assign(playerAutomation, originalAutomation);
      trace.dispose();
      native.restore();
      if (originalStorage)
        Object.defineProperty(globalThis, "localStorage", originalStorage);
      else Reflect.deleteProperty(globalThis, "localStorage");
    }
  });
}

test("manual pause and end notify once; failed interrupted save asks for attention", async () => {
  const task = pendingCourseClick(true);
  try {
    await task.clickStarted;
    await task.store.pause();
    task.releaseClick();
    for (let attempt = 0; attempt < 20 && task.toggleCount() === 0; attempt++)
      await new Promise((resolve) => setTimeout(resolve, 5));
    await task.store.resume();
    await task.store.endTask();
    assert.deepEqual(
      task.native?.calls.map((call) => call.args[0]),
      ["播放编排已开始", "播放编排已暂停", "播放编排已继续", "播放编排已结束"],
    );
  } finally {
    await task.cleanup();
  }

  const failed = pendingCourseClick(true);
  try {
    await failed.clickStarted;
    const trace = useTraceStore();
    trace.stopCapture = async () => {
      throw new Error("记录未保存");
    };
    const ended = failed.store.endTask();
    failed.releaseClick();
    await ended;
    assert.deepEqual(
      failed.native?.calls.map((call) => call.args[0]),
      ["播放编排已开始", "播放编排需要处理"],
    );
  } finally {
    await failed.cleanup();
  }
});

test("a second start during asynchronous preflight cannot start another course", async () => {
  const task = pendingCourseClick();
  const originalStatus = playerAutomation.status;
  let duplicateStatusCalls = 0;
  playerAutomation.status = async () => {
    duplicateStatusCalls++;
    return originalStatus();
  };
  try {
    await task.store.run();
    assert.equal(duplicateStatusCalls, 0);
    await task.clickStarted;
    assert.equal(
      task.store.courses.filter((course) => course.state === "running").length,
      1,
    );
  } finally {
    await task.cleanup();
  }
});

for (const percent of [100, 99.8]) {
  test(`ending a course at its terminal clock (${percent}%) never toggles it into replay`, async () => {
    const task = pendingCourseClick();
    try {
      await task.clickStarted;
      task.store.courses[0]!.playback = {
        percent,
        seconds: 4,
        totalSeconds: 4,
      };
      playerAutomation.playbackSnapshot = async () => ({
        running: true,
        at: "",
        elements: [
          { role: "AXSlider", identifier: "_NS:26", value: String(percent) },
          {
            role: "AXStaticText",
            identifier: "_NS:8",
            value: "00:00:04/00:00:04",
          },
        ],
      });
      const ended = task.store.endTask();
      task.releaseClick();
      await ended;
      assert.equal(task.toggleCount(), 0);
      assert.equal(task.store.runState, "ended");
    } finally {
      await task.cleanup();
    }
  });
}

test("persisted checkbox selection remains editable after reopening", () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  globalThis.localStorage = memoryStorage();
  try {
    localStorage.setItem(
      "szjm.player-orchestration.v1",
      JSON.stringify({
        version: 10,
        courseSelectionInitialized: true,
        selectedCourses: [
          { id: "a", name: "一" },
          { id: "b", name: "二" },
        ],
      }),
    );
    setActivePinia(createPinia());
    const store = usePlayerOrchestrationStore();
    store.hydrate();
    assert.deepEqual(
      store.selectedCourses.map((course) => course.id),
      ["a", "b"],
    );
    store.availableCourses = [
      ...store.selectedCourses,
      { id: "c", name: "三" },
    ];
    store.courseCatalogReady = true;
    store.setCourseSelected({ id: "c", name: "三" }, true);
    assert.deepEqual(
      store.selectedCourses.map((course) => course.id),
      ["a", "b", "c"],
    );
  } finally {
    if (previous) Object.defineProperty(globalThis, "localStorage", previous);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("ending blocks configuration while the native pause is still pending", async () => {
  const task = pendingCourseClick();
  let releasePause!: () => void;
  const pauseGate = new Promise<void>((resolve) => {
    releasePause = resolve;
  });
  let pauseEntered!: () => void;
  const pauseStarted = new Promise<void>((resolve) => {
    pauseEntered = resolve;
  });
  let configurations = 0;
  playerAutomation.togglePlayback = async () => {
    pauseEntered();
    await pauseGate;
  };
  playerAutomation.setVolume = async () => {
    configurations++;
  };
  try {
    await task.clickStarted;
    const ended = task.store.endTask();
    task.releaseClick();
    await pauseStarted;
    await new Promise((resolve) => setTimeout(resolve, 40));
    assert.equal(
      configurations,
      0,
      "stop request must block subsequent player operations",
    );
    assert.equal(
      task.store.canStart,
      false,
      "native stop must retain the start lock",
    );
    await task.store.run();
    assert.equal(configurations, 0);
    releasePause();
    await ended;
    assert.equal(task.store.runState, "ended");
  } finally {
    releasePause();
    await task.cleanup();
  }
});

test("a capture pause failure never rolls the player forward with another toggle", async () => {
  const task = pendingCourseClick();
  let configured!: () => void;
  const configuring = new Promise<void>((resolve) => {
    configured = resolve;
  });
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  playerAutomation.setVolume = async () => {
    configured();
    await gate;
  };
  useTraceStore().pauseCapture = async () => {
    throw new Error("capture pause failed");
  };
  try {
    await task.clickStarted;
    task.releaseClick();
    await configuring;
    await assert.rejects(task.store.pause(), /capture pause failed/);
    assert.equal(
      task.toggleCount(),
      1,
      "failed pause must not resume playback as rollback",
    );
    release();
    await task.run;
    assert.notEqual(task.store.runState, "running");
  } finally {
    release();
    await task.cleanup();
  }
});

test("a hidden control bar during user pause keeps the run alive for retry", async () => {
  const task = pendingCourseClick();
  let attempts = 0;
  playerAutomation.togglePlayback = async () => {
    attempts++;
    if (attempts === 1)
      throw new Error(
        "SzPlayer 未暴露播放控制栏，请恢复播放器窗口并显示控制栏后再操作",
      );
  };
  try {
    await task.clickStarted;
    task.releaseClick();
    for (
      let attempt = 0;
      attempt < 40 &&
      task.store.currentStep !== "startingPlayback" &&
      task.store.currentStep !== "configuringPlayback" &&
      task.store.currentStep !== "waitingCompletion";
      attempt++
    )
      await new Promise((resolve) => setTimeout(resolve, 5));
    await assert.rejects(task.store.pause(), /未暴露播放控制栏/);
    assert.equal(task.store.runState, "running");
    assert.ok(
      task.store.logs.some(
        (entry) =>
          entry.level === "error" &&
          entry.message === "暂停未完成，任务继续运行",
      ),
    );
    await task.store.pause();
    assert.equal(task.store.runState, "paused");
    assert.equal(attempts, 2);
  } finally {
    await task.cleanup();
  }
});

test("fullscreen pause with a hidden control bar exits fullscreen and retries once", async () => {
  const task = pendingCourseClick();
  let toggles = 0;
  let fullscreen = true;
  playerAutomation.playbackSnapshot = async () => ({
    running: true,
    at: "",
    windowMode: fullscreen ? "fullscreen" : "windowed",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: "20" },
      {
        role: "AXStaticText",
        identifier: "_NS:8",
        value: "00:00:12/00:01:00",
      },
    ],
  });
  playerAutomation.setFullscreen = async (enabled) => {
    assert.equal(enabled, false);
    fullscreen = false;
  };
  playerAutomation.togglePlayback = async () => {
    toggles++;
    if (fullscreen)
      throw new Error(
        "SzPlayer 未暴露播放控制栏，请恢复播放器窗口并显示控制栏后再操作",
      );
  };
  try {
    await task.clickStarted;
    task.releaseClick();
    for (
      let attempt = 0;
      attempt < 40 &&
      task.store.currentStep !== "startingPlayback" &&
      task.store.currentStep !== "configuringPlayback" &&
      task.store.currentStep !== "waitingCompletion";
      attempt++
    )
      await new Promise((resolve) => setTimeout(resolve, 5));
    await task.store.pause();
    assert.equal(task.store.runState, "paused");
    assert.equal(toggles, 2);
    assert.equal(fullscreen, false);
  } finally {
    await task.cleanup();
  }
});

test("an uncertain pause still stops the run instead of retrying the toggle", async () => {
  const task = pendingCourseClick();
  playerAutomation.togglePlayback = async () => {
    throw new Error(
      "SzPlayer 暂停或继续的结果不确定，已停止重复点击，请确认播放器状态",
    );
  };
  try {
    await task.clickStarted;
    task.releaseClick();
    for (
      let attempt = 0;
      attempt < 40 &&
      task.store.currentStep !== "startingPlayback" &&
      task.store.currentStep !== "configuringPlayback" &&
      task.store.currentStep !== "waitingCompletion";
      attempt++
    )
      await new Promise((resolve) => setTimeout(resolve, 5));
    await assert.rejects(task.store.pause(), /结果不确定/);
    assert.equal(task.store.runState, "ended");
    assert.ok(
      task.store.logs.some(
        (entry) =>
          entry.level === "error" && entry.message === "暂停失败，任务已停止",
      ),
    );
  } finally {
    await task.cleanup();
  }
});

test("a failed resume closes the active run instead of accepting a second toggle", async () => {
  const task = pendingCourseClick();
  try {
    await task.clickStarted;
    await task.store.pause();
    task.releaseClick();
    for (let attempt = 0; attempt < 20 && task.toggleCount() === 0; attempt++)
      await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(task.toggleCount(), 1);
    let uncertainToggles = 0;
    playerAutomation.togglePlayback = async () => {
      uncertainToggles++;
      throw new Error("uncertain press");
    };
    await assert.rejects(task.store.resume(), /uncertain press/);
    await task.store.resume().catch(() => {});
    assert.equal(uncertainToggles, 1);
  } finally {
    await task.cleanup();
  }
});

test("refreshing a catalog cannot erase a completed execution queue", async () => {
  setActivePinia(createPinia());
  const store = usePlayerOrchestrationStore();
  const original = playerAutomation.courseSnapshot;
  playerAutomation.courseSnapshot = async () => ({
    running: true,
    at: "",
    elements: [{ role: "AXOutline" }, { role: "AXRow", value: "第一讲" }],
  });
  try {
    store.courses = [
      {
        id: "0:第一讲",
        name: "第一讲",
        state: "completed",
        captureId: "saved",
      },
    ];
    store.runState = "completed";
    store.currentIndex = 1;
    await store.refreshCourses();
    assert.equal(store.runState, "completed");
    assert.equal(store.courses[0]?.captureId, "saved");
    assert.equal(store.currentIndex, 1);
  } finally {
    playerAutomation.courseSnapshot = original;
  }
});

test("a previous course's cached EOF cannot suppress pausing the current video", async () => {
  const task = pendingCourseClick();
  try {
    await task.clickStarted;
    task.store.courses[0]!.playback = {
      seconds: 60,
      totalSeconds: 60,
      percent: 100,
    };
    const ended = task.store.endTask();
    task.releaseClick();
    await ended;
    assert.equal(task.toggleCount(), 1);
    assert.equal(task.store.runState, "ended");
  } finally {
    await task.cleanup();
  }
});

test("quit waits for an in-flight course click, pauses it and archives before acknowledgement", async () => {
  const task = pendingCourseClick();
  try {
    await task.clickStarted;
    let ready = false;
    const quit = task.store.prepareQuit().then(() => {
      ready = true;
    });
    await new Promise((resolve) => setTimeout(resolve, 5));
    assert.equal(ready, false);
    task.releaseClick();
    await quit;
    assert.equal(task.toggleCount(), 1);
    assert.equal(task.store.runState, "ended");
    assert.equal(
      task.store.courses.some((course) => course.pendingCapture),
      false,
    );
  } finally {
    await task.cleanup();
  }
});

test("quit rejects a failed player pause instead of acknowledging safe exit", async () => {
  const task = pendingCourseClick();
  try {
    await task.clickStarted;
    playerAutomation.togglePlayback = async () => {
      throw new Error("pause unavailable");
    };
    const quit = task.store.prepareQuit();
    task.releaseClick();
    await assert.rejects(quit, /暂停或任务收尾未能确认/);
  } finally {
    await task.cleanup();
  }
});

test("quit rejects an archive failure and retains the pending capture", async () => {
  const task = pendingCourseClick();
  const trace = useTraceStore();
  try {
    await task.clickStarted;
    trace.flushKeyHistory = async () => {
      throw new Error("disk full");
    };
    const quit = task.store.prepareQuit();
    task.releaseClick();
    await assert.rejects(quit, /等待保存/);
    assert.ok(task.store.courses[0]?.pendingCapture);
  } finally {
    trace.flushKeyHistory = async () => {};
    await task.cleanup();
  }
});

test("catalog refresh clears stale rows and selection on empty or failed reads and across restart", async () => {
  const previousStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const original = playerAutomation.courseSnapshot;
  globalThis.localStorage = memoryStorage();
  let mode = "full";
  playerAutomation.courseSnapshot = async () => {
    if (mode === "error") throw new Error("权限不足");
    return {
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        ...(mode === "full" ? [{ role: "AXRow", value: "第一讲" }] : []),
      ],
    };
  };
  try {
    for (const outcome of ["empty", "error"]) {
      setActivePinia(createPinia());
      const store = usePlayerOrchestrationStore();
      mode = "full";
      await store.refreshCourses();
      store.setCoursesSelected(store.availableCourses, true);
      assert.equal(store.selectedCourses.length, 1);
      mode = outcome;
      if (outcome === "error")
        await assert.rejects(store.refreshCourses(), /权限不足/);
      else await store.refreshCourses();
      assert.deepEqual(store.availableCourses, []);
      assert.deepEqual(store.selectedCourses, []);
      assert.equal(store.canStart, false);
      assert.equal(store.courseError, outcome === "error" ? "权限不足" : "");
      setActivePinia(createPinia());
      const restored = usePlayerOrchestrationStore();
      restored.hydrate();
      assert.deepEqual(restored.availableCourses, []);
      assert.deepEqual(restored.selectedCourses, []);
    }
  } finally {
    playerAutomation.courseSnapshot = original;
    if (previousStorage)
      Object.defineProperty(globalThis, "localStorage", previousStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("cached courses require a fresh read and an in-flight refresh blocks duplicate reads and selection", async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const original = playerAutomation.courseSnapshot;
  globalThis.localStorage = memoryStorage();
  const catalog = {
    running: true,
    at: "",
    elements: [{ role: "AXOutline" }, { role: "AXRow", value: "第一讲" }],
  };
  playerAutomation.courseSnapshot = async () => catalog;
  try {
    setActivePinia(createPinia());
    const initial = usePlayerOrchestrationStore();
    await initial.refreshCourses();
    assert.equal(initial.canContinueSelection, true);
    setActivePinia(createPinia());
    const store = usePlayerOrchestrationStore();
    store.hydrate();
    assert.equal(store.selectedCourses.length, 1);
    assert.equal(store.canContinueSelection, false);
    await store.refreshCourses();
    assert.equal(store.canContinueSelection, true);
    const oldCourse = store.selectedCourses[0]!;
    let release!: () => void;
    let calls = 0;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    playerAutomation.courseSnapshot = async () => {
      calls++;
      await gate;
      return catalog;
    };
    const refresh = store.refreshCourses();
    assert.equal(store.courseLoading, true);
    assert.equal(store.canContinueSelection, false);
    assert.equal(store.canStart, false);
    assert.deepEqual(store.availableCourses, []);
    store.setCourseSelected(oldCourse, true);
    assert.deepEqual(store.selectedCourses, []);
    await store.refreshCourses();
    assert.equal(calls, 1);
    release();
    await refresh;
    assert.equal(store.canContinueSelection, true);
    assert.equal(store.selectedCourses.length, 1);
  } finally {
    playerAutomation.courseSnapshot = original;
    if (previous) Object.defineProperty(globalThis, "localStorage", previous);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});

test("initial empty catalog can populate later and removed selections never return on retry", async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  const original = playerAutomation.courseSnapshot;
  globalThis.localStorage = memoryStorage();
  let names: string[] = [];
  playerAutomation.courseSnapshot = async () => ({
    running: true,
    at: "",
    elements: [
      { role: "AXOutline" },
      ...names.map((value) => ({ role: "AXRow", value })),
    ],
  });
  try {
    setActivePinia(createPinia());
    const store = usePlayerOrchestrationStore();
    await store.refreshCourses();
    assert.equal(store.courseError, "");
    names = ["第一讲", "第二讲"];
    await store.refreshCourses();
    assert.equal(store.selectedCourses.length, 2);
    store.setCourseSelected(store.availableCourses[1]!, false);
    names = ["第二讲"];
    await store.refreshCourses();
    assert.equal(store.selectedCourses.length, 0);
    assert.equal(store.canContinueSelection, false);
    store.setCourseSelected({ id: "foreign", name: "已删除的课程" }, true);
    assert.equal(store.selectedCourses.length, 0);
    playerAutomation.courseSnapshot = async () => {
      throw new Error("temporary read failure");
    };
    await assert.rejects(store.refreshCourses());
    playerAutomation.courseSnapshot = async () => ({
      running: true,
      at: "",
      elements: [{ role: "AXOutline" }, { role: "AXRow", value: "第二讲" }],
    });
    await store.refreshCourses();
    assert.equal(store.courseError, "");
    assert.equal(store.availableCourses.length, 1);
    assert.equal(store.selectedCourses.length, 0);
    store.setCourseSelected(store.availableCourses[0]!, true);
    assert.equal(store.canContinueSelection, true);
  } finally {
    playerAutomation.courseSnapshot = original;
    if (previous) Object.defineProperty(globalThis, "localStorage", previous);
    else Reflect.deleteProperty(globalThis, "localStorage");
  }
});
