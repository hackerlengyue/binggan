import test, { beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { createPinia, setActivePinia } from "pinia";
import { useTraceStore } from "../src/stores/trace";
import {
  KEY_HISTORY_STORAGE,
  findHistorySource,
  makeHistoryEntry,
  readCaptureSession,
  readDeletedCaptureIds,
  readRetiredCaptureIds,
  readHistoryDocument,
  readKeyHistory,
} from "../src/lib/key-history";
import { buildKeyJson } from "../src/lib/keyjson";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";
import type { TrafficRow } from "../src/lib/traffic";
let mygo: FakeMyGo;
const key1 = "1234567890abcdef1234567890abcdef";
const key2 = "abcdef1234567890abcdef1234567890";
function traffic(
  id = "one",
  passwords: Record<string, string> = { "1": key1 },
): TrafficRow[] {
  const base = {
    method: "POST",
    path: "",
    status: 200,
    durationMs: 20,
    timestamp: "2026-09-21T10:00:00Z",
    requestTruncated: false,
    responseTruncated: false,
    hasRequest: true,
    hasResponse: true,
  };
  return [
    {
      ...base,
      id: "metadata-" + id,
      url: "https://learn.shenzaokeji.com/api/courseLocal/getPerinutePwd",
      request: { headers: { uit: "user-" + id }, body: "{}" },
      response: {
        headers: {},
        body: JSON.stringify({
          data: {
            pwdMaps: { 第一个密码: { 密码: "cipher-" + id } },
            den: 1200,
            pmn: 20,
            pattern: "3",
          },
        }),
      },
    },
    {
      ...base,
      id: "password-" + id,
      timestamp: "2026-09-21T10:00:01Z",
      url: "https://learn.shenzaokeji.com/api/courseLocal/getPwd3V2",
      request: { headers: {}, body: "{}" },
      response: { headers: {}, body: JSON.stringify({ passwords }) },
    },
  ];
}
function freshStore() {
  setActivePinia(createPinia());
  return useTraceStore();
}
beforeEach(() => {
  globalThis.localStorage = memoryStorage();
  mygo = new FakeMyGo().install();
});
afterEach(() => {
  mygo.restore();
});
test("one history document restores entries and capture state consistently", () => {
  const rows = traffic();
  const entry = makeHistoryEntry(buildKeyJson(rows), rows)!;
  const session = { id: entry.id, createdAt: entry.createdAt };
  for (const document of [
    { version: 1, items: [entry] },
    {
      version: 1,
      items: [entry],
      currentCapture: session,
      deletedCaptureIds: ["older-capture"],
    },
    {
      version: 1,
      items: [entry],
      currentCapture: session,
      retiredCaptureIds: [session.id],
    },
  ]) {
    const raw = JSON.stringify(document);
    assert.deepEqual(readHistoryDocument(raw), {
      items: readKeyHistory(raw),
      currentCapture: readCaptureSession(raw),
      deletedCaptureIds: readDeletedCaptureIds(raw),
      retiredCaptureIds: readRetiredCaptureIds(raw),
    });
  }
  assert.deepEqual(readHistoryDocument(null), {
    items: [],
    currentCapture: null,
    deletedCaptureIds: [],
    retiredCaptureIds: [],
  });
});
test("history document validation rejects corruption in every saved section", () => {
  const rows = traffic();
  const entry = makeHistoryEntry(buildKeyJson(rows), rows)!;
  for (const document of [
    { version: 2, items: [] },
    { version: 1, items: [entry, entry] },
    {
      version: 1,
      items: [{ ...entry, traces: [{ ...rows[0], request: null }] }],
    },
    {
      version: 1,
      items: [entry],
      currentCapture: { id: "capture", createdAt: "invalid" },
    },
    { version: 1, items: [entry], deletedCaptureIds: [""] },
  ]) {
    assert.throws(() => readHistoryDocument(JSON.stringify(document)));
  }
});
test("capture identity and latest time preserve timestamp ties without reordering traffic", () => {
  const [metadata, password] = traffic();
  const ordinary = {
    ...metadata!,
    id: "ordinary",
    url: "https://example.test/info",
    timestamp: "2026-09-21T09:00:00Z",
  };
  const last = {
    ...ordinary,
    id: "last",
    timestamp: "2026-09-21T10:00:01.000Z",
  };
  const rows = [
    password!,
    metadata!,
    { ...metadata!, id: "tied-key" },
    ordinary,
    last,
  ];
  const before = [...rows];
  const entry = makeHistoryEntry(buildKeyJson(rows), rows)!;
  assert.equal(findHistorySource(rows), metadata);
  assert.equal(entry.id, metadata!.id);
  assert.equal(entry.createdAt, metadata!.timestamp);
  assert.equal(entry.updatedAt, last.timestamp);
  assert.deepEqual(rows, before);
  assert.deepEqual(entry.traces, rows);
  assert.equal(
    findHistorySource([last, ordinary, { ...ordinary, id: "tied" }]),
    ordinary,
  );
  assert.equal(findHistorySource([]), undefined);
  assert.equal(makeHistoryEntry(buildKeyJson([]), []), null);
  const session = {
    id: "explicit-capture",
    createdAt: ordinary.timestamp,
    stoppedAt: "2026-09-21T11:00:00Z",
  };
  const stopped = makeHistoryEntry(buildKeyJson([]), [], session)!;
  assert.equal(stopped.id, session.id);
  assert.equal(stopped.createdAt, session.createdAt);
  assert.equal(stopped.updatedAt, session.stoppedAt);
});
test("history snapshots isolate request and response headers, arrays, bodies and keys", () => {
  const rows = traffic();
  rows[0]!.request.headers.accept = ["application/json"];
  rows[0]!.response.headers["set-cookie"] = ["session=one"];
  const result = buildKeyJson(rows);
  const entry = makeHistoryEntry(result, rows)!;
  const original = JSON.parse(JSON.stringify(entry));
  rows[0]!.request.headers.uit = "changed";
  (rows[0]!.request.headers.accept as string[]).push("text/plain");
  (rows[0]!.response.headers["set-cookie"] as string[])[0] = "session=two";
  rows[0]!.request.body = "changed request";
  rows[0]!.response.body = "changed response";
  rows[0]!.status = 500;
  result.keyJson.passwords["1"] = key2;
  result.keyJson.getPwdData.uit = "changed";
  result.issues.push("changed");
  assert.deepEqual(JSON.parse(JSON.stringify(entry)), original);
});
test("multiple passwords update one saved entry and restore in a fresh app", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  store.traces = traffic("one", { "1": key1, "2": key2 });
  store.saveCurrentKey();
  store.saveCurrentKey();
  assert.equal(store.keyHistory.length, 1);
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory.length, 1);
  assert.deepEqual(Object.values(restored.keyHistory[0]!.keyJson.passwords), [
    key1,
    key2,
  ]);
  assert.equal(restored.keyHistory[0]!.valid, true);
});
test("a batch response retains every password after naming and reloading history", () => {
  const store = freshStore();
  store.traces = traffic();
  const response = store.traces[1]!;
  response.request.body = JSON.stringify({ dts: 60 });
  response.response.body = JSON.stringify({
    data: [{ pwd: key1 }, { pwd: key2 }],
  });
  store.saveCurrentKey();
  store.renameHistory("metadata-one", "多个密码");
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory[0]!.name, "多个密码");
  assert.deepEqual(restored.keyHistory[0]!.keyJson.passwords, {
    "1": key1,
    "2": key2,
  });
});
test("clear current capture preserves history and next capture appends a separate entry", async () => {
  const store = freshStore();
  store.traces = traffic();
  await store.clear();
  assert.equal(store.total, 0);
  assert.equal(store.keyHistory.length, 1);
  const cleared = readHistoryDocument(
    localStorage.getItem(KEY_HISTORY_STORAGE),
  );
  assert.equal(cleared.currentCapture, null);
  assert.deepEqual(cleared.retiredCaptureIds, ["metadata-one"]);
  const reopened = freshStore();
  reopened.loadKeyHistory();
  assert.equal(reopened.currentHistoryId, "");
  store.traces = traffic("two", { "1": key2 });
  store.saveCurrentKey();
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory.length, 2);
  assert.equal(
    restored.keyHistory.find((e) => e.id === "metadata-one")?.keyJson.getPwdData
      .uit,
    "user-one",
  );
  assert.equal(
    restored.keyHistory.find((e) => e.id === "metadata-two")?.keyJson.getPwdData
      .uit,
    "user-two",
  );
});
test("partial keys are saved and later completed without a duplicate entry", () => {
  const store = freshStore();
  store.traces = traffic().slice(0, 1);
  store.saveCurrentKey();
  assert.equal(store.keyHistory.length, 1);
  assert.equal(store.keyHistory[0]!.valid, false);
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory[0]!.keyJson.getPwdData.密码, "cipher-one");
  assert.throws(() => restored.downloadHistory("metadata-one"), /尚未补全/);
  store.traces = traffic();
  store.saveCurrentKey();
  assert.equal(store.keyHistory.length, 1);
  assert.equal(store.keyHistory[0]!.valid, true);
});
test("a smaller restored traffic window cannot erase previously saved passwords", () => {
  const store = freshStore();
  store.traces = traffic("one", { "1": key1, "2": key2 });
  store.saveCurrentKey();
  store.traces = traffic();
  store.saveCurrentKey();
  assert.equal(Object.keys(store.keyHistory[0]!.keyJson.passwords).length, 2);
});

test("a recovered full response restores segment key order after a partial capture", () => {
  const store = freshStore();
  store.traces = traffic("one", { "2": key2 });
  store.saveCurrentKey();
  assert.deepEqual(Object.values(store.keyHistory[0]!.keyJson.passwords), [
    key2,
  ]);
  store.traces = traffic("one", { "1": key1, "2": key2 });
  store.saveCurrentKey();
  assert.deepEqual(Object.values(store.keyHistory[0]!.keyJson.passwords), [
    key1,
    key2,
  ]);
});
test("storage failure prevents clearing unsaved keys", async () => {
  const store = freshStore();
  store.traces = traffic();
  localStorage.setItem = () => {
    throw new Error("QuotaExceededError");
  };
  await assert.rejects(store.clear(), /未保存/);
  assert.equal(store.total, 2);
  assert.equal(mygo.calls.length, 0);
  assert.ok(store.historyError);
});
test("corrupted history is not overwritten and the failure is visible", () => {
  localStorage.setItem(KEY_HISTORY_STORAGE, "broken");
  const store = freshStore();
  store.traces = traffic();
  store.loadKeyHistory();
  store.saveCurrentKey();
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), "broken");
  assert.ok(store.historyError);
  assert.throws(() => readKeyHistory("broken"));
});
test("saved history remains accessible when receiver refresh fails", async () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.History") throw new Error("offline");
    return mygo.defaultCall(method, ...args);
  };
  const restored = freshStore();
  restored.loadKeyHistory();
  await restored.refresh();
  assert.equal(restored.keyHistory.length, 1);
  assert.equal(restored.keyHistory[0]!.valid, true);
});

test("renamed history survives more capture updates and app reloads", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  store.renameHistory("metadata-one", "第一课密钥");
  store.traces = traffic("one", { "1": key1, "2": key2 });
  store.saveCurrentKey();
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory[0]!.name, "第一课密钥");
  assert.equal(
    Object.keys(restored.keyHistory[0]!.keyJson.passwords).length,
    2,
  );
  assert.throws(() => restored.renameHistory("metadata-one", "  "), /名称/);
});
test("JSON download filename uses the saved name safely", async () => {
  const { keyFilename } = await import("../src/lib/key-history");
  assert.equal(keyFilename("第一课密钥"), "第一课密钥.json");
  assert.equal(keyFilename("lesson.json"), "lesson.json");
  assert.equal(keyFilename("lesson/one:*?"), "lesson_one___.json");
});

test("starting a new capture preserves the last file and resets live keys", async () => {
  const store = freshStore();
  store.traces = traffic("first");
  store.saveCurrentKey();
  store.renameHistory("metadata-first", "上一份文件");
  await store.startCapture();
  assert.deepEqual(
    mygo.methods().filter((method) => method.startsWith("WorkspaceService.")),
    ["WorkspaceService.ClearTraces", "WorkspaceService.SetCaptureState"],
  );
  assert.equal(store.captureEnabled, true);
  assert.equal(store.keyResult.passwordCount, 0);
  assert.equal(
    store.keyHistory.find((e) => e.id === "metadata-first")!.name,
    "上一份文件",
  );
  store.traces = traffic("second", { "1": key2 });
  store.saveCurrentKey();
  assert.equal(store.keyHistory.length, 2);
  assert.deepEqual(
    Object.values(
      store.keyHistory.find((e) => e.id === store.currentHistoryId)!.keyJson
        .passwords,
    ),
    [key2],
  );
});

test("storage failure prevents starting a new capture over unsaved keys", async () => {
  const store = freshStore();
  store.traces = traffic();
  localStorage.setItem = () => {
    throw new Error("full");
  };
  await assert.rejects(store.startCapture(), /未保存/);
  assert.equal(
    mygo.methods().filter((method) => method.startsWith("WorkspaceService."))
      .length,
    0,
  );
  assert.equal(store.captureEnabled, false);
  assert.equal(store.total, 2);
});

test("manual stop returns one saved file and repeated stop produces no file", async () => {
  const store = freshStore();
  const events = traffic().flatMap((row) =>
    ["request", "response"].map((phase) => ({
      id: row.id + phase,
      phase,
      ts: row.timestamp,
      method: row.method,
      url: row.url,
      host: "learn.shenzaokeji.com",
      status: phase === "response" ? 200 : 0,
      ...row[phase as "request" | "response"],
      bodyTruncated: false,
      source: "sing-box",
    })),
  );
  mygo.enabled = true;
  mygo.events = events as typeof mygo.events;
  await store.refresh();
  const file = await store.stopCapture();
  assert.equal(store.captureEnabled, false);
  assert.equal(store.keyHistory.length, 1);
  assert.equal(file?.valid, true);
  assert.deepEqual(Object.values(file!.keyJson.passwords), [key1]);
  assert.equal(await store.stopCapture(), null);
});

test("consecutive captures retain separate requests and passwords after reload", async () => {
  const store = freshStore();

  const ids: string[] = [];
  for (const [suffix, password] of [
    ["first", key1],
    ["second", key2],
  ]) {
    await store.startCapture();
    ids.push(store.currentHistoryId);
    mygo.events = traffic(suffix, { "1": password! }).flatMap((row) =>
      ["request", "response"].map((phase) => ({
        id: row.id + phase,
        phase,
        ts: row.timestamp,
        method: row.method,
        url: row.url,
        host: "learn.shenzaokeji.com",
        status: phase === "response" ? 200 : 0,
        ...row[phase as "request" | "response"],
        bodyTruncated: false,
        source: "sing-box",
      })),
    );
    const saved = await store.stopCapture();
    assert.equal(saved?.traces?.length, 2);
    store.renameHistory(saved!.id, suffix!);
  }
  assert.notEqual(ids[0], ids[1]);
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory.length, 2);
  assert.equal(restored.currentHistoryId, "");
  const first = restored.keyHistory.find((entry) => entry.id === ids[0])!;
  const second = restored.keyHistory.find((entry) => entry.id === ids[1])!;
  assert.deepEqual(Object.values(first.keyJson.passwords), [key1]);
  assert.deepEqual(Object.values(second.keyJson.passwords), [key2]);
  assert.ok(first.traces!.every((row) => row.id.includes("first")));
  assert.ok(second.traces!.every((row) => row.id.includes("second")));
  assert.ok(
    first.traces!.some((row) => row.response.body.includes("cipher-first")),
  );
  assert.ok(
    second.traces!.some((row) => row.response.body.includes("cipher-second")),
  );
});

test("ordinary traffic is saved, and adding keys does not change the capture identity", () => {
  const store = freshStore();
  const ordinary = {
    ...traffic()[0]!,
    id: "ordinary",
    url: "https://example.test/info",
  };
  store.traces = [ordinary];
  store.saveCurrentKey();
  const id = store.currentHistoryId;
  store.traces.push(...traffic());
  store.saveCurrentKey();
  assert.equal(store.currentHistoryId, id);
  assert.equal(store.keyHistory.length, 1);
  assert.equal(store.keyHistory[0]!.traces?.length, 3);
  store.traces = [];
  store.saveCurrentKey();
  assert.equal(store.keyHistory[0]!.traces?.length, 3);
});

test("saved request snapshots are independent from live traffic mutations", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  const body = store.keyHistory[0]!.traces![0]!.response.body;
  store.traces[0]!.response.body = "changed later";
  assert.equal(store.keyHistory[0]!.traces![0]!.response.body, body);
});

test("password-only records keep their names and do not invent raw requests", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  const document = JSON.parse(localStorage.getItem(KEY_HISTORY_STORAGE)!);
  delete document.items[0].traces;
  delete document.currentCapture;
  document.items[0].name = "旧记录";
  const restored = readKeyHistory(JSON.stringify(document));
  assert.equal(restored[0]!.traces, undefined);
  assert.equal(restored[0]!.name, "旧记录");
  assert.deepEqual(Object.values(restored[0]!.keyJson.passwords), [key1]);
});

test("deleting a historical capture preserves the active capture and other records", async () => {
  const store = freshStore();
  store.traces = traffic("first");
  await store.clear();
  store.traces = traffic("second", { "1": key2 });
  store.saveCurrentKey();
  store.captureEnabled = true;
  store.deleteHistory("metadata-first");
  assert.equal(store.keyHistory.length, 1);
  assert.equal(store.currentHistoryId, "metadata-second");
  assert.equal(store.total, 2);
  assert.equal(store.captureEnabled, true);
  store.renameHistory("metadata-second", "保留的记录");
  const restored = freshStore();
  restored.loadKeyHistory();
  assert.equal(restored.keyHistory[0]?.name, "保留的记录");
  assert.deepEqual(Object.values(restored.keyHistory[0]!.keyJson.passwords), [
    key2,
  ]);
  assert.deepEqual(
    JSON.parse(localStorage.getItem(KEY_HISTORY_STORAGE)!).deletedCaptureIds,
    ["metadata-first"],
  );
});

test("deleting the latest capture survives receiver refresh and reload, then allows a new capture", async () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  const bufferedEvents = traffic().flatMap((row) =>
    ["request", "response"].map((phase) => ({
      id: row.id + phase,
      phase,
      ts: row.timestamp,
      method: row.method,
      url: row.url,
      host: "learn.shenzaokeji.com",
      status: phase === "response" ? 200 : 0,
      ...row[phase as "request" | "response"],
      bodyTruncated: false,
      source: "sing-box",
    })),
  );
  mygo.events = bufferedEvents as typeof mygo.events;
  store.deleteHistory("metadata-one");
  assert.equal(store.total, 0);
  assert.equal(store.currentHistoryId, "");
  await store.refresh();
  assert.equal(store.keyHistory.length, 0);
  assert.equal(store.total, 0);
  const restored = freshStore();
  restored.loadKeyHistory();
  await restored.refresh();
  assert.equal(restored.keyHistory.length, 0);
  assert.equal(restored.total, 0);
  await restored.startCapture();
  restored.traces = traffic("next", { "1": key2 });
  restored.saveCurrentKey();
  assert.equal(restored.keyHistory.length, 1);
  assert.notEqual(restored.currentHistoryId, "metadata-one");
  assert.deepEqual(Object.values(restored.keyHistory[0]!.keyJson.passwords), [
    key2,
  ]);
});

test("a stale tab cannot save a deleted capture back into history", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  const stale = freshStore();
  stale.loadKeyHistory();
  stale.traces = traffic();
  store.deleteHistory("metadata-one");
  stale.saveCurrentKey();
  assert.equal(stale.keyHistory.length, 0);
  assert.equal(stale.total, 0);
  assert.equal(
    readKeyHistory(localStorage.getItem(KEY_HISTORY_STORAGE)).length,
    0,
  );
});

test("deleting a capture in progress is rejected without changing its contents", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  store.captureEnabled = true;
  const before = localStorage.getItem(KEY_HISTORY_STORAGE);
  assert.throws(() => store.deleteHistory("metadata-one"), /先停止/);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), before);
  assert.equal(store.total, 2);
});

test("failed deletion keeps the record and working contents intact", () => {
  const store = freshStore();
  store.traces = traffic();
  store.saveCurrentKey();
  const before = localStorage.getItem(KEY_HISTORY_STORAGE);
  localStorage.setItem = () => {
    throw new Error("write failed");
  };
  assert.throws(() => store.deleteHistory("metadata-one"), /删除失败/);
  assert.equal(localStorage.getItem(KEY_HISTORY_STORAGE), before);
  assert.equal(store.keyHistory.length, 1);
  assert.equal(store.total, 2);
  assert.equal(store.currentHistoryId, "metadata-one");
});

test("history merging never borrows a checksum from a different player version", async () => {
  const { upsertKeyHistory } = await import("../src/lib/key-history");
  const rows = traffic();
  const old = makeHistoryEntry(buildKeyJson(rows), rows)!;
  old.keyJson.getPwdData.softwareName = "MAC_sz_old";
  old.keyJson.getPwdData.appMd5 = "a".repeat(32);
  const next = structuredClone(old);
  next.keyJson.getPwdData.softwareName = "MAC_sz_new";
  delete next.keyJson.getPwdData.appMd5;
  const merged = upsertKeyHistory([old], next)[0]!;
  assert.equal(merged.keyJson.getPwdData.appMd5, undefined);
  assert.equal(merged.valid, false);
  assert.ok(merged.issues.some((issue) => issue.includes("不同播放器参数")));
  const restored = readKeyHistory(
    JSON.stringify({ version: 1, items: [merged] }),
  )[0]!;
  assert.equal(
    restored.valid,
    false,
    "reload must not clear a known player mismatch",
  );
  assert.ok(restored.issues.some((issue) => issue.includes("不同播放器参数")));
});

test("database history survives an empty WebView and rename/delete are persisted", async () => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  let document = {
    version: 1,
    items: [entry],
    currentCapture: null,
    deletedCaptureIds: [] as string[],
  };
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const first = freshStore();
  await first.initKeyHistory();
  assert.equal(first.keyHistory.length, 1);
  assert.equal(first.keyHistory[0]?.id, entry.id);
  first.renameHistory(entry.id, "持久化名称");
  await first.flushKeyHistory();
  localStorage.clear();
  const second = freshStore();
  await second.initKeyHistory();
  assert.equal(second.keyHistory.length, 1);
  assert.equal(second.keyHistory[0]?.name, "持久化名称");
  second.deleteHistory(entry.id);
  await second.flushKeyHistory();
  localStorage.clear();
  const third = freshStore();
  await third.initKeyHistory();
  assert.equal(third.keyHistory.length, 0);
  assert.ok(document.deletedCaptureIds.includes(entry.id));
});

test("a fresh workspace loads an empty database instead of cached records", async () => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  localStorage.setItem(
    KEY_HISTORY_STORAGE,
    JSON.stringify({ version: 1, items: [entry] }),
  );
  const store = freshStore();
  await store.initKeyHistory();
  assert.equal(store.keyHistory.length, 0);
  assert.equal(JSON.parse(mygo.document).items.length, 0);
  assert.ok(!mygo.methods().includes("WorkspaceService.SaveKeyHistory"));
});

test("a failed database write is not reported as saved history", async () => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  const document = {
    version: 1,
    items: [entry],
    currentCapture: null,
    deletedCaptureIds: [],
  };
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory")
      throw new Error("database unavailable");
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  store.renameHistory(entry.id, "尚未落盘");
  await assert.rejects(store.flushKeyHistory(), /未写入数据库/);
  assert.match(store.historyError, /未写入数据库/);
});

test("bursts of durable history updates write only the latest queued snapshot", async (t) => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  let document = {
    version: 1,
    items: [entry],
    currentCapture: null,
    deletedCaptureIds: [] as string[],
  };
  let writes = 0;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      writes++;
      await new Promise((resolve) => setTimeout(resolve, 5));
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  const start = performance.now();
  store.renameHistory(entry.id, "name-0");
  await new Promise((resolve) => setImmediate(resolve));
  for (let i = 1; i < 50; i++) store.renameHistory(entry.id, `name-${i}`);
  await store.flushKeyHistory();
  t.diagnostic(
    `50 updates: ${writes} writes, ${Math.round(performance.now() - start)} ms`,
  );
  assert.equal(document.items[0]!.name, "name-49");
  assert.equal(writes, 2);
});

test("retry persists a failed rename even when there is no active capture", async () => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  let document = {
    version: 1,
    items: [entry],
    currentCapture: null,
    deletedCaptureIds: [] as string[],
  };
  let failing = true;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      if (failing) throw new Error("database unavailable");
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  store.renameHistory(entry.id, "pending-rename");
  await assert.rejects(store.flushKeyHistory(), /未写入数据库/);
  failing = false;
  await store.retryKeyHistory();
  assert.equal(document.items[0]!.name, "pending-rename");
  assert.equal(store.historyError, "");
});

test("an earlier successful write cannot hide failure of the latest snapshot", async () => {
  const entry = makeHistoryEntry(buildKeyJson(traffic()), traffic(), null)!;
  let document = {
    version: 1,
    items: [entry],
    currentCapture: null,
    deletedCaptureIds: [] as string[],
  };
  let writes = 0;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      writes++;
      await new Promise((resolve) => setTimeout(resolve, 5));
      if (writes === 2) throw new Error("database unavailable");
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  store.renameHistory(entry.id, "first");
  await new Promise((resolve) => setImmediate(resolve));
  store.renameHistory(entry.id, "latest");
  await assert.rejects(store.flushKeyHistory(), /未写入数据库/);
  assert.equal(document.items[0]!.name, "first");
  assert.equal(store.keyHistory[0]!.name, "latest");
  await store.retryKeyHistory();
  assert.equal(document.items[0]!.name, "latest");
  assert.equal(store.historyError, "");
});

test("batch history deletion writes once and failed deletion can be retried", async () => {
  const items = ["a", "b", "keep"].map((id) =>
    makeHistoryEntry(buildKeyJson(traffic(id)), traffic(id), null)!,
  );
  let document = {
    version: 1,
    items,
    currentCapture: null,
    deletedCaptureIds: [] as string[],
  };
  let writes = 0,
    failing = true;
  mygo.handler = async (method, ...args) => {
    if (method === "WorkspaceService.KeyHistory")
      return JSON.stringify(document);
    if (method === "WorkspaceService.SaveKeyHistory") {
      writes++;
      if (failing) throw new Error("database unavailable");
      document = JSON.parse(String(args[0]));
      return JSON.stringify(document);
    }
    return mygo.defaultCall(method, ...args);
  };
  const store = freshStore();
  await store.initKeyHistory();
  const targets = items.slice(0, 2).map((item) => item.id);
  store.deleteHistories(targets);
  await assert.rejects(store.flushKeyHistory(), /未写入数据库/);
  assert.equal(writes, 1);
  assert.equal(document.items.length, 3);
  failing = false;
  store.deleteHistories(targets);
  await store.flushKeyHistory();
  assert.equal(writes, 2);
  assert.deepEqual(
    document.items.map((item) => item.id),
    [items[2]!.id],
  );
  assert.deepEqual(new Set(document.deletedCaptureIds), new Set(targets));
});

test("history readiness distinguishes a pending database read from a loaded empty library", async () => {
  localStorage.clear();
  let finish!: (document: string) => void;
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.KeyHistory"
      ? new Promise<string>((resolve) => {
          finish = resolve;
        })
      : mygo.defaultCall(method, ...args);
  const store = freshStore();
  const pending = store.initKeyHistory();
  assert.equal(store.historyReady, false);
  assert.equal(store.keyHistory.length, 0);
  finish(
    JSON.stringify({
      version: 1,
      items: [],
      currentCapture: null,
      deletedCaptureIds: [],
    }),
  );
  await pending;
  assert.equal(store.historyReady, true);
  assert.equal(store.keyHistory.length, 0);
});

test("a failed initial history read remains unavailable rather than a loaded empty library", async () => {
  localStorage.clear();
  mygo.handler = async () => {
    throw new Error("database unavailable");
  };
  const store = freshStore();
  await assert.rejects(store.initKeyHistory());
  assert.equal(store.historyReady, false);
});
