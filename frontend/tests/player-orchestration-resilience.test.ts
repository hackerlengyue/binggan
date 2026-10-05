import test from "node:test";
import assert from "node:assert/strict";
import {
  OrchestrationInterrupted,
  completionReached,
  validatePlaybackRecovery,
  executeCourse,
  readPlaybackProgress,
  type OrchestrationDependencies,
  type OrchestrationConfig,
} from "../src/lib/player-orchestration";
import type { PlayerSnapshot } from "../src/lib/player-automation";

const config: OrchestrationConfig = {
  playbackVolumePercent: 35,
  playbackRate: null,
  actionDelayMs: 1,
  courseReadyTimeoutSeconds: 1,
  pollIntervalMs: 500,
  maxPlaybackMinutes: 5,
};
const course = { id: "lesson", name: "第一讲" };
function frame(
  percent = "25",
  clock = "00:00:01/00:00:04",
  extra: Partial<PlayerSnapshot> = {},
): PlayerSnapshot {
  return {
    running: true,
    at: "",
    pid: 10,
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value: percent },
      { role: "AXStaticText", identifier: "_NS:8", value: clock },
    ],
    ...extra,
  };
}
function historyPrompt(progress = frame()): PlayerSnapshot {
  return {
    ...progress,
    elements: [
      ...progress.elements,
      { role: "AXStaticText", value: "是否继续上次播放的视频位置" },
      { role: "AXButton", title: "否" },
      { role: "AXButton", title: "是" },
    ],
  };
}
const terminal = () => frame("100", "00:00:04/00:00:04");
function harness(
  samples: PlayerSnapshot[] = [frame(), terminal(), terminal(), terminal()],
) {
  let now = 0,
    reads = 0,
    capturing = false;
  const events: string[] = [],
    displayed: number[] = [];
  const controller = new AbortController();
  const deps: OrchestrationDependencies = {
    now: () => now,
    snapshot: async () => ({
      ...frame("0", "00:00:00/00:00:04"),
      elements: [
        { role: "AXOutline" },
        { role: "AXRow", depth: 2, value: course.name },
      ],
    }),
    playbackSnapshot: async () =>
      samples[Math.min(reads++, samples.length - 1)]!,
    courseSnapshot: async () => ({ running: true, at: "", elements: [] }),
    openCourse: async () => {
      events.push("open");
    },
    expandFolder: async () => {},
    revealCourse: async () => deps.snapshot(),
    setVolume: async () => {
      events.push("volume");
    },
    setFullscreen: async (value) => {
      events.push(`fullscreen:${value}`);
    },
    setPlaybackRate: async () => {},
    click: async () => {
      events.push("click");
    },
    startCapture: async () => {
      events.push("start");
      capturing = true;
    },
    captureActive: () => capturing,
    stopCapture: async () => {
      events.push("stop");
      capturing = false;
      return { id: "record", name: "record" };
    },
    saveCapture: async (_, name) => {
      events.push(`save:${name}`);
    },
    sleep: async (ms) => {
      now += ms;
    },
    manualComplete: () => false,
    onProgress: (progress) => {
      if (progress.seconds !== undefined) displayed.push(progress.seconds);
    },
    onStep: (step) => {
      events.push(`step:${step}`);
    },
    onLog: (_, message) => {
      events.push(`log:${message}`);
    },
  };
  return {
    deps,
    events,
    displayed,
    controller,
    reads: () => reads,
    advance: (ms: number) => {
      now += ms;
    },
    disableCapture: () => {
      capturing = false;
    },
    run: (overrides: Partial<OrchestrationConfig> = {}) =>
      executeCourse(
        deps,
        { ...config, ...overrides },
        course,
        controller.signal,
      ),
  };
}

test("completion rejects malformed values, invalid clocks, missing sliders and unrelated controls", () => {
  for (const percent of ["100oops", "101", "-1", "NaN", "Infinity", "", "100%"])
    assert.equal(
      completionReached(frame(percent, "00:00:04/00:00:04"), true),
      false,
      percent,
    );
  for (const clock of [
    "00:99:04/00:99:04",
    "00:00:05/00:00:04",
    "课程 00:00:04/00:00:04",
    "00:00:00/00:00:00",
  ])
    assert.equal(completionReached(frame("100", clock), true), false, clock);
  const missing = terminal();
  missing.elements.shift();
  assert.equal(completionReached(missing, true), false);
  missing.elements.unshift({ role: "AXProgressIndicator", value: "100" });
  assert.equal(completionReached(missing, true), false);
  assert.equal(completionReached(terminal(), true), true);
});

test("verified controls take priority and conflicting fallback controls remain ambiguous", () => {
  const current = frame();
  current.elements.unshift(
    { role: "AXSlider", width: 600, value: "100" },
    { role: "AXStaticText", value: "00:00:04/00:00:04" },
  );
  assert.deepEqual(readPlaybackProgress(current), {
    seconds: 1,
    totalSeconds: 4,
    percent: 25,
  });
  const ambiguous = terminal();
  ambiguous.elements[0]!.identifier = undefined;
  ambiguous.elements[0]!.width = 600;
  ambiguous.elements.unshift({ role: "AXSlider", width: 500, value: "25" });
  assert.equal(completionReached(ambiguous, true), false);
});

test("a stale terminal marker alone cannot confirm a new course started", async () => {
  const h = harness([terminal()]);
  await assert.rejects(h.run(), /未检测到播放启动/);
  assert.equal(h.events.includes("save:第一讲"), false);
  assert.equal(h.events.filter((e) => e === "click").length, 1);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("a different video's duration cannot complete the current course", async () => {
  const switched = frame("100", "00:00:08/00:00:08");
  const h = harness([frame(), switched, switched, switched]);
  await assert.rejects(h.run(), /播放总时长发生变化/);
  assert.ok(h.events.includes("save:第一讲（中断）"));
  assert.ok(!h.events.includes("save:第一讲"));
});

test("transient window failures preserve capture and last progress until recovery", async () => {
  const unavailable = frame("100", "00:00:04/00:00:04", {
    readIssue: { code: "fullscreen_transition", message: "全屏切换" },
  });
  const h = harness([
    frame(),
    unavailable,
    unavailable,
    frame("50", "00:00:02/00:00:04"),
    terminal(),
    terminal(),
    terminal(),
  ]);
  await h.run();
  assert.deepEqual(h.displayed, [1, 2, 4, 4, 4]);
  assert.equal(h.events.filter((e) => e === "start").length, 1);
  assert.equal(h.events.filter((e) => e === "stop").length, 1);
  assert.equal(
    h.events.filter((e) => e.startsWith("log:暂时无法读取")).length,
    1,
  );
  assert.ok(h.events.includes("log:播放进度读取已恢复"));
});

for (const code of [
  "fullscreen_transition",
  "window_not_visible",
  "window_minimized",
  "controls_unavailable",
])
  test(`persistent ${code} has a bounded recovery and saves interrupted capture`, async () => {
    const h = harness([
      frame(),
      frame("100", "00:00:04/00:00:04", { readIssue: { code, message: code } }),
    ]);
    await assert.rejects(h.run(), /连续 15 秒无法读取/);
    assert.ok(h.reads() < 40);
    assert.ok(h.events.includes("save:第一讲（中断）"));
  });

test("blocking dialogs take priority even while the window is unavailable", async () => {
  const blocked = frame("100", "00:00:04/00:00:04", {
    readIssue: { code: "window_not_visible", message: "窗口不可见" },
  });
  blocked.elements.push({ role: "AXButton", title: "登录" });
  const h = harness([blocked]);
  await assert.rejects(h.run(), /完成登录/);
  assert.equal(h.reads(), 1);
});

test("permission errors are not retried as window transitions", async () => {
  const h = harness();
  let reads = 0;
  h.deps.playbackSnapshot = async () => {
    reads++;
    throw new Error("辅助功能权限已撤销");
  };
  await assert.rejects(h.run(), /权限已撤销/);
  assert.equal(reads, 1);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

for (const current of [
  frame("25", "00:00:01/00:00:04", { running: false }),
  frame("25", "00:00:01/00:00:04", { pid: 11 }),
])
  test(`player ${current.running ? "restart" : "exit"} invalidates this course`, async () => {
    const h = harness([current]);
    await assert.rejects(h.run(), current.running ? /已重启/ : /已退出/);
    assert.equal(h.reads(), 1);
  });

test("a stopped capture cannot silently finish a course", async () => {
  const h = harness();
  h.deps.playbackSnapshot = async () => {
    h.disableCapture();
    return frame();
  };
  await assert.rejects(h.run(), /采集已停止/);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("slow native reads count toward the timeout when playback makes no forward progress", async () => {
  const h = harness();
  let reads = 0;
  h.deps.playbackSnapshot = async () => {
    h.advance(10_000);
    reads++;
    return frame(String(20 + reads));
  };
  await assert.rejects(
    h.run({ maxPlaybackMinutes: 1 }),
    /连续 1 分钟未观察到有效播放推进/,
  );
  assert.ok(reads <= 8);
});

test("normal long playback continues beyond a saved ten-minute limit and saves on completion", async () => {
  const h = harness();
  let seconds = 0;
  h.deps.playbackSnapshot = async () => {
    seconds = Math.min(seconds + 20, 900);
    h.advance(20_000);
    const clock = `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
    return frame(String(seconds / 9), `00:${clock}/00:15:00`);
  };
  await h.run({ maxPlaybackMinutes: 10 });
  assert.equal(seconds, 900);
  assert.ok(h.events.includes("save:第一讲"));
  assert.ok(!h.events.includes("save:第一讲（中断）"));
});

function secondsClock(seconds: number) {
  return [
    Math.floor(seconds / 3600),
    Math.floor((seconds % 3600) / 60),
    seconds % 60,
  ]
    .map((value) => String(value).padStart(2, "0"))
    .join(":");
}

function shorterCourseAfterStaleEnd(sliderOnly = false) {
  const h = harness();
  const oldEnd = frame("100", "00:45:51/00:45:51");
  const originalSnapshot = h.deps.snapshot;
  h.deps.snapshot = async () => {
    const current = await originalSnapshot();
    return { ...current, elements: [...current.elements, ...oldEnd.elements] };
  };
  let reads = 0;
  let seconds = 0;
  h.deps.playbackSnapshot = async () => {
    // Opening a new course is asynchronous: its first poll can still expose
    // the previous video's longer terminal clock before the new clock resets.
    if (++reads === 1) return { ...oldEnd, at: String(reads) };
    seconds = Math.min(seconds + 20, 2354);
    h.advance(10_000);
    return frame(
      String((seconds / 2354) * 100),
      sliderOnly && seconds < 2354 ? "" : `${secondsClock(seconds)}/00:39:14`,
      { at: String(reads) },
    );
  };
  return h;
}

test("a prior course's larger terminal clock cannot expire a moving shorter course's watchdog", async () => {
  const h = shorterCourseAfterStaleEnd();
  await h.run({ maxPlaybackMinutes: 10 });
  assert.ok(h.events.includes("save:第一讲"));
  assert.equal(h.events.includes("save:第一讲（中断）"), false);
});

test("a prior 100 percent marker cannot starve forward slider-only progress", async () => {
  const h = shorterCourseAfterStaleEnd(true);
  await h.run({ maxPlaybackMinutes: 10 });
  assert.ok(h.events.includes("save:第一讲"));
  assert.equal(h.events.includes("save:第一讲（中断）"), false);
});

test("forward progress after recovery is confirmed against the current course rather than a prior EOF", async () => {
  const h = shorterCourseAfterStaleEnd();
  const read = h.deps.playbackSnapshot;
  let reads = 0;
  let recoveries = 0;
  h.deps.playbackSnapshot = async () => {
    if (++reads === 3) throw new Error("transient progress read failure");
    return read();
  };
  h.deps.recoverPlayback = async () => {
    if (++recoveries > 1)
      throw new Error(
        "healthy forward progress was paused again after recovery",
      );
  };
  await h.run({ maxPlaybackMinutes: 10 });
  assert.equal(recoveries, 1);
  assert.ok(h.events.includes("log:播放恢复已确认"));
  assert.ok(h.events.includes("save:第一讲"));
});

test("a long video that stalls after the old limit still stops and preserves partial capture", async () => {
  const h = harness();
  let seconds = 0;
  h.deps.playbackSnapshot = async () => {
    seconds = Math.min(seconds + 20, 700);
    h.advance(20_000);
    const clock = `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
    return frame(String(seconds / 9), `00:${clock}/00:15:00`);
  };
  await assert.rejects(h.run({ maxPlaybackMinutes: 10 }), /连续 120 秒/);
  assert.equal(seconds, 700);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("stalled playback is not mistaken for completion or allowed to wait indefinitely", async () => {
  const h = harness([frame()]);
  await assert.rejects(h.run(), /连续 120 秒/);
  assert.ok(h.reads() < 250);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("cancellation while the terminal read is in flight cannot save as completed", async () => {
  const h = harness();
  let reads = 0;
  h.deps.playbackSnapshot = async () => {
    if (++reads === 4) h.controller.abort();
    return reads === 1 ? frame() : terminal();
  };
  await assert.rejects(h.run(), /已暂停/);
  assert.equal(h.events.includes("save:第一讲"), false);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("partial capture startup failure cleans up the newly enabled capture", async () => {
  const h = harness();
  const start = h.deps.startCapture;
  h.deps.startCapture = async () => {
    await start();
    throw new Error("启动响应丢失");
  };
  await assert.rejects(h.run(), /启动响应丢失/);
  assert.equal(h.events.includes("click"), false);
  assert.equal(h.events.filter((e) => e === "stop").length, 1);
});

test("failed saving never repeats playback or stop and preserves the save error", async () => {
  const h = harness();
  h.deps.saveCapture = async () => {
    throw new Error("磁盘写入失败");
  };
  await assert.rejects(h.run(), /磁盘写入失败/);
  assert.equal(h.events.filter((e) => e === "stop").length, 1);
  assert.equal(h.events.filter((e) => e === "click").length, 1);
});

test("cleanup failure remains visible alongside the original playback failure", async () => {
  const h = harness();
  h.deps.playbackSnapshot = async () => {
    throw new Error("权限错误");
  };
  h.deps.stopCapture = async () => {
    throw new Error("磁盘错误");
  };
  await assert.rejects(h.run(), /权限错误.*停止或中断记录保存失败.*磁盘错误/);
});

test("repeated cached terminal snapshots do not supply three confirmations", async () => {
  const h = harness([frame(), { ...terminal(), at: "2026-10-02T12:00:00Z" }]);
  await assert.rejects(h.run(), /重复的进度快照/);
  assert.equal(h.events.includes("save:第一讲"), false);
});

test("terminal confirmations cannot adopt a new duration after playback started", async () => {
  const long = () => frame("100", "00:00:05/00:00:05");
  const h = harness([frame(), terminal(), terminal(), long(), long(), long()]);
  await assert.rejects(h.run(), /播放总时长发生变化/);
  assert.equal(h.reads(), 4);
  assert.ok(!h.events.includes("save:第一讲"));
});

test("a fullscreen queue returns to the course list before capture and click", async () => {
  const h = harness();
  const read = h.deps.snapshot;
  let fullscreen = true;
  h.deps.snapshot = async () => ({
    ...(await read()),
    windowMode: fullscreen ? "fullscreen" : "windowed",
  });
  h.deps.setFullscreen = async (enabled) => {
    h.events.push(`fullscreen:${enabled}`);
    fullscreen = enabled;
  };
  await h.run();
  assert.ok(h.events.indexOf("fullscreen:false") < h.events.indexOf("start"));
  assert.equal(h.events.includes("fullscreen:true"), false);
});

test("course and recent-menu titles containing error words are not playback dialogs", async () => {
  const h = harness();
  const read = h.deps.snapshot;
  h.deps.snapshot = async () => ({
    ...(await read()),
    elements: [
      { role: "AXOutline", depth: 1 },
      { role: "AXRow", depth: 2, value: "第一讲" },
      { role: "AXStaticText", depth: 3, value: "网络异常处理" },
      { role: "AXMenuBar", depth: 1 },
      { role: "AXMenuItem", depth: 2, title: "视频不存在的排查方法.sz" },
    ],
  });
  await h.run();
  assert.ok(h.events.includes("save:第一讲"));
  const blocked = harness([
    frame("25", "00:00:01/00:00:04", {
      elements: [{ role: "AXStaticText", value: "网络异常" }],
    }),
  ]);
  await assert.rejects(blocked.run(), /无法继续播放.*网络异常/);
});

test("a stable real final frame completes after confirmation even without an exact 100 marker", async () => {
  const h = harness([
    frame("20", "00:05:56/00:29:41"),
    frame("99.98220825195312", "00:29:41/00:29:41"),
  ]);
  await h.run();
  assert.ok(h.reads() >= 12);
  assert.ok(h.events.includes("save:第一讲"));
  assert.equal(h.events.filter((event) => event === "click").length, 1);
});

test("a rounded final frame cannot establish that a new course started", async () => {
  const h = harness([frame("99.98220825195312", "00:29:41/00:29:41")]);
  await assert.rejects(h.run(), /未检测到播放启动/);
  assert.equal(h.events.includes("save:第一讲"), false);
});

test("rounded-end confirmation restarts after read gaps or changing final-frame values", async () => {
  const end = () => frame("99.98220825195312", "00:29:41/00:29:41");
  const samples = [
    frame("20", "00:05:56/00:29:41"),
    ...Array.from({ length: 8 }, end),
    {
      ...end(),
      readIssue: { code: "fullscreen_transition", message: "transition" },
    },
    ...Array.from({ length: 6 }, end),
    frame("99.983", "00:29:41/00:29:41"),
    ...Array.from({ length: 4 }, end),
    ...Array.from({ length: 3 }, () => frame("100", "00:29:41/00:29:41")),
  ];
  const h = harness(samples);
  await h.run();
  assert.equal(h.reads(), samples.length);
  assert.ok(h.events.includes("save:第一讲"));
});

test("rounded-end fallback rejects early, stale, or unverified end positions", async () => {
  const unverified = frame("99.98220825195312", "00:29:41/00:29:41");
  unverified.elements[0]!.identifier = undefined;
  unverified.elements[0]!.width = 624;
  for (const end of [
    frame("99.9", "00:29:41/00:29:41"),
    frame("99.98220825195312", "00:29:40/00:29:41"),
    frame("99.98220825195312", "00:29:41/00:29:41", { at: "cached" }),
    unverified,
  ]) {
    const h = harness([frame("20", "00:05:56/00:29:41"), end]);
    await assert.rejects(h.run(), /未观察到播放进度变化|重复的进度快照/);
    assert.equal(h.events.includes("save:第一讲"), false);
  }
});

test("hidden fullscreen controls recover to windowed playback and complete without pointer input", async () => {
  const h = harness();
  let fullscreen = false;
  let reads = 0;
  h.deps.setFullscreen = async (value) => {
    fullscreen = value;
    h.events.push(`fullscreen:${value}`);
  };
  h.deps.playbackSnapshot = async () => {
    reads++;
    if (reads === 2) fullscreen = true;
    if (fullscreen)
      return frame("0", "", {
        at: String(reads),
        windowMode: "fullscreen",
        elements: [],
      });
    return reads < 4
      ? frame("25", "00:00:01/00:00:04", {
          at: String(reads),
          windowMode: "windowed",
        })
      : frame("100", "00:00:04/00:00:04", {
          at: String(reads),
          windowMode: "windowed",
        });
  };
  await h.run();
  assert.deepEqual(
    h.events.filter((event) => event.startsWith("fullscreen:")),
    ["fullscreen:false"],
  );
  assert.ok(h.events.includes("save:第一讲"));
});

test("stale fullscreen controls recover once instead of waiting for the 120-second stall guard", async () => {
  const h = harness();
  let fullscreen = false;
  let reads = 0;
  h.deps.setFullscreen = async (value) => {
    fullscreen = value;
    h.events.push(`fullscreen:${value}`);
  };
  h.deps.playbackSnapshot = async () => {
    reads++;
    if (reads === 1) fullscreen = true;
    return fullscreen || reads === 1
      ? frame("25", "00:00:01/00:00:04", {
          at: String(reads),
          windowMode: fullscreen ? "fullscreen" : "windowed",
        })
      : frame("100", "00:00:04/00:00:04", {
          at: String(reads),
          windowMode: "windowed",
        });
  };
  await h.run();
  assert.ok(reads < 20);
  assert.equal(
    h.events.filter((event) => event === "fullscreen:false").length,
    1,
  );
  assert.ok(h.events.includes("save:第一讲"));
});

test("fullscreen recovery failure stops the course and never retries the transition", async () => {
  const h = harness([
    frame("25", "00:00:01/00:00:04"),
    frame("0", "", { windowMode: "fullscreen", elements: [] }),
  ]);
  let exits = 0;
  h.deps.setFullscreen = async (value) => {
    if (!value) {
      exits++;
      throw new Error("fullscreen recovery denied");
    }
  };
  await assert.rejects(h.run(), /fullscreen recovery denied/);
  assert.equal(exits, 1);
  assert.ok(h.events.includes("save:第一讲（中断）"));
});

test("a fullscreen EOF stays read-only and a canceled recovery never resumes the queue", async () => {
  const completed = harness([
    frame(),
    ...Array.from({ length: 3 }, () => ({
      ...terminal(),
      windowMode: "fullscreen",
    })),
  ]);
  await completed.run();
  assert.equal(
    completed.events.some((event) => event.startsWith("fullscreen:")),
    false,
  );
  const canceled = harness([
    frame(),
    frame("0", "", { windowMode: "fullscreen", elements: [] }),
  ]);
  canceled.deps.setFullscreen = async () => {
    canceled.controller.abort();
  };
  await assert.rejects(canceled.run(), OrchestrationInterrupted);
  assert.equal(canceled.events.includes("save:第一讲"), false);
  assert.equal(canceled.events.filter((event) => event === "click").length, 1);
});

test("healthy playback emits regular progress logs without logging every poll", async () => {
  const samples = Array.from({ length: 66 }, (_, i) =>
    frame(String(i + 1), `00:00:${String(i + 1).padStart(2, "0")}/00:02:00`),
  );
  // Keep clock values valid when crossing a minute.
  for (let i = 0; i < samples.length; i++) {
    const seconds = i + 1;
    samples[i] = frame(
      String(seconds / 1.2),
      `00:${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}/00:02:00`,
    );
  }
  samples.push(
    ...Array.from({ length: 3 }, () => frame("100", "00:02:00/00:02:00")),
  );
  const task = harness(samples);
  const entries: { message: string; detail?: string }[] = [];
  task.deps.onLog = (_, message, detail) => entries.push({ message, detail });
  await task.run();
  const progress = entries.filter((entry) => entry.message === "播放进度");
  assert.equal(progress.length, 3);
  assert.match(progress[0]!.detail!, /00:00:01 \/ 00:02:00/);
  assert.ok(entries.some((entry) => entry.message === "提取已结束"));
  assert.ok(entries.some((entry) => entry.message === "提取记录已保存"));
});

test("completion logs record actual evidence and capture quality without key data", async () => {
  const task = harness();
  const entries: {
    level: string;
    message: string;
    detail?: string;
    metadata?: { elapsedMs?: number };
  }[] = [];
  task.deps.onLog = (level, message, detail, metadata) =>
    entries.push({ level, message, detail, metadata });
  const stop = task.deps.stopCapture;
  task.deps.stopCapture = async () => ({
    ...(await stop()),
    id: "record",
    name: "record",
    valid: false,
    issues: ["缺少课程密码"],
    requestCount: 0,
  });
  await task.run();
  assert.match(
    entries.find((e) => e.message === "已确认播放完成")!.detail!,
    /连续 3 次新鲜采样/,
  );
  const saved = entries.find(
    (e) => e.message === "记录已保存，密钥完整性未通过",
  )!;
  assert.equal(saved.level, "warn");
  assert.match(saved.detail!, /请求记录：0 条/);
  assert.match(saved.detail!, /缺少课程密码/);
  assert.ok(
    entries.some(
      (e) =>
        e.message === "播放器检查通过" && e.metadata?.elapsedMs !== undefined,
    ),
  );
});

test("manual completion is never described as an observed natural finish", async () => {
  const task = harness();
  const entries: string[] = [];
  task.deps.onLog = (_, message) => entries.push(message);
  task.deps.manualComplete = () => true;
  await task.run();
  assert.ok(entries.includes("用户确认本课完成"));
  assert.equal(entries.includes("已确认播放完成"), false);
});

test("a failed course with no partial record does not claim it saved a record", async () => {
  const task = harness();
  const entries: string[] = [];
  task.deps.onLog = (_, message) => entries.push(message);
  task.deps.click = async () => {
    throw new Error("original native failure");
  };
  task.deps.stopCapture = async () => null;
  await assert.rejects(task.run(), /original native failure/);
  assert.ok(entries.includes("提取已停止，未生成中断记录"));
  assert.equal(entries.includes("中断记录已保存"), false);
});

test("each new video receives the requested volume after media startup replaces prior controls", async () => {
  const task = harness();
  let mediaReady = false;
  let actualVolume = 90;
  let reads = 0;
  const applied: string[] = [];
  task.deps.click = async () => {
    mediaReady = false;
    reads = 0;
  };
  task.deps.setVolume = async (percent) => {
    actualVolume = percent;
    applied.push(`${mediaReady ? "ready" : "loading"}:${percent}`);
  };
  task.deps.playbackSnapshot = async () => {
    if (!mediaReady) {
      // mediaBegin creates the next player and publishes its initial volume.
      mediaReady = true;
      actualVolume = 90;
      return frame("10", "00:00:01/00:00:10");
    }
    if (++reads < 3) return frame("20", "00:00:02/00:00:10");
    return frame("100", "00:00:10/00:00:10");
  };
  for (let video = 1; video <= 2; video++) {
    await task.run({ playbackVolumePercent: 5 });
    assert.equal(actualVolume, 5, `video ${video}: ${applied.join(" → ")}`);
  }
  assert.equal(applied.filter((event) => event === "ready:5").length, 2);
});

test("a progress timeout recovers inside the same execution without reopening or splitting capture", async () => {
  const task = harness();
  let recovered = false;
  let reads = 0;
  let recoveries = 0;
  task.deps.playbackSnapshot = async () => {
    if (!recovered) return { running: true, at: "", pid: 10, elements: [] };
    return reads++ < 2
      ? frame("20", "00:00:02/00:00:10")
      : frame("100", "00:00:10/00:00:10");
  };
  Object.assign(task.deps, {
    recoverPlayback: async () => {
      recoveries++;
      recovered = true;
    },
  });
  await task.run();
  assert.equal(recoveries, 1);
  assert.equal(task.events.filter((event) => event === "click").length, 1);
  assert.equal(task.events.filter((event) => event === "start").length, 1);
  assert.equal(task.events.filter((event) => event === "stop").length, 1);
  assert.equal(
    task.events.filter((event) => event.startsWith("save:")).length,
    1,
  );
});

test("volume failures after media startup retry within a bound before reporting success", async () => {
  const task = harness();
  let afterStart = false,
    attempts = 0;
  const log = task.deps.onLog;
  task.deps.onLog = (...args) => {
    if (args[1] === "已确认播放启动") afterStart = true;
    log(...args);
  };
  task.deps.setVolume = async () => {
    if (afterStart && ++attempts < 3) throw new Error("slider not ready");
  };
  await task.run();
  assert.equal(attempts, 3);
  assert.equal(
    task.events.filter((value) => value === "log:播放音量已设置并校验").length,
    1,
  );
});

test("persistent volume failure enters pause/recovery instead of silently playing at the wrong volume", async () => {
  const task = harness();
  let recoveries = 0;
  task.deps.setVolume = async () => {
    throw new Error("volume write not retained");
  };
  task.deps.recoverPlayback = async (request) => {
    assert.match(
      request.message,
      /音量连续 3 次设置失败.*volume write not retained/,
    );
    assert.equal(request.progress.seconds, 1);
    recoveries++;
  };
  await task.run();
  assert.equal(recoveries, 1);
  assert.equal(
    task.events.some((value) => value === "log:播放音量已设置并校验"),
    false,
  );
});

test("continuation rejects changed media, changed process, stale samples and restart prompts", () => {
  const request = {
    message: "timeout",
    expectedPid: 10,
    progress: { seconds: 2, totalSeconds: 10 },
    sampleAt: "old",
  };
  const valid = frame("30", "00:00:03/00:00:10", { at: "new" });
  assert.equal(validatePlaybackRecovery(valid, request).seconds, 3);
  for (const bad of [
    { ...valid, pid: 11 },
    { ...valid, at: "old" },
    frame("30", "00:00:03/00:00:20"),
    frame("0", "00:00:00/00:00:10"),
    {
      ...valid,
      elements: [
        ...valid.elements,
        { role: "AXStaticText", value: "是否继续上次播放的视频位置" },
      ],
    },
  ])
    assert.throws(() => validatePlaybackRecovery(bad, request));
});

test("a delayed new-course history prompt after first progress is declined once without recovery", async () => {
  const task = harness();
  const originalSnapshot = task.deps.snapshot;
  let reads = 0,
    declined = false,
    recoveries = 0;
  const clicks: string[] = [];
  task.deps.snapshot = async () =>
    declined ? frame("0.25", "00:00:06/00:39:14") : originalSnapshot();
  task.deps.playbackSnapshot = async () => {
    if (++reads === 1) return frame("0.04", "00:00:01/00:39:14");
    if (reads === 2) return frame("0.25", "00:00:06/00:39:14");
    if (!declined) return historyPrompt(frame("0.25", "00:00:06/00:39:14"));
    return frame("100", "00:39:14/00:39:14");
  };
  task.deps.click = async (text, count) => {
    clicks.push(`${text}:${count}`);
    if (text === "否") declined = true;
  };
  task.deps.recoverPlayback = async (request) => {
    recoveries++;
    throw new Error(request.message);
  };
  await task.run();
  assert.equal(recoveries, 0);
  assert.deepEqual(clicks, ["第一讲:2", "否:1"]);
  assert.equal(task.events.filter((event) => event === "start").length, 1);
  assert.equal(task.events.filter((event) => event === "stop").length, 1);
  assert.ok(task.events.includes("save:第一讲"));
});

test("a repeated new-course history prompt is held without a second negative press", async () => {
  const task = harness();
  const originalSnapshot = task.deps.snapshot;
  let reads = 0,
    declines = 0,
    recoveries = 0;
  const clicks: string[] = [];
  task.deps.snapshot = async () => (declines ? frame() : originalSnapshot());
  task.deps.playbackSnapshot = async () => {
    if (++reads === 1) return frame();
    return recoveries ? terminal() : historyPrompt();
  };
  task.deps.click = async (text, count) => {
    clicks.push(`${text}:${count}`);
    if (text === "否") declines++;
  };
  task.deps.recoverPlayback = async (request) => {
    assert.match(request.message, /提示再次出现.*不会自动选择从头播放/);
    recoveries++;
  };
  await task.run();
  assert.equal(recoveries, 1);
  assert.equal(declines, 1);
  assert.deepEqual(clicks, ["第一讲:2", "否:1"]);
});

test("a history prompt after actual automatic continuation is held for recovery without restart", async () => {
  const task = harness();
  let reads = 0,
    recovered = false,
    recoveries = 0;
  task.deps.playbackSnapshot = async () => {
    if (++reads === 1) return frame();
    if (reads === 2) throw new Error("native read failed");
    if (!recovered) return historyPrompt();
    return terminal();
  };
  task.deps.recoverPlayback = async (request) => {
    if (++recoveries === 1) assert.match(request.message, /native read failed/);
    else {
      assert.match(request.message, /不会自动选择从头播放/);
      recovered = true;
    }
  };
  await task.run();
  assert.equal(recoveries, 2);
  assert.equal(task.events.filter((event) => event === "click").length, 1);
});

test("a canceled read does not start recovery after the user ends the task", async () => {
  const task = harness();
  let recovered = false;
  task.deps.playbackSnapshot = async () => {
    task.controller.abort();
    throw new Error("late read failure");
  };
  task.deps.recoverPlayback = async () => {
    recovered = true;
  };
  await assert.rejects(task.run(), OrchestrationInterrupted);
  assert.equal(recovered, false);
});

test("a cached pause failure is reported once while the interrupted capture is still saved", async () => {
  const task = harness();
  task.deps.manualComplete = () => true;
  task.deps.pausePlayback = async () => {
    throw new Error("播放控件读取超时");
  };
  await assert.rejects(task.run(), (error: Error) => {
    assert.equal(error.message.split("播放控件读取超时").length - 1, 1);
    assert.match(error.message, /无法确认视频已暂停/);
    return true;
  });
  assert.ok(task.events.includes("stop"));
  assert.ok(task.events.some((event) => event.startsWith("save:")));
});

test("manual completion pauses playback before archiving the capture", async () => {
  const task = harness();
  task.deps.manualComplete = () => true;
  task.deps.pausePlayback = async () => {
    task.events.push("pause-manual");
  };
  await task.run();
  assert.ok(task.events.indexOf("pause-manual") >= 0);
  assert.ok(task.events.indexOf("pause-manual") < task.events.indexOf("stop"));
  assert.ok(task.events.includes("log:用户确认本课完成"));
});
