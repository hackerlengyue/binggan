// Emits real IPC arguments for the Go validation contract test; no player input.
import { executeCourse } from "../../src/lib/player-orchestration";
import { playerAutomation } from "../../src/lib/player-automation";
import { FakeMyGo } from "../mygo-fixture";
const native = new FakeMyGo().install();
const clicks: unknown[] = [];
try {
  for (const duplicate of [false, true]) {
    const snapshot = {
      running: true,
      pid: 123,
      at: "",
      elements: [
        { role: "AXOutline", depth: 1 },
        { role: "AXRow", depth: 2, level: 0, value: "目录", canExpand: true },
        { role: "AXRow", depth: 2, level: 1, value: "第一讲 .sz" },
        ...(duplicate
          ? [
              {
                role: "AXRow",
                depth: 2,
                level: 0,
                value: "另一个目录",
                canExpand: true,
              },
              { role: "AXRow", depth: 2, level: 1, value: "第一讲 .sz" },
            ]
          : []),
      ],
    };
    native.handler = async (method, ...args) => {
      if (
        method === "PlayerService.Snapshot" ||
        method === "PlayerService.RevealCourse"
      )
        return snapshot;
      if (method === "PlayerService.Click") clicks.push(args[0]);
    };
    let capturing = false;
    await executeCourse(
      {
        ...playerAutomation,
        startCapture: async () => {
          capturing = true;
        },
        captureActive: () => capturing,
        stopCapture: async () => {
          capturing = false;
          return { id: "test", name: "test" };
        },
        saveCapture: async () => {},
        sleep: async () => {},
        manualComplete: () => true,
        onStep: () => {},
        onLog: () => {},
      },
      {
        playbackVolumePercent: 5,
        playbackRate: null,
        actionDelayMs: 0,
        courseReadyTimeoutSeconds: 1,
        pollIntervalMs: 500,
        maxPlaybackMinutes: 10,
      },
      {
        id: "one",
        name: "目录 / 第一讲",
        targetText: "第一讲",
        sourcePath: "/目录/第一讲 .sz",
      },
      new AbortController().signal,
    );
  }
  process.stdout.write(JSON.stringify(clicks));
} finally {
  native.restore();
}
