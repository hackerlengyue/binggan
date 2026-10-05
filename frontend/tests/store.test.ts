import test, { beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { setActivePinia, createPinia } from "pinia";
import { useTraceStore } from "../src/stores/trace";
import { FakeMyGo } from "./mygo-fixture";

let store: ReturnType<typeof useTraceStore>;
let mygo: FakeMyGo;
const originalWindow = globalThis.window;

beforeEach(() => {
  setActivePinia(createPinia());
  const data = new Map<string, string>();
  globalThis.localStorage = {
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => {
      data.set(key, value);
    },
    removeItem: (key: string) => {
      data.delete(key);
    },
    clear: () => data.clear(),
    key: () => null,
    length: 0,
  };
  globalThis.window = new EventTarget() as unknown as Window &
    typeof globalThis;
  mygo = new FakeMyGo().install();
  store = useTraceStore();
});
afterEach(() => {
  store.dispose();
  mygo.restore();
  globalThis.window = originalWindow;
});

for (const stage of ["ready", "error", "offline"]) {
  test(`manual capture recovers only an unavailable native monitor: ${stage}`, async () => {
    mygo.handler = async (method, ...args) =>
      method === "CaptureService.State"
        ? { stage }
        : mygo.defaultCall(method, ...args);
    await store.startCapture();
    const native = mygo
      .methods()
      .filter((name) => name.startsWith("CaptureService."));
    assert.deepEqual(
      native,
      stage === "ready"
        ? ["CaptureService.State"]
        : ["CaptureService.State", "CaptureService.Retry"],
    );
    assert.equal(store.captureEnabled, true);
  });
}

test("native recovery failure leaves receiver and records untouched", async () => {
  mygo.handler = async (method, ...args) => {
    if (method === "CaptureService.State") return { stage: "error" };
    if (method === "CaptureService.Retry")
      throw new Error("native receiver unavailable");
    return mygo.defaultCall(method, ...args);
  };
  await assert.rejects(store.startCapture(), /native receiver unavailable/);
  assert.deepEqual(
    mygo.methods().filter((name) => name.startsWith("WorkspaceService.")),
    [],
  );
  assert.equal(store.captureEnabled, false);
  assert.equal(store.currentHistoryId, "");
  assert.equal(store.keyHistory.length, 0);
  assert.equal(store.busy, "");
});

test("binding failure reports the service error and releases loading", async () => {
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.History")
      throw new Error("receiver offline");
    return mygo.defaultCall(method, ...args);
  };
  await store.refresh();
  assert.match(store.error, /receiver offline/);
  assert.equal(store.loading, false);
  assert.equal(store.captureEnabled, false);
});

test("invalid typed history and state are rejected", async () => {
  for (const [method, value] of [
    ["WorkspaceService.History", null],
    ["WorkspaceService.CaptureState", "true"],
  ] as const) {
    mygo.handler = async (name, ...args) =>
      name === method ? value : mygo.defaultCall(name, ...args);
    await assert.rejects(store.refresh(true), /数据格式无效/);
    assert.equal(store.loading, false);
  }
});

test("malformed capture records are rejected without publishing partial traffic", async () => {
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.History"
      ? [{ id: "invalid", phase: "request" }]
      : mygo.defaultCall(method, ...args);
  await assert.rejects(store.refresh(true), /无法识别的流量记录/);
  assert.equal(store.total, 0);
});

test("a wrong command result cannot report a successful capture start", async () => {
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.SetCaptureState"
      ? false
      : mygo.defaultCall(method, ...args);
  await assert.rejects(store.startCapture(), /没有确认提取状态/);
  assert.equal(store.captureEnabled, false);
  assert.equal(store.busy, "");
});

test("retry clears the sync error once the receiver returns valid data", async () => {
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.History")
      throw new Error("receiver offline");
    return mygo.defaultCall(method, ...args);
  };
  await store.refresh();
  assert.match(store.error, /receiver offline/);
  mygo.handler = null;
  mygo.enabled = true;
  await store.refresh(true);
  assert.equal(store.error, "");
  assert.equal(store.captureEnabled, true);
});

test("failed start leaves capture disabled", async () => {
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.ClearTraces") throw new Error("offline");
    return mygo.defaultCall(method, ...args);
  };
  await assert.rejects(store.startCapture(), /offline/);
  assert.equal(store.captureEnabled, false);
  assert.equal(store.busy, "");
});

test("a double start resets and enables receiver only once", async () => {
  await Promise.all([store.startCapture(), store.startCapture()]);
  assert.deepEqual(
    mygo
      .methods()
      .filter(
        (name) =>
          name === "WorkspaceService.ClearTraces" ||
          name === "WorkspaceService.SetCaptureState",
      ),
    ["WorkspaceService.ClearTraces", "WorkspaceService.SetCaptureState"],
  );
  assert.equal(store.captureEnabled, true);
});

test("failed clear preserves visible records", async () => {
  store.traces = [
    {
      id: "preserve",
      method: "GET",
      url: "/api",
      path: "/api",
      status: 200,
      durationMs: 20,
      timestamp: new Date().toISOString(),
      request: { headers: {}, body: "" },
      response: { headers: {}, body: "" },
      requestTruncated: false,
      responseTruncated: false,
      hasRequest: true,
      hasResponse: true,
    },
  ];
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.ClearTraces")
      throw new Error("write failed");
    return mygo.defaultCall(method, ...args);
  };
  await assert.rejects(store.clear(), /write failed/);
  assert.equal(store.traces.length, 1);
});

test("the request list keeps successful and failed requests with selected detail", () => {
  store.traces = ["a", "b"].map((id, index) => ({
    id,
    method: "GET",
    url: "/" + id,
    path: "/" + id,
    status: index ? 500 : 200,
    durationMs: 20,
    timestamp: new Date().toISOString(),
    request: { headers: {}, body: "" },
    response: { headers: {}, body: "" },
    requestTruncated: false,
    responseTruncated: false,
    hasRequest: true,
    hasResponse: true,
  }));
  store.selectedId = "a";
  assert.deepEqual(
    store.traces.map((row) => row.status),
    [200, 500],
  );
  assert.equal(store.total, 2);
  assert.equal(store.selected?.id, "a");
  assert.equal(store.keyResult.passwordCount, 0);
});

test("an empty manual capture is saved as one independent record", async () => {
  await store.startCapture();
  assert.equal(store.captureEnabled, true);
  const entry = await store.stopCapture();
  assert.equal(store.captureEnabled, false);
  assert.equal(store.keyHistory.length, 1);
  assert.equal(entry?.id, store.keyHistory[0]?.id);
  assert.equal(store.currentHistoryId, "");
  assert.deepEqual(entry?.traces, []);
  assert.deepEqual(entry?.keyJson.passwords, {});
  assert.ok(entry?.stoppedAt);
  assert.ok(localStorage.getItem("shz_key_history_v1"));
});

test("pause and resume preserve one capture session and allow final save", async () => {
  await store.startCapture();
  const id = store.currentHistoryId;
  await store.pauseCapture();
  assert.equal(store.captureEnabled, false);
  assert.equal(store.currentHistoryId, id);
  await store.resumeCapture();
  assert.equal(store.captureEnabled, true);
  assert.equal(store.currentHistoryId, id);
  const saved = await store.stopCapture();
  assert.equal(saved?.id, id);
  assert.deepEqual(
    mygo.calls
      .filter((entry) => entry.method === "WorkspaceService.SetCaptureState")
      .map((entry) => entry.args[0]),
    [true, false, true, false],
  );
});

test("failed manual stop keeps capture enabled and allows retry", async () => {
  store.captureEnabled = true;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.SetCaptureState")
      throw new Error("stop failed");
    return mygo.defaultCall(method, ...args);
  };
  await assert.rejects(store.stopCapture(), /stop failed/);
  assert.equal(store.captureEnabled, true);
  assert.equal(store.busy, "");
});

test("concurrent manual stops send one stop command", async () => {
  store.captureEnabled = true;
  await Promise.all([store.stopCapture(), store.stopCapture()]);
  assert.equal(
    mygo.methods().filter((name) => name === "WorkspaceService.SetCaptureState")
      .length,
    1,
  );
  assert.equal(store.captureEnabled, false);
});

test("a stale refresh cannot turn a manually stopped capture back on", async () => {
  store.captureEnabled = true;
  let releaseHistory!: (events: unknown[]) => void;
  let firstHistory = true;
  let firstState = true;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.History" && firstHistory) {
      firstHistory = false;
      return new Promise<unknown[]>((resolve) => {
        releaseHistory = resolve;
      });
    }
    if (method === "WorkspaceService.CaptureState" && firstState) {
      firstState = false;
      return true;
    }
    return mygo.defaultCall(method, ...args);
  };
  const stale = store.refresh();
  await store.stopCapture();
  releaseHistory([]);
  await stale;
  assert.equal(store.captureEnabled, false);
});

test("capture ownership is published before enabling the receiver", async () => {
  let owner = "";
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.SetCaptureState" && args[0] === true)
      assert.equal(owner, store.currentHistoryId);
    return mygo.defaultCall(method, ...args);
  };
  await store.startCapture((id) => {
    owner = id;
  });
  assert.ok(owner);
  assert.equal(store.captureEnabled, true);
});

test("failure to persist the new capture owner prevents receiver enablement", async () => {
  await assert.rejects(
    store.startCapture(() => {
      throw new Error("owner journal unavailable");
    }),
    /owner journal unavailable/,
  );
  assert.equal(store.captureEnabled, false);
  assert.equal(
    mygo.calls.filter(
      (call) => call.method === "WorkspaceService.SetCaptureState",
    ).length,
    0,
  );
});
