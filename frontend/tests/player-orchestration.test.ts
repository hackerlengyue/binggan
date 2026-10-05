import test from "node:test";
import assert from "node:assert/strict";
import { getEventListeners } from "node:events";
import type { PlayerSnapshot } from "../src/lib/player-automation";
import {
  OrchestrationInterrupted,
  courseCaptureName,
  completionReached,
  createAbortableSleep,
  coursesFromSnapshot,
  detectPlayerPage,
  discoverCourses,
  executeCourse,
  preparePlayer,
  readPlaybackProgress,
  type OrchestrationConfig,
  type OrchestrationDependencies,
} from "../src/lib/player-orchestration";

test("capture history uses only the lesson file name", () => {
  assert.equal(
    courseCaptureName({
      id: "nested",
      name: "第五阶段 / 第一讲",
      sourcePath: "/课程/第五阶段/第一讲.sz",
    }),
    "第一讲",
  );
  assert.equal(
    courseCaptureName({ id: "manual", name: "第五阶段 / 第二讲" }),
    "第二讲",
  );
  assert.equal(
    courseCaptureName({
      id: "ax",
      name: "第五阶段 / 第三讲",
      targetText: "第三讲",
      sourcePath: "_NS:26",
    }),
    "第三讲",
  );
});

test("playback progress reads elapsed time and the wide slider, never volume", () => {
  assert.deepEqual(
    readPlaybackProgress({
      running: true,
      at: "",
      elements: [
        { role: "AXSlider", width: 72, value: "100" },
        { role: "AXSlider", identifier: "_NS:26", width: 624, value: "1" },
        { role: "AXStaticText", value: "00:00:06/00:10:00" },
      ],
    }),
    { seconds: 6, totalSeconds: 600, percent: 1 },
  );
});

test("a download indicator does not override the playback slider", () => {
  assert.equal(
    readPlaybackProgress({
      running: true,
      at: "",
      elements: [
        { role: "AXProgressIndicator", value: "100" },
        { role: "AXSlider", width: 624, value: "20" },
        { role: "AXStaticText", value: "00:02:00/00:10:00" },
      ],
    }).percent,
    20,
  );
});

const config: OrchestrationConfig = {
  playbackVolumePercent: 35,
  playbackRate: null,
  actionDelayMs: 1,
  courseReadyTimeoutSeconds: 1,
  pollIntervalMs: 1,
  maxPlaybackMinutes: 1,
};

test("completed polling sleeps release their abort listeners", async () => {
  const signal = new AbortController().signal;
  const sleep = createAbortableSleep();
  for (let index = 0; index < 15; index++) await sleep(0, signal);
  assert.equal(getEventListeners(signal, "abort").length, 0);
});

function fixture(overrides: Partial<OrchestrationDependencies> = {}) {
  const calls: string[] = [];
  let snapshots = 0;
  let capturing = false;
  const dependencies: OrchestrationDependencies = {
    playbackSnapshot: () => dependencies.snapshot(),
    snapshot: async () => {
      calls.push("snapshot");
      snapshots++;
      return {
        running: true,
        at: "",
        elements: [
          { role: "AXOutline" },
          { role: "AXRow", depth: 2 },
          { role: "AXStaticText", depth: 3, value: "高等数学 第一讲" },
          { role: "AXRow", depth: 2 },
          { role: "AXStaticText", depth: 3, value: "课程二" },
          { role: "AXRow", depth: 2 },
          { role: "AXStaticText", depth: 3, value: "课程三" },
          {
            role: "AXSlider",
            identifier: "_NS:26",
            width: 624,
            value: snapshots >= 5 ? "100" : snapshots === 4 ? "20" : "0",
          },
          {
            role: "AXStaticText",
            value:
              snapshots >= 5
                ? "00:10:00/00:10:00"
                : snapshots === 4
                  ? "00:02:00/00:10:00"
                  : "00:00:00/00:10:00",
          },
        ],
      };
    },
    courseSnapshot: async () => ({
      running: true,
      at: new Date().toISOString(),
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0 },
        { role: "AXStaticText", depth: 3, value: "课程一" },
      ],
    }),
    revealCourse: async (path) => {
      for (const folder of path.slice(0, -1))
        await dependencies.expandFolder(folder);
      return dependencies.snapshot();
    },
    expandFolder: async (name) => {
      calls.push("expand:" + name);
    },
    openCourse: async (path) => {
      calls.push("open:" + path);
    },
    setVolume: async (percent) => {
      calls.push("volume:" + percent);
    },
    setFullscreen: async (enabled) => {
      calls.push("fullscreen:" + enabled);
    },
    setPlaybackRate: async (rate) => {
      calls.push("rate:" + rate.toFixed(1));
    },
    click: async (text, count = 1, match) => {
      calls.push(
        "click:" +
          text +
          ":" +
          count +
          (match && match.count > 1
            ? ":" + match.ordinal + "/" + match.count
            : ""),
      );
    },
    startCapture: async () => {
      calls.push("capture:start");
      capturing = true;
    },
    captureActive: () => capturing,
    stopCapture: async () => {
      calls.push("capture:stop");
      capturing = false;
      return { id: "capture-1", name: "未命名" };
    },
    saveCapture: async (entry, name) => {
      calls.push("save:" + entry.id + ":" + name);
    },
    sleep: async () => {
      calls.push("sleep");
    },
    manualComplete: () => false,
    onStep: (step) => {
      calls.push("step:" + step);
    },
    onLog: () => {},
    ...overrides,
  };
  return { calls, dependencies };
}

test("one course is captured, played, stopped and saved in strict order", async () => {
  const { calls, dependencies } = fixture();
  const result = await executeCourse(
    dependencies,
    config,
    { id: "course-1", name: "高等数学 第一讲" },
    new AbortController().signal,
  );

  assert.equal(result.id, "capture-1");
  assert.ok(
    calls.indexOf("capture:start") < calls.indexOf("click:高等数学 第一讲:2"),
  );
  assert.ok(
    calls.indexOf("click:高等数学 第一讲:2") < calls.indexOf("volume:35"),
  );
  assert.ok(calls.indexOf("volume:35") < calls.indexOf("capture:stop"));
  assert.ok(
    calls.indexOf("click:高等数学 第一讲:2") <
      calls.indexOf("step:startingPlayback"),
  );
  assert.ok(
    calls.indexOf("capture:stop") <
      calls.indexOf("save:capture-1:高等数学 第一讲"),
  );
});

test("speed applies after playback starts and legacy fullscreen does not steal the desktop", async () => {
  const { calls, dependencies } = fixture();
  const legacyConfig = { ...config, playbackRate: 1.5, enterFullscreen: true };
  await executeCourse(
    dependencies,
    legacyConfig,
    { id: "configured", name: "高等数学 第一讲" },
    new AbortController().signal,
  );
  const index = (call: string) => calls.indexOf(call);
  assert.ok(
    index("click:高等数学 第一讲:2") < index("step:configuringPlayback"),
  );
  assert.ok(index("step:configuringPlayback") < index("rate:1.5"));
  assert.ok(index("rate:1.5") < index("step:waitingCompletion"));
  assert.ok(index("step:waitingCompletion") < index("capture:stop"));
  assert.equal(calls.filter((call) => call === "rate:1.5").length, 1);
  assert.equal(calls.filter((call) => call === "fullscreen:true").length, 0);
});

test("failed playback setting stops capture and preserves an interrupted record", async () => {
  const { calls, dependencies } = fixture({
    setPlaybackRate: async () => {
      calls.push("rate:failed");
      throw new Error("SzPlayer 倍速菜单未出现");
    },
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      { ...config, playbackRate: 1.5 },
      { id: "configured", name: "高等数学 第一讲" },
      new AbortController().signal,
    ),
    /倍速菜单未出现/,
  );
  assert.deepEqual(calls.slice(-3), [
    "rate:failed",
    "capture:stop",
    "save:capture-1:高等数学 第一讲（中断）",
  ]);
  assert.equal(calls.includes("fullscreen:true"), false);
});

test("pausing the completion checkpoint retains the same playback and capture", async () => {
  const { calls, dependencies } = fixture();
  let release!: () => void;
  let entered!: () => void;
  const paused = new Promise<void>((resolve) => {
    entered = resolve;
  });
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  let held = false;
  dependencies.checkpoint = async () => {
    if (!held && calls.includes("step:startingPlayback")) {
      held = true;
      entered();
      await gate;
    }
  };
  const task = executeCourse(
    dependencies,
    config,
    { id: "course-1", name: "高等数学 第一讲" },
    new AbortController().signal,
  );
  await paused;
  assert.equal(calls.filter((call) => call.startsWith("click:")).length, 1);
  assert.equal(calls.includes("capture:stop"), false);
  release();
  await task;
  assert.equal(calls.filter((call) => call.startsWith("click:")).length, 1);
  assert.equal(calls.filter((call) => call === "capture:start").length, 1);
});

test("time spent paused does not consume the playback startup timeout", async () => {
  const originalNow = Date.now;
  let now = 0;
  let snapshots = 0;
  let checkpoints = 0;
  Date.now = () => now;
  try {
    const { dependencies } = fixture({
      snapshot: async () => {
        const poll = Math.max(0, ++snapshots - 3);
        return {
          running: true,
          at: "",
          elements: [
            { role: "AXOutline" },
            { role: "AXRow", depth: 2, value: "高等数学 第一讲" },
            {
              role: "AXSlider",
              width: 624,
              value: poll >= 3 ? "100" : poll >= 2 ? "50" : "0",
            },
            {
              role: "AXStaticText",
              value:
                poll >= 3
                  ? "00:01:00/00:01:00"
                  : poll >= 2
                    ? "00:00:01/00:01:00"
                    : "00:00:00/00:01:00",
            },
          ],
        };
      },
      sleep: async () => {
        now += 1000;
      },
      checkpoint: async () => {
        if (++checkpoints === 6) now += 45_000;
      },
    });
    await executeCourse(
      dependencies,
      config,
      { id: "course-1", name: "高等数学 第一讲" },
      new AbortController().signal,
    );
    assert.ok(checkpoints >= 6);
  } finally {
    Date.now = originalNow;
  }
});

test("a catalog course double-clicks the visible UI even when a file path exists", async () => {
  const { calls, dependencies } = fixture();
  const sourcePath = "/课程/高等数学 第一讲.sz";

  await executeCourse(
    dependencies,
    config,
    { id: "course-file", name: "高等数学 第一讲", sourcePath },
    new AbortController().signal,
  );

  assert.ok(
    calls.indexOf("capture:start") < calls.indexOf("click:高等数学 第一讲:2"),
  );
  assert.ok(
    calls.indexOf("click:高等数学 第一讲:2") < calls.indexOf("capture:stop"),
  );
  assert.equal(
    calls.some((call) => call.startsWith("open:")),
    false,
  );
  assert.equal(calls.includes("wake"), false);
});

test("a catalog course reaches the visible player row before capture starts", async () => {
  const { calls, dependencies } = fixture();

  await executeCourse(
    dependencies,
    config,
    {
      id: "course-file-visible",
      name: "高等数学 第一讲",
      sourcePath: "/课程/高等数学 第一讲.sz",
    },
    new AbortController().signal,
  );

  const snapshot = calls.indexOf("snapshot");
  const capture = calls.indexOf("capture:start");
  assert.notEqual(snapshot, -1);
  assert.ok(snapshot < capture);
});

test("a visible course starts without confirmation controls", async () => {
  const { calls, dependencies } = fixture();
  await executeCourse(
    dependencies,
    config,
    { id: "course-visible", name: "高等数学 第一讲" },
    new AbortController().signal,
  );
  assert.equal(calls.includes("click:确认:1"), false);
});

test("a confirmation page asks the user to enter the course list manually", async () => {
  const { calls, dependencies } = fixture();
  dependencies.snapshot = async () => {
    calls.push("snapshot");
    return {
      running: true,
      at: "",
      elements: [{ role: "AXButton", title: "确认" }],
    };
  };
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "course-confirm", name: "课程一" },
      new AbortController().signal,
    ),
    /进入课程列表/,
  );
  assert.equal(
    calls.some((call) => call.startsWith("click:")),
    false,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("course matching tolerates spacing and row metadata, then clicks the visible row", async () => {
  const { calls, dependencies } = fixture();
  let snapshots = 0;
  dependencies.snapshot = async () => {
    snapshots++;
    return {
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        { role: "AXRow", depth: 2 },
        { role: "AXStaticText", depth: 3, value: "高等数学 · 第一讲  30分钟" },
        {
          role: "AXSlider",
          width: 624,
          value: snapshots >= 5 ? "100" : snapshots === 4 ? "20" : "0",
        },
        {
          role: "AXStaticText",
          value:
            snapshots >= 5
              ? "00:30:00/00:30:00"
              : snapshots === 4
                ? "00:06:00/00:30:00"
                : "00:00:00/00:30:00",
        },
      ],
    };
  };

  await executeCourse(
    dependencies,
    config,
    { id: "course-fuzzy", name: "高等数学 第一讲" },
    new AbortController().signal,
  );

  assert.ok(calls.includes("click:高等数学 · 第一讲  30分钟:2"));
});

test("duplicate visible lesson labels stop before capture rather than opening another course", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        { role: "AXRow", depth: 2 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
        { role: "AXRow", depth: 2 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
      ],
    }),
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "duplicate", name: "第一讲" },
      new AbortController().signal,
    ),
    /多个.*第一讲/,
  );
  assert.equal(calls.includes("capture:start"), false);
  assert.equal(
    calls.some((call) => call.startsWith("click:")),
    false,
  );
});

test("a course path selects the right row when two folders have the same lesson name", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, canExpand: true, expanded: true },
        { role: "AXStaticText", depth: 3, value: "高等数学" },
        { role: "AXRow", depth: 2, level: 1 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
        { role: "AXRow", depth: 2, level: 0, canExpand: true, expanded: true },
        { role: "AXStaticText", depth: 3, value: "物理" },
        { role: "AXRow", depth: 2, level: 1 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
      ],
    }),
    manualComplete: () => true,
  });
  await executeCourse(
    dependencies,
    config,
    { id: "physics-lesson", name: "物理 / 第一讲", targetText: "第一讲" },
    new AbortController().signal,
  );
  assert.ok(calls.includes("click:第一讲:2:1/2"));
  assert.equal(calls.includes("click:第一讲:2"), false);
});

test("a course path never falls back to a lesson in another folder", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, canExpand: true, expanded: true },
        { role: "AXStaticText", depth: 3, value: "高等数学" },
        { role: "AXRow", depth: 2, level: 1 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
      ],
    }),
    manualComplete: () => true,
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      { ...config, courseReadyTimeoutSeconds: 0.01 },
      { id: "physics", name: "物理 / 第一讲", targetText: "第一讲" },
      new AbortController().signal,
    ),
    /仍未看到课程/,
  );
  assert.equal(calls.includes("capture:start"), false);
  assert.equal(
    calls.some((call) => call.startsWith("click:")),
    false,
  );
});

test("a course list change after capture starts never clicks the stale row", async () => {
  let snapshots = 0;
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        ...(++snapshots < 3
          ? [
              { role: "AXRow", depth: 2 },
              { role: "AXStaticText", depth: 3, value: "第一讲" },
            ]
          : []),
      ],
    }),
    manualComplete: () => true,
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "stale-row", name: "第一讲" },
      new AbortController().signal,
    ),
    /课程列表已变化/,
  );
  assert.ok(calls.includes("capture:start"));
  assert.ok(calls.includes("capture:stop"));
  assert.equal(
    calls.some((call) => call.startsWith("click:")),
    false,
  );
});

test("a login screen asks the user to sign in before capture starts", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [{ role: "AXButton", title: "登陆" }],
    }),
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "course-login-required", name: "课程一" },
      new AbortController().signal,
    ),
    /完成登录/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("SzPlayer's unlabeled username arrow is recognized as a login screen", async () => {
  const loginSnapshot = {
    running: true,
    at: "",
    elements: [
      { role: "AXTextField", value: "saved-user", placeholder: "输入用户名" },
      { role: "AXButton" },
      { role: "AXButton", title: "线路" },
    ],
  };
  assert.equal(detectPlayerPage(loginSnapshot), "login");
  const { calls, dependencies } = fixture({
    snapshot: async () => loginSnapshot,
  });
  await assert.rejects(
    preparePlayer(dependencies, config, new AbortController().signal),
    /完成登录/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

const resumePrompt = (
  message = "是否继续播放上次关闭的视频位置：第一讲",
): PlayerSnapshot => ({
  running: true,
  at: "",
  elements: [
    { role: "AXOutline" },
    { role: "AXStaticText", value: message },
    { role: "AXButton", title: "[是]" },
    { role: "AXButton", title: "[否]" },
  ],
});

for (const message of [
  "是否继续播放上次关闭的视频位置：第一讲",
  "是否继续上次播放的视频位置？[00:02:00]",
]) {
  test(`resume prompt is declined automatically: ${message}`, async () => {
    let declined = false;
    const { calls, dependencies } = fixture({
      snapshot: async () =>
        declined
          ? { running: true, at: "", elements: [{ role: "AXOutline" }] }
          : resumePrompt(message),
      click: async (text, count) => {
        assert.equal(text, "[否]");
        assert.equal(count, 1);
        declined = true;
      },
    });
    await preparePlayer(dependencies, config, new AbortController().signal);
    assert.equal(declined, true);
    assert.equal(calls.includes("capture:start"), false);
  });
}

test("SzPlayer's recording-process alert names the playback blocker", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        {
          role: "AXStaticText",
          value:
            "温馨提示 请关闭以下进程或者取消其录屏权限再播放加密视频，播放器将会自动关闭！\ncom.openai.sky.CUAService",
        },
      ],
    }),
  });
  await assert.rejects(
    preparePlayer(dependencies, config, new AbortController().signal),
    /录屏.*com\.openai\.sky\.CUAService/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("an unreadable or playback-only player gives a specific preflight error", async () => {
  const cases = [
    { elements: [], reason: /无法读取 SzPlayer 可见控件/ },
    {
      elements: [{ role: "AXSlider", width: 624, value: "30" }],
      reason: /切回.*本地文件.*课程列表/,
    },
  ];
  for (const { elements, reason } of cases) {
    const { dependencies } = fixture({
      snapshot: async () => ({ running: true, at: "", elements }),
    });
    await assert.rejects(
      preparePlayer(dependencies, config, new AbortController().signal),
      reason,
    );
  }
});

test("a failure after capture begins stops and saves an interrupted record", async () => {
  const { calls, dependencies } = fixture({
    click: async (text) => {
      calls.push("click:" + text + ":1");
      if (text === "课程二") throw new Error("未找到可见控件：课程二");
    },
  });

  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "course-2", name: "课程二" },
      new AbortController().signal,
    ),
    /未找到可见控件/,
  );
  assert.deepEqual(calls.slice(-2), [
    "capture:stop",
    "save:capture-1:课程二（中断）",
  ]);
});

test("playback never starts when the direct capture API is not active", async () => {
  const { calls, dependencies } = fixture({ captureActive: () => false });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "capture-inactive", name: "课程三" },
      new AbortController().signal,
    ),
    /抓包 API 未进入运行状态/,
  );
  assert.equal(calls.includes("click:课程三:2"), false);
});

test("a closed player refuses to capture and is never opened automatically", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: false,
      at: new Date().toISOString(),
      elements: [],
    }),
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "course-3", name: "课程三" },
      new AbortController().signal,
    ),
    /请先打开播放器/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("a missing course stops before capture starts", async () => {
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [{ role: "AXOutline" }],
    }),
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      { ...config, courseReadyTimeoutSeconds: 0 },
      { id: "missing", name: "不存在的课程" },
      new AbortController().signal,
    ),
    /仍未看到课程/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("player page detection distinguishes login, course list and playback", () => {
  assert.equal(
    detectPlayerPage({
      running: true,
      at: "",
      elements: [{ role: "AXButton", title: "登陆" }],
    }),
    "login",
  );
  assert.equal(
    detectPlayerPage({
      running: true,
      at: "",
      elements: [{ role: "AXOutline" }],
    }),
    "courses",
  );
  assert.equal(
    detectPlayerPage({
      running: true,
      at: "",
      elements: [{ role: "AXSlider", width: 624, value: "10" }],
    }),
    "playback",
  );
});

test("course discovery keeps leaf rows and builds a stable hierarchy path", () => {
  const courses = coursesFromSnapshot({
    running: true,
    at: "",
    elements: [
      { role: "AXOutline", depth: 1 },
      { role: "AXRow", depth: 2, level: 0, canExpand: true, expanded: true },
      { role: "AXStaticText", depth: 3, value: "高等数学" },
      {
        role: "AXRow",
        depth: 2,
        level: 1,
        identifier: "/课程/高等数学/第一讲.sz",
      },
      { role: "AXStaticText", depth: 3, value: "第一讲" },
      { role: "AXStaticText", depth: 3, value: "进度:0%" },
      { role: "AXStaticText", depth: 3, value: "30分钟" },
    ],
  });
  assert.deepEqual(courses, [
    {
      id: "file:/课程/高等数学/第一讲.sz",
      name: "高等数学 / 第一讲",
      targetText: "第一讲",
      sourcePath: "/课程/高等数学/第一讲.sz",
    },
  ]);
});

test("SzPlayer row values provide folder and lesson names without child text", async () => {
  const elements = [
    { role: "AXOutline", depth: 1 },
    {
      role: "AXRow",
      depth: 2,
      level: 0,
      value: "第五阶段",
      title: "levelFoldIcon",
      canExpand: true,
      expanded: true,
    },
    { role: "AXRow", depth: 2, level: 1, value: "第一讲" },
  ];
  assert.deepEqual(coursesFromSnapshot({ running: true, at: "", elements }), [
    {
      id: "1:第五阶段 / 第一讲",
      name: "第五阶段 / 第一讲",
      targetText: "第一讲",
    },
  ]);
  const { calls, dependencies } = fixture({
    snapshot: async () => ({ running: true, at: "", elements }),
    manualComplete: () => true,
  });
  await executeCourse(
    dependencies,
    config,
    { id: "row-value-lesson", name: "第五阶段 / 第一讲", targetText: "第一讲" },
    new AbortController().signal,
  );
  assert.ok(calls.includes("click:第一讲:2"));
});

test("a row value takes precedence over longer detail text when naming a lesson", () => {
  assert.deepEqual(
    coursesFromSnapshot({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, value: "第一讲" },
        { role: "AXStaticText", depth: 3, value: "第一讲：课程详情与学习说明" },
      ],
    }),
    [{ id: "0:第一讲", name: "第一讲", targetText: "第一讲" }],
  );
});

test("course rows exclude text from a following sibling panel", () => {
  assert.deepEqual(
    coursesFromSnapshot({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
        { role: "AXGroup", depth: 2 },
        { role: "AXStaticText", depth: 3, value: "第一讲相关课程推荐" },
      ],
    }),
    [{ id: "0:第一讲", name: "第一讲", targetText: "第一讲" }],
  );
});

test("course discovery performs one read-only outline snapshot", async () => {
  const { calls, dependencies } = fixture({
    courseSnapshot: async () => {
      calls.push("courseSnapshot");
      return {
        running: true,
        at: "",
        elements: [
          { role: "AXOutline", depth: 1 },
          {
            role: "AXRow",
            depth: 2,
            level: 0,
            canExpand: true,
            expanded: true,
          },
          { role: "AXStaticText", depth: 3, value: "高等数学" },
          { role: "AXRow", depth: 2, level: 1 },
          { role: "AXStaticText", depth: 3, value: "第一讲" },
        ],
      };
    },
  });

  const courses = await discoverCourses(
    dependencies,
    config,
    new AbortController().signal,
  );

  assert.equal(courses[0]?.name, "高等数学 / 第一讲");
  assert.deepEqual(calls, ["courseSnapshot"]);
});

test("course discovery rejects a saved local catalog when the player is stopped", async () => {
  const { calls, dependencies } = fixture({
    courseSnapshot: async () => {
      calls.push("courseSnapshot");
      return {
        running: false,
        at: "",
        elements: [
          { role: "AXOutline", depth: 1 },
          {
            role: "AXRow",
            depth: 2,
            level: 0,
            canExpand: true,
            expanded: true,
          },
          { role: "AXStaticText", depth: 3, value: "第五阶段" },
          { role: "AXRow", depth: 2, level: 1 },
          { role: "AXStaticText", depth: 3, value: "课程一" },
        ],
      };
    },
  });

  await assert.rejects(
    discoverCourses(dependencies, config, new AbortController().signal),
    /请先打开 SzPlayer/,
  );
  assert.deepEqual(calls, ["courseSnapshot"]);
});

test("course discovery explains an empty native snapshot instead of crashing", async () => {
  for (const running of [false, true]) {
    const { dependencies } = fixture({
      courseSnapshot: async () =>
        ({
          running,
          at: "",
          elements: null,
        }) as unknown as PlayerSnapshot,
    });
    await assert.rejects(
      discoverCourses(dependencies, config, new AbortController().signal),
      running ? /无法读取 SzPlayer 课程控件/ : /请先打开 SzPlayer/,
    );
  }
});

test("completion requires progress and matching elapsed and total time", () => {
  assert.equal(
    completionReached(
      {
        running: true,
        at: "",
        elements: [
          { role: "AXSlider", width: 624, value: "100" },
          { role: "AXStaticText", value: "00:10:00/00:10:00" },
        ],
      },
      false,
    ),
    false,
  );
  assert.equal(
    completionReached(
      {
        running: true,
        at: "",
        elements: [
          { role: "AXSlider", width: 624, value: "100" },
          { role: "AXStaticText", value: "00:09:59/00:10:00" },
        ],
      },
      true,
    ),
    false,
  );
  assert.equal(
    completionReached(
      {
        running: true,
        at: "",
        elements: [
          { role: "AXProgressIndicator", value: "100" },
          { role: "AXStaticText", value: "00:10:00/00:10:00" },
        ],
      },
      true,
    ),
    false,
  );
});

test("completion uses the wide playback slider and ignores the volume slider", () => {
  const snapshot = {
    running: true,
    at: "",
    elements: [
      { role: "AXSlider", identifier: "_NS:68", width: 72, value: "100" },
      { role: "AXSlider", identifier: "_NS:26", width: 624, value: "42" },
    ],
  };
  snapshot.elements.push({
    role: "AXStaticText",
    value: "00:10:00/00:10:00",
  });
  assert.equal(completionReached(snapshot, true), false);
  snapshot.elements[1].value = "100";
  assert.equal(completionReached(snapshot, true), true);
});

test("a clock alone does not prove playback completion", () => {
  assert.equal(
    completionReached(
      {
        running: true,
        at: "",
        elements: [{ role: "AXStaticText", value: "00:00:04/00:00:04" }],
      },
      true,
    ),
    false,
  );
});

test("a ticking playback clock confirms start but needs a terminal slider to finish", async (t) => {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  let snapshots = 0;
  const { calls, dependencies } = fixture({
    snapshot: async () => {
      snapshots++;
      const seconds = snapshots < 4 ? 0 : snapshots === 4 ? 1 : 4;
      return {
        running: true,
        at: "",
        elements: [
          { role: "AXOutline" },
          { role: "AXRow", depth: 2 },
          { role: "AXStaticText", depth: 3, value: "第一讲" },
          { role: "AXStaticText", value: `00:00:0${seconds}/00:00:04` },
          ...(seconds === 4
            ? [{ role: "AXSlider", identifier: "_NS:26", value: "100" }]
            : []),
        ],
      };
    },
    sleep: async () => {
      now += 1000;
    },
  });
  await executeCourse(
    dependencies,
    config,
    { id: "clock-only", name: "第一讲" },
    new AbortController().signal,
  );
  assert.ok(calls.includes("step:waitingCompletion"));
  assert.ok(calls.includes("capture:stop"));
});

test("hidden control bars are refreshed for every playback poll before reading progress", async (t) => {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  let refreshes = 0;
  const seen: number[] = [];
  const stale: PlayerSnapshot = {
    running: true,
    at: "",
    elements: [
      { role: "AXOutline" },
      { role: "AXRow", depth: 2, value: "第一讲" },
      { role: "AXSlider", identifier: "_NS:26", value: "0" },
      { role: "AXStaticText", value: "00:00:00/00:00:04" },
    ],
  };
  const { calls, dependencies } = fixture({
    snapshot: async () => stale,
    playbackSnapshot: async () => {
      refreshes++;
      const seconds = refreshes === 1 ? 1 : 4;
      return {
        ...stale,
        elements: [
          {
            role: "AXSlider",
            identifier: "_NS:26",
            value: seconds === 1 ? "25" : "100",
          },
          { role: "AXStaticText", value: `00:00:0${seconds}/00:00:04` },
        ],
      };
    },
    onProgress: (progress) => {
      seen.push(progress.seconds ?? -1);
    },
    sleep: async () => {
      now += 1000;
    },
  });
  await executeCourse(
    dependencies,
    config,
    { id: "hidden-bar", name: "第一讲" },
    new AbortController().signal,
  );
  assert.equal(
    refreshes,
    4,
    "three stable terminal samples are still required",
  );
  assert.deepEqual(seen, [0, 1, 4, 4, 4]);
  assert.ok(calls.includes("step:waitingCompletion"));
  assert.ok(calls.includes("capture:stop"));
  assert.ok(calls.includes("save:capture-1:第一讲"));
});

test("completion requires SzPlayer's terminal marker, not a rounded final frame", () => {
  const frame = (
    value: string,
    time = "00:00:04/00:00:04",
  ): PlayerSnapshot => ({
    running: true,
    at: "",
    elements: [
      { role: "AXSlider", identifier: "_NS:26", value },
      { role: "AXStaticText", value: time },
    ],
  });
  assert.equal(completionReached(frame("99"), true), false);
  assert.equal(completionReached(frame("99.999"), true), false);
  assert.equal(completionReached(frame("100"), true), true);
  assert.equal(
    completionReached(frame("100", "00:00:04 / 00:00:04"), true),
    true,
  );
  assert.equal(completionReached(frame("99"), false), false);
  assert.equal(completionReached(frame("50"), true), false);
  assert.equal(
    completionReached(frame("99", "00:00:03/00:00:04"), true),
    false,
  );
  assert.equal(completionReached(frame("0", "00:00:00/00:00:00"), true), false);
});

test("an unrelated progress indicator cannot start a stale completed video", async (t) => {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  let snapshots = 0;
  const { dependencies } = fixture({
    snapshot: async () => {
      snapshots++;
      return {
        running: true,
        at: "",
        elements: [
          { role: "AXOutline" },
          { role: "AXRow", depth: 2 },
          { role: "AXStaticText", depth: 3, value: "第一讲" },
          { role: "AXProgressIndicator", value: snapshots >= 4 ? "100" : "0" },
          { role: "AXStaticText", value: "00:10:00/00:10:00" },
        ],
      };
    },
    sleep: async () => {
      now += 1000;
    },
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "stale-video", name: "第一讲" },
      new AbortController().signal,
    ),
    /未检测到播放启动/,
  );
});

test("a recent-document menu match never starts capture or opens a missing course", async () => {
  const name = "【阶段五】1. SDD开发理念与三大框架全景解析";
  const controller = new AbortController();
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 3 },
        { role: "AXRow", depth: 4, canExpand: true, expanded: true },
        { role: "AXStaticText", value: "第五阶段", depth: 6 },
        { role: "AXMenuBar", depth: 1 },
        { role: "AXMenuItem", title: name + " .sz", depth: 6 },
      ],
    }),
    sleep: async () => controller.abort(),
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "recent-document", name, sourcePath: "/课程/" + name + ".sz" },
      controller.signal,
    ),
  );
  assert.equal(calls.includes("capture:start"), false);
  assert.equal(
    calls.some((call) => call.startsWith("open:")),
    false,
  );
});

test("nested folders expand before visibility check, capture, and double-click", async () => {
  const expanded: string[] = [];
  const { calls, dependencies } = fixture({
    expandFolder: async (name) => {
      expanded.push(name);
      calls.push("expand:" + name);
    },
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        ...(expanded.length === 2
          ? [
              {
                role: "AXRow",
                depth: 2,
                level: 0,
                canExpand: true,
                expanded: true,
              },
              { role: "AXStaticText", depth: 3, value: "第五阶段" },
              {
                role: "AXRow",
                depth: 2,
                level: 1,
                canExpand: true,
                expanded: true,
              },
              { role: "AXStaticText", depth: 3, value: "子目录" },
              { role: "AXRow", depth: 2, level: 2 },
              { role: "AXStaticText", depth: 3, value: "第一讲" },
            ]
          : []),
      ],
    }),
    manualComplete: () => true,
  });
  await executeCourse(
    dependencies,
    config,
    {
      id: "nested",
      name: "第五阶段 / 子目录 / 第一讲",
      targetText: "第一讲",
    },
    new AbortController().signal,
  );
  assert.deepEqual(expanded, ["第五阶段", "子目录"]);
  assert.ok(calls.indexOf("expand:子目录") < calls.indexOf("capture:start"));
  assert.ok(calls.indexOf("capture:start") < calls.indexOf("click:第一讲:2"));
});

test("failed folder expansion stops before capture", async () => {
  const { calls, dependencies } = fixture({
    expandFolder: async () => {
      throw new Error("展开失败");
    },
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      {
        id: "folder",
        name: "第五阶段 / 第一讲",
        targetText: "第一讲",
      },
      new AbortController().signal,
    ),
    /展开失败/,
  );
  assert.equal(calls.includes("capture:start"), false);
});

test("a delivered click without playback fails promptly and stops capture", async (t) => {
  let now = 0;
  t.mock.method(Date, "now", () => now);
  const { calls, dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2 },
        { role: "AXStaticText", depth: 3, value: "第一讲" },
        { role: "AXSlider", width: 624, value: "0" },
        { role: "AXStaticText", value: "00:00:00/00:00:00" },
      ],
    }),
    sleep: async () => {
      now += 1000;
    },
  });
  await assert.rejects(
    executeCourse(
      dependencies,
      config,
      { id: "no-play", name: "第一讲" },
      new AbortController().signal,
    ),
    /未检测到播放启动/,
  );
  assert.ok(now < 40_000);
  assert.ok(calls.includes("click:第一讲:2"));
  assert.ok(calls.includes("capture:stop"));
  assert.equal(calls.includes("step:waitingCompletion"), false);
});

test("a resume prompt after the course double click is declined before completing capture", async () => {
  const task = fixture();
  const read = task.dependencies.snapshot;
  const click = task.dependencies.click;
  let prompt = false,
    negatives = 0,
    courseClicks = 0;
  task.dependencies.click = async (text, count, match) => {
    await click(text, count, match);
    if (count === 2) {
      prompt = true;
      courseClicks++;
    } else {
      assert.equal(text, "[否]");
      negatives++;
      prompt = false;
    }
  };
  task.dependencies.snapshot = async () => (prompt ? resumePrompt() : read());
  await executeCourse(
    task.dependencies,
    config,
    { id: "1", name: "高等数学 第一讲" },
    new AbortController().signal,
  );
  assert.equal(negatives, 1);
  assert.equal(
    courseClicks,
    1,
    "declining an in-player reminder must not open the course twice",
  );
  assert.ok(task.calls.includes("capture:stop"));
  assert.ok(task.calls.some((call) => call.startsWith("save:")));
});

for (const fault of [
  "missing",
  "ambiguous",
  "stuck",
  "uncertain",
  "cancel",
  "other-error",
] as const) {
  test(`resume prompt safely handles ${fault}`, async () => {
    const abort = new AbortController();
    let clicks = 0;
    const snapshot = resumePrompt();
    if (fault === "missing")
      snapshot.elements = snapshot.elements.filter((e) => e.title !== "[否]");
    if (fault === "ambiguous")
      snapshot.elements.push({ role: "AXButton", title: "否" });
    if (fault === "other-error")
      snapshot.elements.push({ role: "AXStaticText", value: "请输入验证码" });
    const task = fixture({
      snapshot: async () => {
        if (fault === "cancel") abort.abort();
        return snapshot;
      },
      click: async () => {
        clicks++;
        if (fault === "uncertain") throw new Error("uncertain press");
      },
    });
    await assert.rejects(
      preparePlayer(task.dependencies, config, abort.signal),
    );
    assert.equal(clicks, fault === "stuck" || fault === "uncertain" ? 1 : 0);
    assert.equal(task.calls.includes("capture:start"), false);
  });
}

test("an unrelated negative button is never selected automatically", async () => {
  const task = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline" },
        { role: "AXStaticText", value: "是否删除文件？" },
        { role: "AXButton", title: "是" },
        { role: "AXButton", title: "否" },
      ],
    }),
  });
  await preparePlayer(task.dependencies, config, new AbortController().signal);
  assert.equal(
    task.calls.some((call) => call.startsWith("click:")),
    false,
  );
});

test("a slowly disappearing resume prompt receives only one negative press", async () => {
  let clicks = 0,
    readsAfterClick = 0;
  const task = fixture({
    snapshot: async () => {
      if (clicks && ++readsAfterClick >= 3)
        return { running: true, at: "", elements: [{ role: "AXOutline" }] };
      return resumePrompt();
    },
    click: async () => {
      clicks++;
    },
  });
  await preparePlayer(task.dependencies, config, new AbortController().signal);
  assert.equal(clicks, 1);
  assert.equal(readsAfterClick, 3);
});

test("same display paths from different source roots keep distinct course identities", () => {
  const current: PlayerSnapshot = {
    running: true,
    at: "",
    elements: [
      { role: "AXOutline" },
      { role: "AXRow", level: 0, canExpand: true, value: "课程" },
      {
        role: "AXRow",
        level: 1,
        value: "第一讲",
        identifier: "/A/课程/第一讲.sz",
      },
      { role: "AXRow", level: 0, canExpand: true, value: "课程" },
      {
        role: "AXRow",
        level: 1,
        value: "第一讲",
        identifier: "/B/课程/第一讲.sz",
      },
    ],
  };
  const found = coursesFromSnapshot(current);
  assert.equal(found.length, 2);
  assert.notEqual(found[0]!.id, found[1]!.id);
});

test("a missing exact lesson never falls back to a similarly named lesson", async () => {
  const task = fixture();
  const read = task.dependencies.snapshot;
  let now = 0;
  task.dependencies.now = () => now;
  task.dependencies.sleep = async () => {
    now += 500;
  };
  task.dependencies.snapshot = async () => {
    const current = await read();
    current.elements = current.elements.map((element) =>
      element.value === "高等数学 第一讲"
        ? { ...element, value: "高等数学 第一讲 补充" }
        : element,
    );
    return current;
  };
  await assert.rejects(
    executeCourse(
      task.dependencies,
      config,
      { id: "exact", name: "高等数学 第一讲" },
      new AbortController().signal,
    ),
    /仍未看到课程/,
  );
  assert.equal(
    task.calls.some((call) => call.startsWith("click:")),
    false,
  );
});

test("native locator uses the complete current path and player process", async () => {
  let selected: unknown;
  const { dependencies } = fixture({
    snapshot: async () => ({
      running: true,
      at: "",
      pid: 123,
      elements: [
        { role: "AXOutline", depth: 1 },
        {
          role: "AXRow",
          depth: 2,
          level: 0,
          canExpand: true,
          expanded: true,
          value: "目录",
        },
        { role: "AXRow", depth: 2, level: 1, value: "第一讲" },
      ],
    }),
    revealCourse: async (path, pid) => {
      assert.deepEqual(path, ["目录", "第一讲"]);
      assert.equal(pid, 123);
      return dependencies.snapshot();
    },
    click: async (_, __, match) => {
      selected = match;
    },
    manualComplete: () => true,
  });
  await executeCourse(
    dependencies,
    config,
    { id: "a", name: "目录 / 第一讲" },
    new AbortController().signal,
  );
  assert.deepEqual(selected, {
    ordinal: 0,
    count: 1,
    path: ["目录", "第一讲"],
    expectedPid: 123,
  });
});

test("failed reveal or a replaced player cannot start capture", async () => {
  for (const restarted of [false, true]) {
    const { calls, dependencies } = fixture({
      snapshot: async () => ({
        running: true,
        at: "",
        pid: 123,
        elements: [{ role: "AXOutline" }],
      }),
      revealCourse: async () => {
        if (!restarted) throw new Error("目标不在可见窗口内");
        return { running: true, at: "", pid: 456, elements: [] };
      },
    });
    await assert.rejects(
      executeCourse(
        dependencies,
        config,
        { id: "a", name: "第一讲" },
        new AbortController().signal,
      ),
      restarted ? /进程已变化/ : /可见窗口/,
    );
    assert.equal(calls.includes("capture:start"), false);
  }
});

test("filesystem course labels resolve the player filename with its sz extension", async () => {
  let clicked = "";
  let now = 0;
  const { dependencies } = fixture({
    now: () => now,
    snapshot: async () => ({
      running: true,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, canExpand: true, value: "目录" },
        { role: "AXRow", depth: 2, level: 1, value: "第一讲 .sz" },
      ],
    }),
    revealCourse: async () => dependencies.snapshot(),
    click: async (text) => {
      clicked = text;
    },
    sleep: async (ms) => {
      now += ms;
    },
    manualComplete: () => true,
  });
  await executeCourse(
    dependencies,
    { ...config, courseReadyTimeoutSeconds: 1 },
    {
      id: "file",
      name: "目录 / 第一讲",
      targetText: "第一讲",
      sourcePath: "/目录/第一讲 .sz",
    },
    new AbortController().signal,
  );
  assert.equal(clicked, "第一讲 .sz");
});

test("native orchestration waits do not depend on suspended web timers", async (t) => {
  t.mock.method(globalThis, "setTimeout", () => {
    throw new Error("web timers suspended");
  });
  const delays: number[] = [];
  const sleep = createAbortableSleep(async (ms: number) => {
    delays.push(ms);
  });
  await sleep(500, new AbortController().signal);
  assert.deepEqual(delays, [500]);
});

test("canceling a native wait interrupts immediately and ignores its eventual reply", async () => {
  let release!: () => void;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  const controller = new AbortController();
  const waiting = createAbortableSleep(() => pending)(500, controller.signal);
  controller.abort();
  await assert.rejects(waiting, OrchestrationInterrupted);
  release();
  await Promise.resolve();
});

test("course discovery treats a verified empty outline as a successful empty catalog", async () => {
  const { dependencies } = fixture({
    courseSnapshot: async () => ({
      running: true,
      at: "",
      elements: [{ role: "AXOutline" }],
    }),
  });
  assert.deepEqual(
    await discoverCourses(dependencies, config, new AbortController().signal),
    [],
  );
});

test("course discovery does not confuse stopped, incomplete or missing outlines with empty catalogs", async () => {
  for (const snapshot of [
    {
      running: false,
      at: "",
      elements: [{ role: "AXOutline" }, { role: "AXRow", value: "old" }],
    },
    {
      running: true,
      at: "",
      elements: [{ role: "AXOutline" }],
      readIssue: { code: "budget", message: "目录读取不完整" },
    },
    { running: true, at: "", elements: [] },
  ]) {
    const { dependencies } = fixture({ courseSnapshot: async () => snapshot });
    await assert.rejects(
      discoverCourses(dependencies, config, new AbortController().signal),
    );
  }
});

test("course discovery rejects an aborted late result", async () => {
  const controller = new AbortController();
  const { dependencies } = fixture({
    courseSnapshot: async () => {
      controller.abort();
      return {
        running: true,
        at: "",
        elements: [{ role: "AXOutline" }, { role: "AXRow", value: "old" }],
      };
    },
  });
  await assert.rejects(
    discoverCourses(dependencies, config, controller.signal),
  );
});
