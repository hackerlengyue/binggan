import test, { beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createPinia, setActivePinia } from "pinia";
import { useTraceStore } from "../src/stores/trace";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";
import type { CaptureEvent } from "../src/types/trace";

const originalWindow = globalThis.window;
let store: ReturnType<typeof useTraceStore>;
let mygo: FakeMyGo;

beforeEach(() => {
  setActivePinia(createPinia());
  globalThis.localStorage = memoryStorage();
  globalThis.window = new EventTarget() as unknown as Window &
    typeof globalThis;
  mygo = new FakeMyGo().install();
  store = useTraceStore();
});
afterEach(() => {
  store.dispose();
  globalThis.window = originalWindow;
  mygo.restore();
});

const settle = () => new Promise<void>((resolve) => setImmediate(resolve));

function captureEvent(index: number): CaptureEvent {
  return {
    id: `request-${index}`,
    phase: "request",
    ts: new Date(Date.UTC(2026, 8, 21) + index).toISOString(),
    method: "GET",
    url: `https://example.test/api/${index}`,
    host: "example.test",
    headers: {},
    body: "",
    bodyTruncated: false,
    source: "sing-box",
  };
}

test("a failed MyGo channel reconnects and restores the connected state", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  await store.init();
  await settle();
  const first = mygo.channels[0]!;
  assert.equal(store.connected, true);
  first.fail();
  await settle();
  assert.equal(store.connected, false);
  assert.equal(store.channelError, "实时通道连接中断");
  assert.match(store.channelLogs.at(-1)!.message, /3 秒后自动重连/);
  t.mock.timers.tick(3000);
  assert.equal(mygo.channels.length, 2);
  await settle();
  assert.equal(store.connected, true);
  assert.equal(store.error, "");
  assert.equal(store.channelError, "");
  assert.ok(store.channelLogs.some((entry) => entry.level === "error"));
});

test("manual retry creates a new MyGo channel immediately", async () => {
  await store.init();
  mygo.channels[0]!.fail();
  await settle();
  await store.retry();
  assert.equal(mygo.channels.length, 2);
  await settle();
  assert.equal(store.connected, true);
});

test("disposing cancels reconnection and prevents a late init from reconnecting", async (t) => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  await store.init();
  mygo.channels[0]!.fail();
  await settle();
  store.dispose();
  t.mock.timers.tick(3000);
  assert.equal(mygo.channels.length, 1);
  let release!: (events: CaptureEvent[]) => void;
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.History"
      ? new Promise<CaptureEvent[]>((resolve) => {
          release = resolve;
        })
      : mygo.defaultCall(method, ...args);
  const pending = store.init();
  store.dispose();
  release([]);
  await pending;
  assert.equal(mygo.channels.length, 1);
});

test("channel replays are deduplicated against history and live messages", async () => {
  const request = captureEvent(0);
  mygo.events = [request];
  await store.init();
  const source = mygo.channels[0]!;
  source.receive(request);
  assert.equal(store.total, 1);
  const response: CaptureEvent = {
    ...request,
    id: "response-0",
    phase: "response",
    status: 200,
    body: "complete",
  };
  source.receive(response);
  source.receive(response);
  assert.equal(store.total, 1);
  assert.equal(store.traces[0]?.response.body, "complete");
  assert.equal(store.keyHistory[0]?.traces?.length, 1);
});

test("clearing the capture releases event IDs for the next capture", async () => {
  await store.init();
  const source = mygo.channels[0]!;
  const request = captureEvent(0);
  source.receive(request);
  await store.clear();
  assert.equal(store.total, 0);
  source.receive(request);
  assert.equal(store.total, 1);
});

test("a clear while disconnected cannot swallow another tab's later clear", async () => {
  await store.init();
  mygo.channels[0]!.fail();
  await settle();
  await store.clear();
  await store.retry();
  const source = mygo.channels[1]!;
  source.receive(captureEvent(1));
  assert.equal(store.total, 1);
  source.emit({ kind: "clear", clientId: "another-tab" });
  assert.equal(store.total, 0);
});

test("clear notifications identify their sender even when another tab clears first", async () => {
  await store.init();
  const source = mygo.channels[0]!;
  await store.clear();
  const ownClient = String(
    mygo.calls.find((entry) => entry.method === "WorkspaceService.ClearTraces")
      ?.args[0],
  );
  assert.ok(ownClient);
  source.receive(captureEvent(1));
  source.emit({ kind: "clear", clientId: "another-tab" });
  assert.equal(store.total, 0);
  source.receive(captureEvent(2));
  source.emit({ kind: "clear", clientId: ownClient });
  assert.equal(store.total, 1);
});

test("live events remain deduplicated while evicted IDs can be received again", async () => {
  const history = Array.from({ length: 5000 }, (_, index) =>
    captureEvent(index),
  );
  mygo.events = history;
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.History"
      ? history
      : mygo.defaultCall(method, ...args);
  await store.init();
  const source = mygo.channels[0]!;
  const newest = captureEvent(5000);
  source.receive(newest);
  const saved = localStorage.getItem("shz_key_history_v1");
  source.receive(history[1]!);
  source.receive(newest);
  assert.equal(localStorage.getItem("shz_key_history_v1"), saved);
  source.receive(history[0]!);
  assert.equal(store.traces[0]?.id, history[0]!.id);
  assert.equal(store.total, 2500);
});

test("late messages from a replaced channel cannot change capture state", async () => {
  await store.init();
  const first = mygo.channels[0]!;
  first.fail();
  await settle();
  await store.retry();
  first.receive(captureEvent(999));
  first.emit({ kind: "capture_state", enabled: true });
  assert.equal(store.total, 0);
  assert.equal(store.captureEnabled, false);
  mygo.channels[1]!.receive(captureEvent(1));
  assert.equal(store.total, 1);
});
