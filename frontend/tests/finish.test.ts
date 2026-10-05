import test, { afterEach, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createPinia, setActivePinia } from "pinia";
import { useTraceStore } from "../src/stores/trace";
import {
  KEY_HISTORY_STORAGE,
  readHistoryDocument,
} from "../src/lib/key-history";
import { buildKeyJson } from "../src/lib/keyjson";
import type { CaptureEvent } from "../src/types/trace";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";

const firstKey = "1234567890abcdef1234567890abcdef";
const secondKey = "fedcba0987654321fedcba0987654321";
const originalWindow = globalThis.window;
const originalStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  "localStorage",
);
type TraceStore = ReturnType<typeof useTraceStore>;
let stores: TraceStore[];
let mygo: FakeMyGo;

function freshStore() {
  setActivePinia(createPinia());
  const store = useTraceStore();
  stores.push(store);
  return store;
}

function captureEvents(suffix = "first", password = firstKey): CaptureEvent[] {
  return ["metadata", "password"].flatMap((kind, index) => {
    const metadata = kind === "metadata";
    return (["request", "response"] as const).map((phase, offset) => ({
      id: `${suffix}-${kind}-${phase}`,
      phase,
      ts: new Date(
        Date.UTC(2026, 8, 22, 10, 0, index * 2 + offset),
      ).toISOString(),
      method: "POST",
      url: `https://learn.shenzaokeji.com/api/courseLocal/${metadata ? "getPerinutePwd" : "getPwd3V2"}`,
      host: "learn.shenzaokeji.com",
      status: phase === "response" ? 200 : 0,
      headers: metadata && phase === "request" ? { uit: `user-${suffix}` } : {},
      body:
        phase === "request"
          ? "{}"
          : JSON.stringify(
              metadata
                ? {
                    data: {
                      pwdMaps: { 第一个密码: { 密码: `cipher-${suffix}` } },
                      den: 1200,
                      pmn: 20,
                      pattern: "3",
                    },
                  }
                : { passwords: { "1": password } },
            ),
      bodyTruncated: false,
      source: "sing-box" as const,
    }));
  });
}

function receiver() {
  return mygo;
}

function assertIdle(store: TraceStore) {
  assert.deepEqual(store.traces, []);
  assert.equal(store.total, 0);
  assert.equal(store.selectedId, "");
  assert.equal(store.selected, null);
  assert.equal(store.currentHistoryId, "");
  assert.equal(store.currentSaved, false);
  assert.equal(store.keyRequestCount, 0);
  assert.deepEqual(store.keyResult, buildKeyJson([]));
}

beforeEach(() => {
  stores = [];
  globalThis.localStorage = memoryStorage();
  globalThis.window = new EventTarget() as unknown as Window &
    typeof globalThis;
  mygo = new FakeMyGo().install();
});

afterEach(() => {
  for (const store of stores) store.dispose();
  globalThis.window = originalWindow;
  mygo.restore();
  if (originalStorage)
    Object.defineProperty(globalThis, "localStorage", originalStorage);
  else Reflect.deleteProperty(globalThis, "localStorage");
});

test("finishing saves the final receiver snapshot and resets the extraction page", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  const id = store.currentHistoryId;
  api.events = captureEvents().slice(0, 3);
  await store.refresh(true);
  store.selectedId = store.traces[0]!.id;
  assert.equal(store.keyResult.passwordCount, 0);

  api.events = captureEvents();
  api.calls = [];
  const saved = await store.stopCapture();

  assert.equal(store.captureEnabled, false);
  assert.equal(store.busy, "");
  assertIdle(store);
  assert.equal(saved?.id, id);
  assert.equal(saved?.valid, true);
  assert.deepEqual(saved?.keyJson.passwords, { "1": firstKey });
  assert.equal(saved?.keyJson.getPwdData.密码, "cipher-first");
  assert.equal(saved?.traces?.length, 2);
  assert.ok(saved?.traces?.every((row) => row.hasRequest && row.hasResponse));
  assert.ok(saved?.stoppedAt);
  const persisted = readHistoryDocument(
    localStorage.getItem(KEY_HISTORY_STORAGE),
  );
  assert.deepEqual(persisted.items, [saved]);
  assert.equal(persisted.currentCapture?.id, id);
  assert.equal(persisted.currentCapture?.stoppedAt, saved?.stoppedAt);
  assert.deepEqual(api.methods(), [
    "WorkspaceService.SetCaptureState",
    "WorkspaceService.History",
    "WorkspaceService.CaptureState",
  ]);
});

test("coalesced database writes preserve final passwords and the stopped boundary", async () => {
  const api = receiver();
  let document: Record<string, any> = JSON.parse(mygo.document);
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      await new Promise((resolve) => setTimeout(resolve, 5));
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const saved = await store.stopCapture();
  assert.ok(saved?.stoppedAt);
  assert.deepEqual(document.items, [saved]);
  assert.deepEqual(document.items[0].keyJson.passwords, { "1": firstKey });
  assert.equal(document.currentCapture?.stoppedAt, saved.stoppedAt);
  assertIdle(store);
});

test("reloading an archived capture ignores the receiver's retained buffer", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  api.events = captureEvents();
  const saved = await store.stopCapture();
  const raw = localStorage.getItem(KEY_HISTORY_STORAGE);

  const restored = freshStore();
  restored.loadKeyHistory();
  await restored.refresh(true);
  restored.saveCurrentKey(true);

  assertIdle(restored);
  assert.deepEqual(restored.keyHistory, [saved]);
  assert.equal(await restored.stopCapture(), null);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), raw);
  assert.equal(api.events.length, 4);
});

test("a new capture after finishing gets a new session and independent keys", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  api.events = captureEvents();
  const first = await store.stopCapture();

  api.calls = [];
  await store.startCapture();
  const nextId = store.currentHistoryId;
  assert.notEqual(nextId, first?.id);
  assert.equal(store.captureEnabled, true);
  assert.equal(store.total, 0);
  assert.equal(store.keyResult.passwordCount, 0);
  assert.deepEqual(api.events, []);
  assert.deepEqual(
    api.methods().filter((name) => name.startsWith("WorkspaceService.")),
    ["WorkspaceService.ClearTraces", "WorkspaceService.SetCaptureState"],
  );

  api.events = captureEvents("second", secondKey);
  await store.refresh(true);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": secondKey });
  const second = await store.stopCapture();
  assert.equal(second?.id, nextId);
  assert.deepEqual(second?.keyJson.passwords, { "1": secondKey });
  assert.ok(second?.traces?.every((row) => row.id.startsWith("second-")));
  assert.deepEqual(
    store.keyHistory.find((entry) => entry.id === first?.id),
    first,
  );
  assert.equal(store.keyHistory.length, 2);
  assertIdle(store);
});

test("a failed final archive write retains the visible data and remains recoverable", async () => {
  const api = receiver();
  const store = freshStore();
  await store.init();
  await store.startCapture();
  const id = store.currentHistoryId;
  api.events = captureEvents();
  await store.refresh(true);
  const selectedId = store.traces[0]!.id;
  store.selectedId = selectedId;
  const setItem = localStorage.setItem.bind(localStorage);
  localStorage.setItem = (key, value) => {
    if (
      key === KEY_HISTORY_STORAGE &&
      JSON.parse(value).currentCapture?.stoppedAt
    )
      throw new Error("storage full during final save");
    setItem(key, value);
  };

  await assert.rejects(store.stopCapture(), /未保存/);

  assert.equal(store.captureEnabled, false);
  assert.equal(store.busy, "");
  assert.match(store.historyError, /未保存/);
  assert.equal(store.currentHistoryId, id);
  assert.equal(store.selectedId, selectedId);
  assert.equal(store.total, 2);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": firstKey });
  assert.equal(
    readHistoryDocument(localStorage.getItem(KEY_HISTORY_STORAGE))
      .currentCapture?.stoppedAt,
    undefined,
  );

  localStorage.setItem = setItem;
  mygo.channels[0]!.receive({
    ...captureEvents()[0]!,
    id: "after-failed-save",
    url: "https://example.test/after-failed-save",
  });
  await store.refresh(true);
  assert.equal(store.total, 3);
  assert.equal(store.currentHistoryId, id);
  assert.equal(store.selectedId, selectedId);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": firstKey });
});

test("late channel messages and an in-flight refresh cannot revive a finished extraction", async () => {
  const api = receiver();
  const store = freshStore();
  await store.init();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  let releaseHistory!: (events: CaptureEvent[]) => void;
  let holdHistory = true;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.History" && holdHistory) {
      holdHistory = false;
      return new Promise<CaptureEvent[]>((resolve) => {
        releaseHistory = resolve;
      });
    }
    return mygo.defaultCall(method, ...args);
  };
  const pending = store.refresh(true);
  const saved = await store.stopCapture();
  const raw = localStorage.getItem(KEY_HISTORY_STORAGE);
  for (const event of captureEvents("late", secondKey))
    mygo.channels[0]!.receive(event);
  releaseHistory(captureEvents("late", secondKey));
  await pending;
  await store.refresh(true);

  assert.equal(store.captureEnabled, false);
  assertIdle(store);
  assert.deepEqual(store.keyHistory, [saved]);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), raw);
});

test("a stale tab cannot overwrite or reopen the same archived session", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const stale = freshStore();
  stale.loadKeyHistory();
  await stale.refresh(true);
  assert.equal(stale.currentHistoryId, store.currentHistoryId);
  assert.equal(stale.total, 2);

  const saved = await store.stopCapture();
  const raw = localStorage.getItem(KEY_HISTORY_STORAGE);
  stale.traces[0]!.response.body = JSON.stringify({
    passwords: { "1": secondKey },
  });
  stale.saveCurrentKey(true);
  await stale.refresh(true);

  assertIdle(stale);
  assert.deepEqual(stale.keyHistory, [saved]);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), raw);
});

test("storage notifications clear a same-session tab when its archive finishes", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const stale = freshStore();
  await stale.init();
  stale.selectedId = stale.traces[0]!.id;
  const saved = await store.stopCapture();

  const event = new Event("storage");
  Object.defineProperty(event, "key", { value: KEY_HISTORY_STORAGE });
  window.dispatchEvent(event);

  assertIdle(stale);
  assert.deepEqual(stale.keyHistory, [saved]);
  for (const late of captureEvents("late", secondKey))
    mygo.channels[0]!.receive(late);
  assertIdle(stale);
});

test("deferred completion preserves the final preview until its saved name is confirmed", async () => {
  const api = receiver();
  const store = freshStore();
  await store.init();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const id = store.currentHistoryId;
  const selectedId = store.traces[0]!.id;
  store.selectedId = selectedId;

  const saved = await store.stopCapture({ deferReset: true });

  assert.equal(saved?.id, id);
  assert.ok(saved?.stoppedAt);
  assert.equal(store.captureEnabled, false);
  assert.equal(store.currentHistoryId, id);
  assert.equal(store.selectedId, selectedId);
  assert.deepEqual(store.traces, saved?.traces);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": firstKey });

  const raw = localStorage.getItem(KEY_HISTORY_STORAGE);
  api.events = captureEvents("late", secondKey);
  for (const event of api.events) mygo.channels[0]!.receive(event);
  await store.refresh(true);
  assert.equal(store.currentHistoryId, id);
  assert.equal(store.selectedId, selectedId);
  assert.deepEqual(store.traces, saved?.traces);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), raw);

  store.renameHistory(id, "第一课秘钥");
  store.completeCapture(id);

  assertIdle(store);
  const expected = { ...saved, name: "第一课秘钥" };
  assert.deepEqual(store.keyHistory, [expected]);
  assert.deepEqual(
    readHistoryDocument(localStorage.getItem(KEY_HISTORY_STORAGE)).items,
    [expected],
  );
});

test("a failed deferred rename retains the preview and can be retried before resetting", async () => {
  const api = receiver();
  const store = freshStore();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const selectedId = store.traces[0]!.id;
  store.selectedId = selectedId;
  const saved = await store.stopCapture({ deferReset: true });
  assert.ok(saved);
  const raw = localStorage.getItem(KEY_HISTORY_STORAGE);
  const setItem = localStorage.setItem.bind(localStorage);
  localStorage.setItem = () => {
    throw new Error("storage full during rename");
  };

  assert.throws(
    () => store.renameHistory(saved.id, "重试命名"),
    /名称保存失败/,
  );

  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), raw);
  assert.equal(store.currentHistoryId, saved.id);
  assert.equal(store.selectedId, selectedId);
  assert.deepEqual(store.traces, saved.traces);
  assert.deepEqual(store.keyHistory, [saved]);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": firstKey });

  localStorage.setItem = setItem;
  store.renameHistory(saved.id, "重试命名");
  store.completeCapture(saved.id);

  assertIdle(store);
  assert.deepEqual(
    readHistoryDocument(localStorage.getItem(KEY_HISTORY_STORAGE)).items,
    [{ ...saved, name: "重试命名" }],
  );
});

test("deferred previews survive same-session storage updates and cannot clear a newer capture", async () => {
  const api = receiver();
  const store = freshStore();
  await store.init();
  await store.startCapture();
  api.events = captureEvents();
  await store.refresh(true);
  const selectedId = store.traces[0]!.id;
  store.selectedId = selectedId;
  const saved = await store.stopCapture({ deferReset: true });
  assert.ok(saved);
  const otherTab = freshStore();
  otherTab.renameHistory(saved.id, "另一页签命名");

  const event = new Event("storage");
  Object.defineProperty(event, "key", { value: KEY_HISTORY_STORAGE });
  window.dispatchEvent(event);

  assert.equal(store.currentHistoryId, saved.id);
  assert.equal(store.selectedId, selectedId);
  assert.deepEqual(store.traces, saved.traces);
  assert.equal(store.keyHistory[0]?.name, "另一页签命名");

  await store.startCapture();
  const nextId = store.currentHistoryId;
  assert.notEqual(nextId, saved.id);
  api.events = captureEvents("second", secondKey);
  await store.refresh(true);
  const nextSelectedId = store.traces[0]!.id;
  store.selectedId = nextSelectedId;

  store.completeCapture(saved.id);

  assert.equal(store.captureEnabled, true);
  assert.equal(store.currentHistoryId, nextId);
  assert.equal(store.selectedId, nextSelectedId);
  assert.equal(store.total, 2);
  assert.deepEqual(store.keyResult.keyJson.passwords, { "1": secondKey });
  assert.equal(store.keyHistory.length, 2);
  assert.equal(
    store.keyHistory.find((entry) => entry.id === saved.id)?.name,
    "另一页签命名",
  );
});
